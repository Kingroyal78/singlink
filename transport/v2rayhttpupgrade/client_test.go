package v2rayhttpupgrade

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type trackingConn struct {
	net.Conn
	closed chan struct{}
	once   sync.Once
}

func (c *trackingConn) Close() error {
	c.once.Do(func() {
		close(c.closed)
	})
	return c.Conn.Close()
}

type testDialer struct {
	dialed chan *trackingConn
	onDial func(net.Conn)
}

func (d *testDialer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	clientConn, serverConn := net.Pipe()
	conn := &trackingConn{Conn: clientConn, closed: make(chan struct{})}
	if d.onDial != nil {
		go d.onDial(serverConn)
	} else {
		go func() {
			<-conn.closed
			serverConn.Close()
		}()
	}
	if d.dialed != nil {
		d.dialed <- conn
	}
	return conn, nil
}

func (d *testDialer) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	return nil, os.ErrInvalid
}

func newTestClient(dialer N.Dialer) *Client {
	return &Client{
		dialer:     dialer,
		serverAddr: M.ParseSocksaddr("example.com:443"),
		requestURL: url.URL{
			Scheme: "http",
			Host:   "example.com",
			Path:   "/",
		},
		headers: http.Header{},
		host:    "example.com",
	}
}

func TestDialContextCancelUnblocksWriteAndClosesConn(t *testing.T) {
	dialer := &testDialer{dialed: make(chan *trackingConn, 1)}
	client := newTestClient(dialer)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		conn, err := client.DialContext(ctx)
		if conn != nil {
			conn.Close()
		}
		done <- err
	}()
	trackedConn := <-dialer.dialed
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("DialContext unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("DialContext did not unblock after context cancellation")
	}
	select {
	case <-trackedConn.closed:
	case <-time.After(time.Second):
		t.Fatal("failed handshake did not close the underlying conn")
	}
}

func TestDialContextUnexpectedStatusClosesConn(t *testing.T) {
	dialer := &testDialer{
		dialed: make(chan *trackingConn, 1),
		onDial: func(conn net.Conn) {
			defer conn.Close()
			request, err := http.ReadRequest(bufio.NewReader(conn))
			if err == nil {
				request.Body.Close()
				_, _ = fmt.Fprint(conn, "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n")
			}
		},
	}
	client := newTestClient(dialer)

	conn, err := client.DialContext(t.Context())
	if err == nil {
		conn.Close()
		t.Fatal("DialContext unexpectedly succeeded")
	}
	trackedConn := <-dialer.dialed
	select {
	case <-trackedConn.closed:
	case <-time.After(time.Second):
		t.Fatal("failed handshake did not close the underlying conn")
	}
}

func TestDialContextPreservesDataBufferedWithUpgradeResponse(t *testing.T) {
	payload := []byte("early-response-data")
	dialer := &testDialer{
		onDial: func(conn net.Conn) {
			defer conn.Close()
			request, err := http.ReadRequest(bufio.NewReader(conn))
			if err != nil {
				return
			}
			request.Body.Close()
			_, _ = fmt.Fprintf(conn,
				"HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n%s",
				payload,
			)
		},
	}
	client := newTestClient(dialer)

	conn, err := client.DialContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	received := make([]byte, len(payload))
	if _, err = io.ReadFull(conn, received); err != nil {
		t.Fatal(err)
	}
	if string(received) != string(payload) {
		t.Fatalf("received %q, want %q", received, payload)
	}
}
