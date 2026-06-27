//go:build with_quic

package httpclient

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	stdTLS "crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/quic-go"
	"github.com/sagernet/quic-go/http3"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/singlink/singlink/option"
)

type http3LifecycleDialer struct {
	dialer net.Dialer
	conn   atomic.Pointer[http3LifecycleConn]
}

func (d *http3LifecycleDialer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	conn, err := d.dialer.DialContext(ctx, network, destination.String())
	if err != nil {
		return nil, err
	}
	trackedConn := &http3LifecycleConn{Conn: conn}
	d.conn.Store(trackedConn)
	return trackedConn, nil
}

func (d *http3LifecycleDialer) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	return nil, net.ErrClosed
}

type http3LifecycleConn struct {
	net.Conn
	closeCount atomic.Int32
}

func (c *http3LifecycleConn) Close() error {
	c.closeCount.Add(1)
	return c.Conn.Close()
}

func TestHTTP3RoundTripperClosesRawUDPConnOnCloseIdleConnections(t *testing.T) {
	serverAddr, closeServer := startHTTP3LifecycleServer(t)
	defer closeServer()

	dialer := &http3LifecycleDialer{}
	transport := newHTTP3RoundTripper(dialer, nil, option.QUICOptions{})
	transport.TLSClientConfig.InsecureSkipVerify = true
	defer transport.Close()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://"+serverAddr+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()

	transport.CloseIdleConnections()

	trackedConn := dialer.conn.Load()
	if trackedConn == nil {
		t.Fatal("HTTP/3 transport did not dial through the test dialer")
	}
	eventually(t, time.Second, func() bool {
		return trackedConn.closeCount.Load() == 1
	}, "raw UDP conn was not closed after CloseIdleConnections")

	if err := transport.Close(); err != nil {
		t.Fatal(err)
	}
	if closeCount := trackedConn.closeCount.Load(); closeCount != 1 {
		t.Fatalf("raw UDP conn close count = %d, want 1", closeCount)
	}
}

type http3LifecycleFallback struct{}

func (http3LifecycleFallback) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("fallback")),
		Request:    request,
	}, nil
}

func (http3LifecycleFallback) CloseIdleConnections() {}

func (http3LifecycleFallback) Close() error { return nil }

func TestHTTP3RaceDoesNotLeaveCleanupGoroutineWhenLoserIgnoresCancel(t *testing.T) {
	blockDial := make(chan struct{})
	var closeBlock sync.Once
	defer closeBlock.Do(func() { close(blockDial) })

	transport := &http3FallbackTransport{
		h3Transport: &http3.Transport{
			Dial: func(ctx context.Context, addr string, tlsCfg *stdTLS.Config, cfg *quic.Config) (*quic.Conn, error) {
				<-blockDial
				return nil, ctx.Err()
			},
		},
		h2Fallback:    http3LifecycleFallback{},
		fallbackDelay: time.Millisecond,
		broken:        make(map[string]http3BrokenEntry),
	}

	before := runtime.NumGoroutine()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.roundTripHTTP3Race(request, "example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()

	eventually(t, 300*time.Millisecond, func() bool {
		return runtime.NumGoroutine() <= before+1
	}, "HTTP/3 race left more goroutines than the blocked loser")

	closeBlock.Do(func() { close(blockDial) })
	eventually(t, time.Second, func() bool {
		return runtime.NumGoroutine() <= before
	}, "blocked loser goroutine did not exit after unblocking dial")
}

func startHTTP3LifecycleServer(t *testing.T) (string, func()) {
	t.Helper()
	packetConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http3.Server{
		TLSConfig: &stdTLS.Config{
			Certificates: []stdTLS.Certificate{newHTTP3LifecycleCertificate(t)},
		},
		Handler: http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			_, _ = response.Write([]byte("ok"))
		}),
	}
	errCh := make(chan error, 1)
	go func() {
		if err := server.Serve(packetConn); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	return packetConn.LocalAddr().String(), func() {
		_ = server.Close()
		_ = packetConn.Close()
		if err := <-errCh; err != nil {
			t.Fatalf("HTTP/3 test server failed: %v", err)
		}
	}
}

func newHTTP3LifecycleCertificate(t *testing.T) stdTLS.Certificate {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER := x509.MarshalPKCS1PrivateKey(privateKey)
	certificate, err := stdTLS.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}),
		pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyDER}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return certificate
}

func eventually(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if condition() {
		return
	}
	t.Fatal(message)
}
