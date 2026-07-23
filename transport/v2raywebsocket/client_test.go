package v2raywebsocket

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/sagernet/sing/common/buf"
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
	onDial func(net.Conn)
}

func (d *testDialer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	clientConn, serverConn := net.Pipe()
	conn := &trackingConn{Conn: clientConn, closed: make(chan struct{})}
	if d.onDial != nil {
		go d.onDial(serverConn)
	} else {
		go func() {
			defer serverConn.Close()
			request, err := http.ReadRequest(bufio.NewReader(serverConn))
			if err == nil {
				request.Body.Close()
				_, _ = fmt.Fprint(serverConn, "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n")
			}
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

func TestDialContextPreservesDataBufferedWithUpgradeResponse(t *testing.T) {
	payload := []byte("early-websocket-data")
	dialer := &testDialer{
		onDial: func(conn net.Conn) {
			defer conn.Close()
			request, err := http.ReadRequest(bufio.NewReader(conn))
			if err != nil {
				return
			}
			request.Body.Close()
			key := request.Header.Get("Sec-Websocket-Key")
			sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
			frame := append([]byte{0x82, byte(len(payload))}, payload...)
			var response bytes.Buffer
			_, _ = fmt.Fprintf(&response,
				"HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: %s\r\n\r\n",
				base64.StdEncoding.EncodeToString(sum[:]),
			)
			response.Write(frame)
			_, _ = conn.Write(response.Bytes())
		},
	}
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
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	received := make([]byte, len(payload))
	if _, err = io.ReadFull(conn, received); err != nil {
		t.Fatal(err)
	}
	if string(received) != string(payload) {
		t.Fatalf("received %q, want %q", received, payload)
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

func TestEarlyWebsocketWriteBufferAfterCloseReleasesBuffer(t *testing.T) {
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
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
	buffer := newManagedBuffer(t, []byte("hello"))
	err = conn.(*EarlyWebsocketConn).WriteBuffer(buffer)
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("WriteBuffer after Close error = %v, want net.ErrClosed", err)
	}
	requireReleased(t, buffer)
}

func TestEarlyWebsocketInitialWriteBufferReleasesBuffer(t *testing.T) {
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
		maxEarlyData: 8,
	}

	conn, err := client.DialContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	buffer := newManagedBuffer(t, []byte("hello"))
	if err = conn.(*EarlyWebsocketConn).WriteBuffer(buffer); err != nil {
		t.Fatal(err)
	}
	requireReleased(t, buffer)
}

func newManagedBuffer(t *testing.T, payload []byte) *buf.Buffer {
	t.Helper()
	buffer := buf.NewSize(len(payload))
	if _, err := buffer.Write(payload); err != nil {
		t.Fatal(err)
	}
	return buffer
}

func requireReleased(t *testing.T, buffer *buf.Buffer) {
	t.Helper()
	if buffer.Cap() != 0 {
		t.Fatalf("buffer was not released: cap = %d", buffer.Cap())
	}
}
