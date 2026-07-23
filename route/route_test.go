package route

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/singlink/singlink/adapter"
	"github.com/singlink/singlink/common/sniff"
	C "github.com/singlink/singlink/constant"
	boxLog "github.com/singlink/singlink/log"
	R "github.com/singlink/singlink/route/rule"

	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type testRule struct {
	action adapter.RuleAction
}

func (r testRule) Match(metadata *adapter.InboundContext) bool {
	return true
}

func (r testRule) String() string {
	return ""
}

func (r testRule) Start() error {
	return nil
}

func (r testRule) Close() error {
	return nil
}

func (r testRule) Type() string {
	return C.RuleTypeDefault
}

func (r testRule) Action() adapter.RuleAction {
	return r.action
}

type testDNSTransportManager struct{}

func (m testDNSTransportManager) Start(adapter.StartStage) error {
	return nil
}

func (m testDNSTransportManager) Close() error {
	return nil
}

func (m testDNSTransportManager) Transports() []adapter.DNSTransport {
	return nil
}

func (m testDNSTransportManager) Transport(tag string) (adapter.DNSTransport, bool) {
	return nil, false
}

func (m testDNSTransportManager) Default() adapter.DNSTransport {
	return nil
}

func (m testDNSTransportManager) FakeIP() adapter.FakeIPTransport {
	return nil
}

func (m testDNSTransportManager) Remove(tag string) error {
	return nil
}

func (m testDNSTransportManager) Create(ctx context.Context, logger boxLog.ContextLogger, tag string, outboundType string, options any) error {
	return nil
}

type testReadConn struct {
	*bytes.Reader
}

func (c *testReadConn) Read(p []byte) (int, error) {
	return c.Reader.Read(p)
}

func (c *testReadConn) Write(p []byte) (int, error) {
	return len(p), nil
}

func (c *testReadConn) Close() error {
	return nil
}

func (c *testReadConn) LocalAddr() net.Addr {
	return M.Socksaddr{}
}

func (c *testReadConn) RemoteAddr() net.Addr {
	return M.Socksaddr{}
}

func (c *testReadConn) SetDeadline(t time.Time) error {
	return nil
}

func (c *testReadConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *testReadConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func TestMatchRuleDropsCachedBuffersOnFatalError(t *testing.T) {
	sniffErr := errors.New("sniff failed")
	router := &Router{
		ctx:          t.Context(),
		logger:       boxLog.NewNOPFactory().Logger(),
		dnsTransport: testDNSTransportManager{},
		rules: []adapter.Rule{
			testRule{action: &R.RuleActionSniff{
				StreamSniffers: []sniff.StreamSniffer{
					func(ctx context.Context, metadata *adapter.InboundContext, reader io.Reader) error {
						_, _ = io.ReadAll(reader)
						return sniffErr
					},
				},
				Timeout: time.Second,
			}},
			testRule{action: &R.RuleActionResolve{Server: "missing"}},
		},
	}
	metadata := &adapter.InboundContext{
		Network:     N.NetworkTCP,
		Domain:      "example.com",
		Destination: M.ParseSocksaddr("example.com:443"),
	}

	_, _, buffers, packetBuffers, err := router.matchRule(
		t.Context(),
		metadata,
		false,
		false,
		&testReadConn{Reader: bytes.NewReader([]byte("payload"))},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "DNS server not found") {
		t.Fatalf("matchRule error = %v, want DNS server not found", err)
	}
	if len(buffers) > 0 {
		t.Fatalf("returned %d stream buffers after fatal error", len(buffers))
	}
	if len(packetBuffers) > 0 {
		t.Fatalf("returned %d packet buffers after fatal error", len(packetBuffers))
	}
}
