//go:build with_quic

package v2rayquic

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	stdTLS "crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/quic-go"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/singlink/singlink/common/tls"
	"github.com/singlink/singlink/option"
)

type quicLifecycleDialer struct {
	dialer net.Dialer
	conns  []*quicLifecycleConn
}

func (d *quicLifecycleDialer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	conn, err := d.dialer.DialContext(ctx, network, destination.String())
	if err != nil {
		return nil, err
	}
	trackedConn := &quicLifecycleConn{Conn: conn}
	d.conns = append(d.conns, trackedConn)
	return trackedConn, nil
}

func (d *quicLifecycleDialer) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	return nil, net.ErrClosed
}

type quicLifecycleConn struct {
	net.Conn
	closeCount atomic.Int32
}

func (c *quicLifecycleConn) Close() error {
	c.closeCount.Add(1)
	return c.Conn.Close()
}

func TestOfferNewClosesPreviousRawConnAfterSuccessfulReconnect(t *testing.T) {
	serverAddr, closeServer := startQUICLifecycleServer(t)
	defer closeServer()

	dialer := &quicLifecycleDialer{}
	tlsConfig := newQUICLifecycleClientTLS(t)
	client, err := NewClient(t.Context(), dialer, M.ParseSocksaddr(serverAddr), option.V2RayQUICOptions{}, tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	c := client.(*Client)
	defer c.Close()

	firstConn, err := c.offer(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	firstConn.CloseWithError(0, "")
	eventuallyQUIC(t, time.Second, func() bool {
		return firstConn.Context().Err() != nil
	}, "first QUIC conn did not close")

	secondConn, err := c.offer(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if secondConn == firstConn {
		t.Fatal("expected a fresh QUIC conn after closing the first one")
	}
	if len(dialer.conns) != 2 {
		t.Fatalf("dial count = %d, want 2", len(dialer.conns))
	}
	if closeCount := dialer.conns[0].closeCount.Load(); closeCount != 1 {
		t.Fatalf("old raw UDP conn close count = %d, want 1", closeCount)
	}
	if closeCount := dialer.conns[1].closeCount.Load(); closeCount != 0 {
		t.Fatalf("new raw UDP conn close count before client close = %d, want 0", closeCount)
	}
}

func startQUICLifecycleServer(t *testing.T) (string, func()) {
	t.Helper()
	packetConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := quic.Listen(packetConn, &stdTLS.Config{
		Certificates: []stdTLS.Certificate{newQUICLifecycleCertificate(t)},
		NextProtos:   []string{"h3"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		for {
			conn, err := listener.Accept(ctx)
			if err != nil {
				if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
					errCh <- nil
				} else {
					errCh <- err
				}
				return
			}
			go func() {
				<-conn.Context().Done()
			}()
		}
	}()
	return packetConn.LocalAddr().String(), func() {
		cancel()
		_ = listener.Close()
		_ = packetConn.Close()
		if err := <-errCh; err != nil {
			t.Fatalf("QUIC test server failed: %v", err)
		}
	}
}

func newQUICLifecycleClientTLS(t *testing.T) tls.Config {
	t.Helper()
	config, err := tls.NewClient(t.Context(), nil, "127.0.0.1", option.OutboundTLSOptions{
		Enabled:  true,
		Insecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func newQUICLifecycleCertificate(t *testing.T) stdTLS.Certificate {
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

func eventuallyQUIC(t *testing.T, timeout time.Duration, condition func() bool, message string) {
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
