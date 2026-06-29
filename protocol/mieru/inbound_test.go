package mieru

import (
	"context"
	"net"
	"testing"
	"time"

	mieruconstant "github.com/enfein/mieru/v3/apis/constant"
	mierumodel "github.com/enfein/mieru/v3/apis/model"
	"github.com/singlink/singlink/log"
)

func TestInboundRejectsUnsupportedCommandBeforeSuccess(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	inbound := &Inbound{
		ctx:    context.Background(),
		logger: log.NewNOPFactory().Logger(),
	}

	done := make(chan struct{})
	go func() {
		inbound.handleConnection(serverConn, &mierumodel.Request{
			Command: mieruconstant.Socks5BindCmd,
			DstAddr: mierumodel.AddrSpec{
				IP:   net.IPv4(192, 0, 2, 1),
				Port: 443,
			},
		})
		close(done)
	}()

	if err := clientConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var response mierumodel.Response
	if err := response.ReadFromSocks5(clientConn); err != nil {
		t.Fatal(err)
	}
	if response.Reply != mieruconstant.Socks5ReplyCommandNotSupported {
		t.Fatalf("unexpected reply: %d", response.Reply)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for unsupported command handler")
	}
}
