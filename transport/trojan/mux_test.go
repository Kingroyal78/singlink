package trojan

import (
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type staticConn struct {
	*bytes.Reader
}

func (c *staticConn) Read(p []byte) (int, error) {
	return c.Reader.Read(p)
}

func (c *staticConn) Write(p []byte) (int, error) {
	return len(p), nil
}

func (c *staticConn) Close() error {
	return nil
}

func (c *staticConn) LocalAddr() net.Addr {
	return M.Socksaddr{}
}

func (c *staticConn) RemoteAddr() net.Addr {
	return M.Socksaddr{}
}

func (c *staticConn) SetDeadline(t time.Time) error {
	return nil
}

func (c *staticConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *staticConn) SetWriteDeadline(t time.Time) error {
	return nil
}

type muxTestHandler struct {
	data        []byte
	destination M.Socksaddr
	err         error
}

func (h *muxTestHandler) NewConnectionEx(ctx context.Context, conn net.Conn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	h.destination = destination
	h.data, h.err = io.ReadAll(conn)
}

func (h *muxTestHandler) NewPacketConnectionEx(ctx context.Context, conn N.PacketConn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
}

func TestMuxConnectionPreservesBufferedPayload(t *testing.T) {
	destination := M.ParseSocksaddr("example.com:443")
	payload := []byte("mux-payload")
	var request bytes.Buffer
	request.WriteByte(CommandTCP)
	if err := M.SocksaddrSerializer.WriteAddrPort(&request, destination); err != nil {
		t.Fatal(err)
	}
	request.Write(payload)
	handler := &muxTestHandler{}

	err := newMuxConnection0(t.Context(), &staticConn{Reader: bytes.NewReader(request.Bytes())}, M.Socksaddr{}, handler)
	if err != nil {
		t.Fatal(err)
	}
	if handler.err != nil {
		t.Fatal(handler.err)
	}
	if handler.destination != destination {
		t.Fatalf("destination = %v, want %v", handler.destination, destination)
	}
	if string(handler.data) != string(payload) {
		t.Fatalf("payload = %q, want %q", handler.data, payload)
	}
}

func TestClientConnWriteBufferReleasesInitialBuffer(t *testing.T) {
	destination := M.ParseSocksaddr("example.com:443")
	headroom := KeyLength + M.SocksaddrSerializer.AddrPortLen(destination) + 5
	buffer := newManagedBufferWithHeadroom(t, headroom, []byte("payload"))
	conn := NewClientConn(&staticConn{Reader: bytes.NewReader(nil)}, Key("password"), destination)

	if err := conn.WriteBuffer(buffer); err != nil {
		t.Fatal(err)
	}
	requireReleased(t, buffer)
}

func TestClientPacketConnWritePacketReleasesInitialBuffer(t *testing.T) {
	destination := M.ParseSocksaddr("example.com:443")
	headroom := KeyLength + 2*M.SocksaddrSerializer.AddrPortLen(destination) + 9
	buffer := newManagedBufferWithHeadroom(t, headroom, []byte("payload"))
	conn := NewClientPacketConn(&staticConn{Reader: bytes.NewReader(nil)}, Key("password"))

	if err := conn.WritePacket(buffer, destination); err != nil {
		t.Fatal(err)
	}
	requireReleased(t, buffer)
}

func TestClientConnWriteBufferRejectsLongDestinationWithoutPanic(t *testing.T) {
	destination := M.ParseSocksaddrHostPort(strings.Repeat("a", 256), 443)
	buffer := newManagedBufferWithHeadroom(t, KeyLength+M.MaxSocksaddrLength+5, []byte("payload"))
	conn := NewClientConn(&staticConn{Reader: bytes.NewReader(nil)}, Key("password"), destination)

	if err := conn.WriteBuffer(buffer); err == nil {
		t.Fatal("WriteBuffer unexpectedly succeeded")
	}
	requireReleased(t, buffer)
}

func TestWritePacketRejectsLongDestinationWithoutPanic(t *testing.T) {
	destination := M.ParseSocksaddrHostPort(strings.Repeat("a", 256), 443)
	buffer := newManagedBufferWithHeadroom(t, M.MaxSocksaddrLength+4, []byte("payload"))

	if err := WritePacket(&staticConn{Reader: bytes.NewReader(nil)}, buffer, destination); err == nil {
		t.Fatal("WritePacket unexpectedly succeeded")
	}
	requireReleased(t, buffer)
}

func newManagedBufferWithHeadroom(t *testing.T, headroom int, payload []byte) *buf.Buffer {
	t.Helper()
	buffer := buf.NewSize(headroom + len(payload))
	buffer.Advance(headroom)
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
