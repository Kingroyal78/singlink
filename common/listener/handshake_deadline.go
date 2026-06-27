package listener

import (
	"context"
	"net"
	"time"

	"github.com/sagernet/sing/common"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/singlink/singlink/adapter"
)

type HandshakeDeadlineConn interface {
	ClearHandshakeDeadline() error
}

type handshakeDeadlineConn struct {
	net.Conn
}

func newHandshakeDeadlineConn(conn net.Conn) net.Conn {
	return &handshakeDeadlineConn{Conn: conn}
}

func (c *handshakeDeadlineConn) Upstream() any {
	return c.Conn
}

func (c *handshakeDeadlineConn) ClearHandshakeDeadline() error {
	return c.Conn.SetDeadline(time.Time{})
}

func (c *handshakeDeadlineConn) HandshakeSuccess() error {
	return c.ClearHandshakeDeadline()
}

func (c *handshakeDeadlineConn) ConnHandshakeSuccess(conn net.Conn) error {
	return c.ClearHandshakeDeadline()
}

func ClearHandshakeDeadline(conn net.Conn) error {
	if deadlineConn, loaded := common.Cast[HandshakeDeadlineConn](conn); loaded {
		return deadlineConn.ClearHandshakeDeadline()
	}
	return conn.SetDeadline(time.Time{})
}

func SetHandshakeDeadline(conn net.Conn, timeout time.Duration) error {
	if timeout <= 0 {
		return nil
	}
	return conn.SetDeadline(time.Now().Add(timeout))
}

func NewHandshakeDeadlineUpstreamHandler(conn net.Conn, handler adapter.UpstreamHandlerAdapter) adapter.UpstreamHandlerAdapter {
	return &handshakeDeadlineUpstreamHandler{
		conn:    conn,
		handler: handler,
	}
}

func NewHandshakeSuccessDeadlineUpstreamHandler(conn net.Conn, handler adapter.UpstreamHandlerAdapter) adapter.UpstreamHandlerAdapter {
	return &handshakeSuccessDeadlineUpstreamHandler{
		conn:    conn,
		handler: handler,
	}
}

type handshakeDeadlineUpstreamHandler struct {
	conn    net.Conn
	handler adapter.UpstreamHandlerAdapter
}

func (h *handshakeDeadlineUpstreamHandler) NewConnectionEx(ctx context.Context, conn net.Conn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	_ = ClearHandshakeDeadline(h.conn)
	h.handler.NewConnectionEx(ctx, conn, source, destination, onClose)
}

func (h *handshakeDeadlineUpstreamHandler) NewPacketConnectionEx(ctx context.Context, conn N.PacketConn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	_ = ClearHandshakeDeadline(h.conn)
	h.handler.NewPacketConnectionEx(ctx, conn, source, destination, onClose)
}

type handshakeSuccessDeadlineUpstreamHandler struct {
	conn    net.Conn
	handler adapter.UpstreamHandlerAdapter
}

func (h *handshakeSuccessDeadlineUpstreamHandler) NewConnectionEx(ctx context.Context, conn net.Conn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	h.handler.NewConnectionEx(ctx, &handshakeSuccessDeadlineConn{
		Conn:         conn,
		deadlineConn: h.conn,
	}, source, destination, onClose)
}

func (h *handshakeSuccessDeadlineUpstreamHandler) NewPacketConnectionEx(ctx context.Context, conn N.PacketConn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	_ = ClearHandshakeDeadline(h.conn)
	h.handler.NewPacketConnectionEx(ctx, conn, source, destination, onClose)
}

type handshakeSuccessDeadlineConn struct {
	net.Conn
	deadlineConn net.Conn
}

func (c *handshakeSuccessDeadlineConn) Upstream() any {
	return c.Conn
}

func (c *handshakeSuccessDeadlineConn) ConnHandshakeSuccess(conn net.Conn) error {
	err := N.ReportConnHandshakeSuccess(c.Conn, conn)
	if err != nil {
		return err
	}
	return ClearHandshakeDeadline(c.deadlineConn)
}
