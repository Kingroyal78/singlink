package listener

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestCloseOnReadErrorPacketConnClosesUnderlyingConn(t *testing.T) {
	rawConn := &readErrorPacketConn{}
	conn := &closeOnReadErrorPacketConn{PacketConn: rawConn}

	_, _, err := conn.ReadFrom(make([]byte, 1))
	if !errors.Is(err, errTestPacketRead) {
		t.Fatalf("unexpected read error: %v", err)
	}
	if !rawConn.closed.Load() {
		t.Fatal("underlying packet conn was not closed")
	}
}

func TestCloseOnReadErrorPacketListenerWrapsConn(t *testing.T) {
	rawConn := &readErrorPacketConn{}
	packetListener := NewCloseOnReadErrorPacketListener(staticPacketListener{conn: rawConn})

	conn, err := packetListener.ListenPacket(net.ListenConfig{}, context.Background(), "udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, loaded := conn.(*closeOnReadErrorPacketConn)
	if !loaded {
		t.Fatalf("expected closeOnReadErrorPacketConn, got %T", conn)
	}
}

var errTestPacketRead = errors.New("read failed")

type staticPacketListener struct {
	conn net.PacketConn
}

func (l staticPacketListener) ListenPacket(net.ListenConfig, context.Context, string, string) (net.PacketConn, error) {
	return l.conn, nil
}

type readErrorPacketConn struct {
	closed atomic.Bool
}

func (c *readErrorPacketConn) ReadFrom([]byte) (int, net.Addr, error) {
	return 0, nil, errTestPacketRead
}

func (c *readErrorPacketConn) WriteTo([]byte, net.Addr) (int, error) {
	return 0, net.ErrClosed
}

func (c *readErrorPacketConn) Close() error {
	c.closed.Store(true)
	return nil
}

func (c *readErrorPacketConn) LocalAddr() net.Addr {
	return dummyAddr("local")
}

func (c *readErrorPacketConn) SetDeadline(time.Time) error {
	return nil
}

func (c *readErrorPacketConn) SetReadDeadline(time.Time) error {
	return nil
}

func (c *readErrorPacketConn) SetWriteDeadline(time.Time) error {
	return nil
}
