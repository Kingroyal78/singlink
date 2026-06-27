package redirect

import (
	"errors"
	"net"
	"net/netip"
	"testing"

	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/singlink/singlink/common/listener"
)

func TestTProxyPacketWriterClosesCachedConnOnWriteError(t *testing.T) {
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}

	destination := M.ParseSocksaddrHostPort("127.0.0.1", 12345)
	writer := &tproxyPacketWriter{
		listener:    &listener.Listener{},
		source:      netip.AddrPort{},
		destination: destination,
		conn:        udpConn,
	}

	err = writer.WritePacket(buf.As([]byte("x")), destination)
	if err == nil {
		t.Fatal("expected write error")
	}
	if writer.conn != nil {
		t.Fatal("expected cached conn to be cleared")
	}

	_, err = udpConn.WriteToUDPAddrPort([]byte("x"), netip.MustParseAddrPort("127.0.0.1:9"))
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("expected cached conn to be closed, got %v", err)
	}
}
