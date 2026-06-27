package vmess

import (
	"context"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
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

func TestListenPacketPacketAddrDomainClosesConn(t *testing.T) {
	closed := make(chan struct{})
	outbound := &Outbound{
		logger:     log.NewNOPFactory().NewLogger("vmess"),
		dialer:     &closeTrackingDialer{closed: closed},
		serverAddr: M.ParseSocksaddr("server.example:443"),
		packetAddr: true,
	}
	dialer := (*vmessDialer)(outbound)

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
