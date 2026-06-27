package listener

import (
	"context"
	"net"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/singlink/singlink/adapter"
)

func TestHandshakeDeadlineConnClearsDeadlineOnSuccess(t *testing.T) {
	rawConn := new(deadlineRecorderConn)
	conn := newHandshakeDeadlineConn(rawConn)

	deadline := time.Now().Add(time.Minute)
	if err := conn.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if err := N.ReportConnHandshakeSuccess(conn, rawConn); err != nil {
		t.Fatal(err)
	}

	if len(rawConn.deadlines) != 2 {
		t.Fatalf("expected two deadline calls, got %d", len(rawConn.deadlines))
	}
	if !rawConn.deadlines[0].Equal(deadline) {
		t.Fatalf("unexpected handshake deadline: %v", rawConn.deadlines[0])
	}
	if !rawConn.deadlines[1].IsZero() {
		t.Fatalf("expected cleared deadline, got %v", rawConn.deadlines[1])
	}
}

func TestClearHandshakeDeadlineThroughWrapper(t *testing.T) {
	rawConn := new(deadlineRecorderConn)
	conn := newHandshakeDeadlineConn(rawConn)

	if err := ClearHandshakeDeadline(conn); err != nil {
		t.Fatal(err)
	}
	if len(rawConn.deadlines) != 1 {
		t.Fatalf("expected one deadline call, got %d", len(rawConn.deadlines))
	}
	if !rawConn.deadlines[0].IsZero() {
		t.Fatalf("expected cleared deadline, got %v", rawConn.deadlines[0])
	}
}

func TestHandshakeDeadlineUpstreamHandlerClearsDeadline(t *testing.T) {
	rawConn := new(deadlineRecorderConn)
	handler := NewHandshakeDeadlineUpstreamHandler(rawConn, recordingUpstreamHandler{})

	handler.NewConnectionEx(context.Background(), rawConn, M.Socksaddr{}, M.Socksaddr{}, nil)

	if len(rawConn.deadlines) != 1 {
		t.Fatalf("expected one deadline call, got %d", len(rawConn.deadlines))
	}
	if !rawConn.deadlines[0].IsZero() {
		t.Fatalf("expected cleared deadline, got %v", rawConn.deadlines[0])
	}
}

func TestHandshakeSuccessDeadlineUpstreamHandlerClearsDeadlineOnConnSuccess(t *testing.T) {
	rawConn := new(deadlineRecorderConn)
	innerConn := &recordingHandshakeConn{Conn: rawConn}
	var routedConn net.Conn
	handler := NewHandshakeSuccessDeadlineUpstreamHandler(rawConn, recordingUpstreamHandler{
		onConnection: func(conn net.Conn) {
			routedConn = conn
		},
	})

	handler.NewConnectionEx(context.Background(), innerConn, M.Socksaddr{}, M.Socksaddr{}, nil)

	if routedConn == nil {
		t.Fatal("expected routed connection")
	}
	if len(rawConn.deadlines) != 0 {
		t.Fatalf("expected deadline to remain during upstream handling, got %d deadline calls", len(rawConn.deadlines))
	}
	if err := N.ReportConnHandshakeSuccess(routedConn, new(deadlineRecorderConn)); err != nil {
		t.Fatal(err)
	}
	if innerConn.connHandshakeSuccesses != 1 {
		t.Fatalf("expected inner connection handshake success once, got %d", innerConn.connHandshakeSuccesses)
	}
	if len(rawConn.deadlines) != 1 {
		t.Fatalf("expected one deadline clear, got %d", len(rawConn.deadlines))
	}
	if !rawConn.deadlines[0].IsZero() {
		t.Fatalf("expected cleared deadline, got %v", rawConn.deadlines[0])
	}
}

func TestHandshakeSuccessDeadlineUpstreamHandlerClearsDeadlineForPacketConnection(t *testing.T) {
	rawConn := new(deadlineRecorderConn)
	handler := NewHandshakeSuccessDeadlineUpstreamHandler(rawConn, recordingUpstreamHandler{})

	handler.NewPacketConnectionEx(context.Background(), nil, M.Socksaddr{}, M.Socksaddr{}, nil)

	if len(rawConn.deadlines) != 1 {
		t.Fatalf("expected one deadline clear, got %d", len(rawConn.deadlines))
	}
	if !rawConn.deadlines[0].IsZero() {
		t.Fatalf("expected cleared deadline, got %v", rawConn.deadlines[0])
	}
}

type deadlineRecorderConn struct {
	deadlines []time.Time
}

func (c *deadlineRecorderConn) Read([]byte) (int, error) {
	return 0, net.ErrClosed
}

func (c *deadlineRecorderConn) Write([]byte) (int, error) {
	return 0, net.ErrClosed
}

func (c *deadlineRecorderConn) Close() error {
	return nil
}

func (c *deadlineRecorderConn) LocalAddr() net.Addr {
	return dummyAddr("local")
}

func (c *deadlineRecorderConn) RemoteAddr() net.Addr {
	return dummyAddr("remote")
}

func (c *deadlineRecorderConn) SetDeadline(deadline time.Time) error {
	c.deadlines = append(c.deadlines, deadline)
	return nil
}

func (c *deadlineRecorderConn) SetReadDeadline(time.Time) error {
	return nil
}

func (c *deadlineRecorderConn) SetWriteDeadline(time.Time) error {
	return nil
}

type dummyAddr string

func (a dummyAddr) Network() string {
	return string(a)
}

func (a dummyAddr) String() string {
	return string(a)
}

type recordingUpstreamHandler struct {
	onConnection func(net.Conn)
}

func (h recordingUpstreamHandler) NewConnectionEx(_ context.Context, conn net.Conn, _ M.Socksaddr, _ M.Socksaddr, _ N.CloseHandlerFunc) {
	if h.onConnection != nil {
		h.onConnection(conn)
	}
}

func (recordingUpstreamHandler) NewPacketConnectionEx(context.Context, N.PacketConn, M.Socksaddr, M.Socksaddr, N.CloseHandlerFunc) {
}

var _ adapter.UpstreamHandlerAdapter = recordingUpstreamHandler{}

type recordingHandshakeConn struct {
	net.Conn
	connHandshakeSuccesses int
}

func (c *recordingHandshakeConn) ConnHandshakeSuccess(net.Conn) error {
	c.connHandshakeSuccesses++
	return nil
}
