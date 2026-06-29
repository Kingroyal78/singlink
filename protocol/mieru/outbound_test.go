package mieru

import (
	"context"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	mieruclient "github.com/enfein/mieru/v3/apis/client"
	mierumodel "github.com/enfein/mieru/v3/apis/model"
	"github.com/miekg/dns"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
	"github.com/singlink/singlink/adapter"
	outboundAdapter "github.com/singlink/singlink/adapter/outbound"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"
)

func TestBuildMieruClientConfigSetsResolver(t *testing.T) {
	resolver := &recordingMieruResolver{}
	config, err := buildMieruClientConfig(option.MieruOutboundOptions{
		ServerOptions: option.ServerOptions{
			Server:     "proxy.example",
			ServerPort: 8964,
		},
		Transport: "UDP",
		UserName:  "user",
		Password:  "password",
	}, mieruDialer{dialer: noopDialer{}}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if config.Resolver != resolver {
		t.Fatal("mieru resolver was not set")
	}
}

func TestBuildMieruClientConfigNormalizesBracketedIPv6Server(t *testing.T) {
	config, err := buildMieruClientConfig(option.MieruOutboundOptions{
		ServerOptions: option.ServerOptions{
			Server:     "[2001:db8::1]",
			ServerPort: 8964,
		},
		Transport: "TCP",
		UserName:  "user",
		Password:  "password",
	}, mieruDialer{dialer: noopDialer{}}, &recordingMieruResolver{})
	if err != nil {
		t.Fatal(err)
	}
	server := config.Profile.GetServers()[0]
	if server.GetIpAddress() != "2001:db8::1" {
		t.Fatalf("unexpected IP address: %q", server.GetIpAddress())
	}
	if server.GetDomainName() != "" {
		t.Fatalf("unexpected domain name: %q", server.GetDomainName())
	}
}

func TestBuildMieruClientConfigPreservesDomainServer(t *testing.T) {
	config, err := buildMieruClientConfig(option.MieruOutboundOptions{
		ServerOptions: option.ServerOptions{
			Server:     "proxy.example",
			ServerPort: 8964,
		},
		Transport: "TCP",
		UserName:  "user",
		Password:  "password",
	}, mieruDialer{dialer: noopDialer{}}, &recordingMieruResolver{})
	if err != nil {
		t.Fatal(err)
	}
	server := config.Profile.GetServers()[0]
	if server.GetDomainName() != "proxy.example" {
		t.Fatalf("unexpected domain name: %q", server.GetDomainName())
	}
	if server.GetIpAddress() != "" {
		t.Fatalf("unexpected IP address: %q", server.GetIpAddress())
	}
}

func TestMieruRouterResolverLookupIPUsesRouter(t *testing.T) {
	router := &recordingDNSRouter{
		addrs: []netip.Addr{
			netip.MustParseAddr("2001:db8::1"),
			netip.MustParseAddr("192.0.2.1"),
		},
	}
	resolver := mieruRouterResolver{
		router: router,
		queryOptions: adapter.DNSQueryOptions{
			Strategy: C.DomainStrategyPreferIPv6,
		},
	}
	ips, err := resolver.LookupIP(context.Background(), "ip4", "Proxy.Example.")
	if err != nil {
		t.Fatal(err)
	}
	if router.domain != "Proxy.Example" {
		t.Fatalf("unexpected lookup domain: %s", router.domain)
	}
	if router.options.Strategy != C.DomainStrategyIPv4Only {
		t.Fatalf("unexpected lookup strategy: %d", router.options.Strategy)
	}
	if len(ips) != 1 || !ips[0].Equal(net.ParseIP("192.0.2.1")) {
		t.Fatalf("unexpected lookup result: %v", ips)
	}
}

func TestMieruUDPDomainServerDetourDefersResolution(t *testing.T) {
	options := option.MieruOutboundOptions{
		DialerOptions: option.DialerOptions{
			Detour: "proxy",
		},
		ServerOptions: option.ServerOptions{
			Server:     "proxy.example",
			ServerPort: 8964,
		},
		Transport: "UDP",
		UserName:  "user",
		Password:  "password",
	}

	router := &recordingDNSRouter{
		addrs: []netip.Addr{netip.MustParseAddr("198.51.100.1")},
	}
	ctx := service.ContextWith[adapter.DNSRouter](context.Background(), router)
	resolver := newMieruResolver(ctx, options, noopDialer{})
	if _, loaded := resolver.(virtualMieruResolver); !loaded {
		t.Fatalf("unexpected resolver type: %T", resolver)
	}
	ips, err := resolver.LookupIP(ctx, "ip", "proxy.example")
	if err != nil {
		t.Fatal(err)
	}
	if router.domain != "" {
		t.Fatalf("detour resolver leaked lookup to DNS router: %s", router.domain)
	}
	if len(ips) != 1 || !ips[0].Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("unexpected virtual resolver result: %v", ips)
	}

	packetConn := &recordingPacketConn{
		read: []byte("pong"),
	}
	baseDialer := &recordingDialer{
		packetConn: packetConn,
	}
	mieruDialer := newMieruDialer(options, baseDialer)
	conn, err := mieruDialer.ListenPacket(context.Background(), "udp", "", "127.0.0.1:8964")
	if err != nil {
		t.Fatal(err)
	}
	if baseDialer.destination.String() != "proxy.example:8964" {
		t.Fatalf("unexpected packet dial destination: %s", baseDialer.destination)
	}

	_, err = conn.WriteTo([]byte("ping"), M.ParseSocksaddr("127.0.0.1:8964"))
	if err != nil {
		t.Fatal(err)
	}
	if packetConn.writeAddr.String() != "proxy.example:8964" {
		t.Fatalf("unexpected write destination: %s", packetConn.writeAddr)
	}

	readBuffer := make([]byte, 4)
	n, addr, err := conn.ReadFrom(readBuffer)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 || string(readBuffer[:n]) != "pong" {
		t.Fatalf("unexpected read payload: %q", readBuffer[:n])
	}
	if addr.String() != "127.0.0.1:8964" {
		t.Fatalf("unexpected virtual read source: %s", addr)
	}
}

