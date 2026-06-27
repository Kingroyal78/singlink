package listener

import (
	"context"
	"net"
	"sync"
)

type PacketListener interface {
	ListenPacket(listenConfig net.ListenConfig, ctx context.Context, network string, address string) (net.PacketConn, error)
}

func NewCloseOnReadErrorPacketListener(listener PacketListener) PacketListener {
	return &closeOnReadErrorPacketListener{
		listener: listener,
	}
}

type closeOnReadErrorPacketListener struct {
	listener PacketListener
}

func (l *closeOnReadErrorPacketListener) ListenPacket(listenConfig net.ListenConfig, ctx context.Context, network string, address string) (net.PacketConn, error) {
	conn, err := l.listener.ListenPacket(listenConfig, ctx, network, address)
	if err != nil {
		return nil, err
	}
	return &closeOnReadErrorPacketConn{PacketConn: conn}, nil
}

type closeOnReadErrorPacketConn struct {
	net.PacketConn
	closeOnce sync.Once
}

func (c *closeOnReadErrorPacketConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	n, addr, err = c.PacketConn.ReadFrom(p)
	if err != nil {
		c.closeOnce.Do(func() {
			_ = c.PacketConn.Close()
		})
	}
	return
}
