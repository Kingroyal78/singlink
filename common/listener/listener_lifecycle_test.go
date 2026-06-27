package listener

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"
)

func TestCloseWaitsForInflightUDPPacketHandler(t *testing.T) {
	handler := newBlockingPacketHandler()
	listener := New(Options{
		Context:       context.Background(),
		Logger:        log.NewNOPFactory().Logger(),
		Network:       []string{N.NetworkUDP},
		Listen:        option.ListenOptions{},
		PacketHandler: handler,
	})
	if err := listener.Start(); err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	clientConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()
	_, err = clientConn.WriteToUDPAddrPort([]byte("packet"), listener.UDPConn().LocalAddr().(*net.UDPAddr).AddrPort())
	if err != nil {
		t.Fatal(err)
	}

	<-handler.entered

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- listener.Close()
	}()

	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before UDP handler drained: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(handler.release)

	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not return after UDP handler drained")
	}
}

type blockingPacketHandler struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingPacketHandler() *blockingPacketHandler {
	return &blockingPacketHandler{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (h *blockingPacketHandler) NewPacket(buffer *buf.Buffer, source M.Socksaddr) {
	h.once.Do(func() {
		close(h.entered)
	})
	<-h.release
}
