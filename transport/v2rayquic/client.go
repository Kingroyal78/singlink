//go:build with_quic

package v2rayquic

import (
	"context"
	"net"
	"sync"

	"github.com/sagernet/quic-go"
	"github.com/sagernet/quic-go/http3"
	"github.com/sagernet/sing-quic"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/singlink/singlink/adapter"
	"github.com/singlink/singlink/common/tls"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/option"
)

var _ adapter.V2RayClientTransport = (*Client)(nil)

type Client struct {
	ctx        context.Context
	dialer     N.Dialer
	serverAddr M.Socksaddr
	tlsConfig  tls.Config
	quicConfig *quic.Config
	connAccess sync.Mutex
	conn       common.TypedValue[*quic.Conn]
	rawConn    net.Conn
}

func NewClient(ctx context.Context, dialer N.Dialer, serverAddr M.Socksaddr, options option.V2RayQUICOptions, tlsConfig tls.Config) (adapter.V2RayClientTransport, error) {
	quicConfig := &quic.Config{
		DisablePathMTUDiscovery: !C.IsLinux && !C.IsWindows,
	}
	if len(tlsConfig.NextProtos()) == 0 {
		tlsConfig.SetNextProtos([]string{http3.NextProtoH3})
	}
	return &Client{
		ctx:        ctx,
		dialer:     dialer,
		serverAddr: serverAddr,
		tlsConfig:  tlsConfig,
		quicConfig: quicConfig,
	}, nil
}

func (c *Client) offer(ctx context.Context) (*quic.Conn, error) {
	conn := c.conn.Load()
	if conn != nil && !common.Done(conn.Context()) {
		return conn, nil
	}
	c.connAccess.Lock()
	defer c.connAccess.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	conn = c.conn.Load()
	if conn != nil && !common.Done(conn.Context()) {
		return conn, nil
	}
	conn, err := c.offerNew(ctx)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (c *Client) offerNew(ctx context.Context) (*quic.Conn, error) {
	udpConn, err := c.dialer.DialContext(ctx, "udp", c.serverAddr)
	if err != nil {
		return nil, err
	}
	packetConn := bufio.NewUnbindPacketConn(udpConn)
	quicConn, err := qtls.Dial(ctx, packetConn, udpConn.RemoteAddr(), c.tlsConfig, c.quicConfig)
	if err != nil {
		packetConn.Close()
		return nil, err
	}
	oldRawConn := c.rawConn
	c.conn.Store(quicConn)
	c.rawConn = udpConn
	if oldRawConn != nil {
		oldRawConn.Close()
	}
	return quicConn, nil
}

func (c *Client) DialContext(ctx context.Context) (net.Conn, error) {
	ctx, cancel := mergeContexts(c.ctx, ctx)
	defer cancel()
	conn, err := c.offer(ctx)
	if err != nil {
		return nil, err
	}
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	return &StreamWrapper{Conn: conn, Stream: stream}, nil
}

func mergeContexts(parent context.Context, child context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if child == nil {
		child = context.Background()
	}
	ctx, cancel := context.WithCancel(child)
	go func() {
		select {
		case <-parent.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func (c *Client) Close() error {
	c.connAccess.Lock()
	defer c.connAccess.Unlock()
	conn := c.conn.Swap(nil)
	if conn != nil {
		conn.CloseWithError(0, "")
	}
	if c.rawConn != nil {
		c.rawConn.Close()
	}
	c.rawConn = nil
	return nil
}
