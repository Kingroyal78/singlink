package v2raywebsocket

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/ws"
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
}

func (d *testDialer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	clientConn, serverConn := net.Pipe()
	conn := &trackingConn{Conn: clientConn, closed: make(chan struct{})}
	go func() {
		defer serverConn.Close()
		request, err := http.ReadRequest(bufio.NewReader(serverConn))
		if err == nil {
			request.Body.Close()
			_, _ = fmt.Fprint(serverConn, "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n")
		}
	}()
	d.dialed <- conn
	return conn, nil
}

func (d *testDialer) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	return nil, os.ErrInvalid
}

func TestDialContextHandshakeFailureClosesConn(t *testing.T) {
	dialer := &testDialer{dialed: make(chan *trackingConn, 1)}
	client := &Client{
		dialer:     dialer,
		serverAddr: M.ParseSocksaddr("example.com:443"),
		requestURL: url.URL{
			Scheme: "ws",
			Host:   "example.com",
			Path:   "/",
		},
		headers: http.Header{},
	}

	conn, err := client.DialContext(t.Context())
	if err == nil {
		conn.Close()
		t.Fatal("DialContext unexpectedly succeeded")
	}
	trackedConn := <-dialer.dialed
	select {
	case <-trackedConn.closed:
	case <-time.After(time.Second):
		t.Fatal("failed websocket handshake did not close the underlying conn")
	}
}

func TestEarlyWebsocketCloseBeforeWriteWakesReadAndPreventsDial(t *testing.T) {
	dialer := &testDialer{dialed: make(chan *trackingConn, 1)}
	client := &Client{
		dialer:     dialer,
		serverAddr: M.ParseSocksaddr("example.com:443"),
		requestURL: url.URL{
			Scheme: "ws",
			Host:   "example.com",
			Path:   "/",
		},
		headers:      http.Header{},
		maxEarlyData: 8,
	}

	conn, err := client.DialContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	readErr := make(chan error, 1)
	go func() {
		_, err := conn.Read(make([]byte, 1))
		readErr <- err
	}()

	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-readErr:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Read error = %v, want net.ErrClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Read was not woken by Close before websocket creation")
	}

	_, err = conn.Write([]byte("hello"))
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Write after Close error = %v, want net.ErrClosed", err)
	}
	select {
	case <-dialer.dialed:
		t.Fatal("Write after Close unexpectedly dialed")
	default:
	}
}

type closeAfterUpgradeDialer struct {
	dialed chan *trackingConn
}

func (d *closeAfterUpgradeDialer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	clientConn, serverConn := net.Pipe()
	conn := &trackingConn{Conn: clientConn, closed: make(chan struct{})}
	go func() {
		defer serverConn.Close()
		_, _ = ws.Upgrader{}.Upgrade(serverConn)
	}()
	d.dialed <- conn
	return conn, nil
}

func (d *closeAfterUpgradeDialer) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	return nil, os.ErrInvalid
}

func TestEarlyWebsocketLateDataWriteFailureClosesDialedConn(t *testing.T) {
	dialer := &closeAfterUpgradeDialer{dialed: make(chan *trackingConn, 1)}
	client := &Client{
		dialer:     dialer,
		serverAddr: M.ParseSocksaddr("example.com:443"),
		requestURL: url.URL{
			Scheme: "ws",
			Host:   "example.com",
			Path:   "/",
		},
		headers:      http.Header{},
		maxEarlyData: 2,
	}

	conn, err := client.DialContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.Write([]byte("hello"))
	if err == nil {
		conn.Close()
		t.Fatal("Write unexpectedly succeeded")
	}

	trackedConn := <-dialer.dialed
	select {
	case <-trackedConn.closed:
	case <-time.After(time.Second):
		t.Fatal("late-data write failure did not close the dialed websocket conn")
	}
}
