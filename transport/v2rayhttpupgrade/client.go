package v2rayhttpupgrade

import (
	std_bufio "bufio"
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	"github.com/sagernet/sing/common/bufio/deadline"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	sHTTP "github.com/sagernet/sing/protocol/http"
	"github.com/singlink/singlink/adapter"
	"github.com/singlink/singlink/common/tls"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/option"
)

var _ adapter.V2RayClientTransport = (*Client)(nil)

type Client struct {
	dialer     N.Dialer
	serverAddr M.Socksaddr
	requestURL url.URL
	headers    http.Header
	host       string
}

func NewClient(ctx context.Context, dialer N.Dialer, serverAddr M.Socksaddr, options option.V2RayHTTPUpgradeOptions, tlsConfig tls.Config) (*Client, error) {
	if tlsConfig != nil {
		if len(tlsConfig.NextProtos()) == 0 {
			tlsConfig.SetNextProtos([]string{"http/1.1"})
		}
		dialer = tls.NewDialer(dialer, tlsConfig)
	}
	var host string
	if options.Host != "" {
		host = options.Host
	} else if tlsConfig != nil && tlsConfig.ServerName() != "" {
		host = tlsConfig.ServerName()
	} else {
		host = serverAddr.String()
	}
	var requestURL url.URL
	if tlsConfig == nil {
		requestURL.Scheme = "http"
	} else {
		requestURL.Scheme = "https"
	}
	requestURL.Host = serverAddr.String()
	requestURL.Path = options.Path
	err := sHTTP.URLSetPath(&requestURL, options.Path)
	if err != nil {
		return nil, E.Cause(err, "parse path")
	}
	if !strings.HasPrefix(requestURL.Path, "/") {
		requestURL.Path = "/" + requestURL.Path
	}
	headers := make(http.Header)
	for key, value := range options.Headers {
		headers[key] = value
	}
	return &Client{
		dialer:     dialer,
		serverAddr: serverAddr,
		requestURL: requestURL,
		headers:    headers,
		host:       host,
	}, nil
}

func (c *Client) DialContext(ctx context.Context) (net.Conn, error) {
	conn, err := c.dialer.DialContext(ctx, N.NetworkTCP, c.serverAddr)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			conn.Close()
		}
	}()
	deadlineConn, stopDeadline := armHandshakeDeadline(ctx, conn)
	defer stopDeadline()
	request := &http.Request{
		Method: http.MethodGet,
		URL:    &c.requestURL,
		Header: c.headers.Clone(),
		Host:   c.host,
	}
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	err = request.Write(deadlineConn)
	if err != nil {
		return nil, err
	}
	bufReader := std_bufio.NewReader(deadlineConn)
	response, err := http.ReadResponse(bufReader, request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != 101 ||
		!strings.EqualFold(response.Header.Get("Connection"), "upgrade") ||
		!strings.EqualFold(response.Header.Get("Upgrade"), "websocket") {
		return nil, E.New("v2ray-http-upgrade: unexpected status: ", response.Status)
	}
	if bufReader.Buffered() > 0 {
		cachedLen := bufReader.Buffered()
		buffer := buf.NewSize(cachedLen)
		_, err = buffer.ReadFullFrom(bufReader, cachedLen)
		if err != nil {
			return nil, err
		}
		conn = bufio.NewCachedConn(conn, buffer)
	}
	success = true
	return conn, nil
}

func armHandshakeDeadline(ctx context.Context, conn net.Conn) (net.Conn, func()) {
	var deadlineConn net.Conn
	if deadline.NeedAdditionalReadDeadline(conn) {
		deadlineConn = deadline.NewConn(conn)
	} else {
		deadlineConn = conn
	}
	deadlineConn.SetDeadline(time.Now().Add(C.TCPTimeout))
	done := make(chan struct{})
	var doneOnce sync.Once
	if ctxDone := ctx.Done(); ctxDone != nil {
		go func() {
			select {
			case <-ctxDone:
				deadlineConn.SetDeadline(time.Now())
			case <-done:
			}
		}()
	}
	return deadlineConn, func() {
		doneOnce.Do(func() {
			close(done)
			deadlineConn.SetDeadline(time.Time{})
		})
	}
}

func (c *Client) Close() error {
	return nil
}