func TestMieruUDPAssociateDestinationAllowsEmptyDestination(t *testing.T) {
	destination, err := mieruUDPAssociateDestination(M.Socksaddr{})
	if err != nil {
		t.Fatal(err)
	}
	if destination.Network() != N.NetworkUDP {
		t.Fatalf("unexpected network: %s", destination.Network())
	}
	if destination.Port != 0 || !destination.IP.Equal(net.IPv4zero) {
		t.Fatalf("unexpected UDP associate destination: %+v", destination)
	}
}

func TestOutboundListenPacketAllowsEmptyDestination(t *testing.T) {
	client := &recordingMieruClient{}
	outbound := &Outbound{
		Adapter: outboundAdapter.NewAdapter(C.TypeMieru, "test", []string{N.NetworkUDP}, nil),
		logger:  log.NewNOPFactory().Logger(),
		client:  client,
	}
	packetConn, err := outbound.ListenPacket(context.Background(), M.Socksaddr{})
	if err != nil {
		t.Fatal(err)
	}
	defer packetConn.Close()

	destination, loaded := client.addr.(mierumodel.NetAddrSpec)
	if !loaded {
		t.Fatalf("unexpected destination type: %T", client.addr)
	}
	if destination.Network() != N.NetworkUDP {
		t.Fatalf("unexpected network: %s", destination.Network())
	}
	if destination.Port != 0 || !destination.IP.Equal(net.IPv4zero) {
		t.Fatalf("unexpected UDP associate destination: %+v", destination)
	}
}

type recordingMieruResolver struct{}

func (r *recordingMieruResolver) LookupIP(context.Context, string, string) ([]net.IP, error) {
	return nil, nil
}

type recordingDNSRouter struct {
	domain  string
	options adapter.DNSQueryOptions
	addrs   []netip.Addr
}

func (r *recordingDNSRouter) Start(adapter.StartStage) error {
	return nil
}

func (r *recordingDNSRouter) Close() error {
	return nil
}

func (r *recordingDNSRouter) Exchange(context.Context, *dns.Msg, adapter.DNSQueryOptions) (*dns.Msg, error) {
	return nil, nil
}

func (r *recordingDNSRouter) Lookup(_ context.Context, domain string, options adapter.DNSQueryOptions) ([]netip.Addr, error) {
	r.domain = domain
	r.options = options
	return r.addrs, nil
}

func (r *recordingDNSRouter) ClearCache() {
}

func (r *recordingDNSRouter) LookupReverseMapping(netip.Addr) (string, bool) {
	return "", false
}

func (r *recordingDNSRouter) ResetNetwork() {
}

type noopDialer struct{}

func (noopDialer) DialContext(context.Context, string, M.Socksaddr) (net.Conn, error) {
	return noopConn{}, nil
}

func (noopDialer) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return &memoryPacketConn{}, nil
}

type recordingDialer struct {
	destination M.Socksaddr
	packetConn  net.PacketConn
}

func (d *recordingDialer) DialContext(context.Context, string, M.Socksaddr) (net.Conn, error) {
	return noopConn{}, nil
}

func (d *recordingDialer) ListenPacket(_ context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	d.destination = destination
	return d.packetConn, nil
}

type recordingPacketConn struct {
	read      []byte
	writeAddr net.Addr
}

func (c *recordingPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	copy(p, c.read)
	return len(c.read), M.ParseSocksaddr("proxy.example:8964"), nil
}

func (c *recordingPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	c.writeAddr = addr
	return len(p), nil
}

func (c *recordingPacketConn) Close() error {
	return nil
}

func (c *recordingPacketConn) LocalAddr() net.Addr {
	return M.ParseSocksaddr("127.0.0.1:1")
}

func (c *recordingPacketConn) SetDeadline(time.Time) error {
	return nil
}

func (c *recordingPacketConn) SetReadDeadline(time.Time) error {
	return nil
}

func (c *recordingPacketConn) SetWriteDeadline(time.Time) error {
	return nil
}

type recordingMieruClient struct {
	addr net.Addr
}

func (c *recordingMieruClient) Load() (*mieruclient.ClientConfig, error) {
	return nil, mieruclient.ErrNoClientConfig
}

func (c *recordingMieruClient) Store(*mieruclient.ClientConfig) error {
	return nil
}

func (c *recordingMieruClient) Start() error {
	return nil
}

func (c *recordingMieruClient) Stop() error {
	return nil
}

func (c *recordingMieruClient) IsRunning() bool {
	return true
}

func (c *recordingMieruClient) DialContext(_ context.Context, addr net.Addr) (net.Conn, error) {
	c.addr = addr
	return noopConn{}, nil
}

type noopConn struct{}

func (noopConn) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (noopConn) Write(p []byte) (int, error) {
	return len(p), nil
}

func (noopConn) Close() error {
	return nil
}

func (noopConn) LocalAddr() net.Addr {
	return M.ParseSocksaddr("127.0.0.1:1")
}

func (noopConn) RemoteAddr() net.Addr {
	return M.ParseSocksaddr("127.0.0.1:2")
}

func (noopConn) SetDeadline(time.Time) error {
	return nil
}

func (noopConn) SetReadDeadline(time.Time) error {
	return nil
}

func (noopConn) SetWriteDeadline(time.Time) error {
	return nil
}
