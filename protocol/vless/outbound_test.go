package vless

import (
	"context"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/singlink/singlink/log"
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

type closeTrackingDialer struct {
	closed chan struct{}
}

func (d *closeTrackingDialer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	clientConn, serverConn := net.Pipe()
	conn := &trackingConn{Conn: clientConn, closed: d.closed}
	go func() {
		<-d.closed
		serverConn.Close()
	}()
	return conn, nil
}

func (d *closeTrackingDialer) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	return nil, os.ErrInvalid
}

func newPacketAddrTestDialer(dialer N.Dialer) *vlessDialer {
	outbound := &Outbound{
		logger:     log.NewNOPFactory().NewLogger("vless"),
		dialer:     dialer,
		serverAddr: M.ParseSocksaddr("server.example:443"),
		packetAddr: true,
	}
	return (*vlessDialer)(outbound)
}

func TestDialContextPacketAddrDomainClosesConn(t *testing.T) {
	closed := make(chan struct{})
	dialer := newPacketAddrTestDialer(&closeTrackingDialer{closed: closed})

	_, err := dialer.DialContext(t.Context(), N.NetworkUDP, M.Socksaddr{Fqdn: "target.example", Port: 53})
	if err == nil {
		t.Fatal("DialContext unexpectedly succeeded")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("packetaddr domain error did not close the underlying conn")
	}
}

func TestListenPacketPacketAddrDomainClosesConn(t *testing.T) {
	closed := make(chan struct{})
	dialer := newPacketAddrTestDialer(&closeTrackingDialer{closed: closed})

	_, err := dialer.ListenPacket(t.Context(), M.Socksaddr{Fqdn: "target.example", Port: 53})
	if err == nil {
		t.Fatal("ListenPacket unexpectedly succeeded")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("packetaddr domain error did not close the underlying conn")
	}
}
