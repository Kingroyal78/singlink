package socks

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	N "github.com/sagernet/sing/common/network"
	"github.com/singlink/singlink/adapter"
	inboundAdapter "github.com/singlink/singlink/adapter/inbound"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/log"
)

func TestInboundKeepsHandshakeDeadlineUntilSOCKSConnectSuccess(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	serverConnWithDeadline := &deadlineRecordingConn{Conn: serverConn}
	router := &blockingConnectionRouter{
		routed:  make(chan net.Conn, 1),
		release: make(chan struct{}),
	}
	inbound := &Inbound{
		Adapter:    inboundAdapter.NewAdapter(C.TypeSOCKS, "test"),
		router:     router,
		logger:     log.NewNOPFactory().Logger(),
		udpTimeout: C.UDPTimeout,
	}

	handlerDone := make(chan struct{})
	go func() {
		inbound.NewConnection(context.Background(), serverConnWithDeadline, adapter.InboundContext{}, nil)
		close(handlerDone)
	}()

	clientConn.SetDeadline(time.Now().Add(time.Second))
	if _, err := clientConn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatal(err)
	}
	authResponse := make([]byte, 2)
	if _, err := io.ReadFull(clientConn, authResponse); err != nil {
		t.Fatal(err)
	}
	if authResponse[0] != 0x05 || authResponse[1] != 0x00 {
		t.Fatalf("unexpected auth response: %v", authResponse)
	}
	if _, err := clientConn.Write([]byte{0x05, 0x01, 0x00, 0x01, 192, 0, 2, 1, 0x01, 0xbb}); err != nil {
		t.Fatal(err)
	}

	var routedConn net.Conn
	select {
	case routedConn = <-router.routed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for routed SOCKS connection")
	}
	if serverConnWithDeadline.wasCleared() {
		t.Fatal("handshake deadline was cleared before SOCKS success response")
	}

	readResponseDone := make(chan error, 1)
	go func() {
		response := make([]byte, 10)
		_, err := io.ReadFull(clientConn, response)
		if err == nil && (response[0] != 0x05 || response[1] != 0x00) {
			err = io.ErrUnexpectedEOF
		}
		readResponseDone <- err
	}()
	err := N.ReportConnHandshakeSuccess(routedConn, &staticAddrConn{
		localAddr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 443},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-readResponseDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SOCKS success response")
	}
	if !serverConnWithDeadline.wasCleared() {
		t.Fatal("handshake deadline was not cleared after SOCKS success response")
	}

	close(router.release)
	serverConnWithDeadline.Close()
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for inbound handler to exit")
	}
}

type blockingConnectionRouter struct {
	routed  chan net.Conn
	release chan struct{}
}

func (r *blockingConnectionRouter) RouteConnection(context.Context, net.Conn, adapter.InboundContext) error {
	return nil
}

func (r *blockingConnectionRouter) RoutePacketConnection(context.Context, N.PacketConn, adapter.InboundContext) error {
	return nil
}

func (r *blockingConnectionRouter) RouteConnectionEx(_ context.Context, conn net.Conn, _ adapter.InboundContext, _ N.CloseHandlerFunc) {
	r.routed <- conn
	<-r.release
}

func (r *blockingConnectionRouter) RoutePacketConnectionEx(context.Context, N.PacketConn, adapter.InboundContext, N.CloseHandlerFunc) {
}

type deadlineRecordingConn struct {
	net.Conn
	access    sync.Mutex
	deadlines []time.Time
}

func (c *deadlineRecordingConn) SetDeadline(deadline time.Time) error {
	c.access.Lock()
	c.deadlines = append(c.deadlines, deadline)
	c.access.Unlock()
	return c.Conn.SetDeadline(deadline)
}

func (c *deadlineRecordingConn) wasCleared() bool {
	c.access.Lock()
	defer c.access.Unlock()
	for _, deadline := range c.deadlines {
		if deadline.IsZero() {
			return true
		}
	}
	return false
}

type staticAddrConn struct {
	localAddr net.Addr
}

func (c *staticAddrConn) Read([]byte) (int, error) {
	return 0, net.ErrClosed
}

func (c *staticAddrConn) Write([]byte) (int, error) {
	return 0, net.ErrClosed
}

func (c *staticAddrConn) Close() error {
	return nil
}

func (c *staticAddrConn) LocalAddr() net.Addr {
	return c.localAddr
}

func (c *staticAddrConn) RemoteAddr() net.Addr {
	return c.localAddr
}

func (c *staticAddrConn) SetDeadline(time.Time) error {
	return nil
}

func (c *staticAddrConn) SetReadDeadline(time.Time) error {
	return nil
}

func (c *staticAddrConn) SetWriteDeadline(time.Time) error {
	return nil
}
