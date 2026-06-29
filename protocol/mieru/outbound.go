package mieru

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	mieruclient "github.com/enfein/mieru/v3/apis/client"
	mierucommon "github.com/enfein/mieru/v3/apis/common"
	mierumodel "github.com/enfein/mieru/v3/apis/model"
	mierutp "github.com/enfein/mieru/v3/apis/trafficpattern"
	mierupb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
	"github.com/singlink/singlink/adapter"
	"github.com/singlink/singlink/adapter/outbound"
	"github.com/singlink/singlink/common/dialer"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"
	"google.golang.org/protobuf/proto"
)

func RegisterOutbound(registry *outbound.Registry) {
	outbound.Register[option.MieruOutboundOptions](registry, C.TypeMieru, NewOutbound)
}

type Outbound struct {
	outbound.Adapter
	logger log.ContextLogger
	client mieruclient.Client
}

func NewOutbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.MieruOutboundOptions) (adapter.Outbound, error) {
	outboundDialer, err := dialer.NewWithOptions(dialer.Options{
		Context:        ctx,
		Options:        options.DialerOptions,
		RemoteIsDomain: options.ServerIsDomain(),
	})
	if err != nil {
		return nil, err
	}

	mieruDialer := newMieruDialer(options, outboundDialer)
	config, err := buildMieruClientConfig(options, mieruDialer, newMieruResolver(ctx, options, outboundDialer))
	if err != nil {
		return nil, E.Cause(err, "build mieru client config")
	}
	client := mieruclient.NewClient()
	if err = client.Store(config); err != nil {
		return nil, E.Cause(err, "store mieru client config")
	}

	return &Outbound{
		Adapter: outbound.NewAdapterWithDialerOptions(C.TypeMieru, tag, []string{N.NetworkTCP, N.NetworkUDP}, options.DialerOptions),
		logger:  logger,
		client:  client,
	}, nil
}

func (o *Outbound) Start(stage adapter.StartStage) error {
	if stage != adapter.StartStateStart {
		return nil
	}
	if err := o.client.Start(); err != nil {
		return E.Cause(err, "start mieru client")
	}
	o.logger.Info("mieru client started")
	return nil
}

func (o *Outbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	ctx, metadata := adapter.ExtendContext(ctx)
	metadata.Outbound = o.Tag()
	metadata.Destination = destination

	switch N.NetworkName(network) {
	case N.NetworkTCP:
		o.logger.InfoContext(ctx, "outbound mieru connection to ", destination)
		destinationAddr, err := socksAddrToNetAddrSpec(destination, N.NetworkTCP)
		if err != nil {
			return nil, E.Cause(err, "convert destination address")
		}
		return o.client.DialContext(ctx, destinationAddr)
	case N.NetworkUDP:
		o.logger.InfoContext(ctx, "outbound mieru packet stream to ", destination)
		destinationAddr, err := socksAddrToNetAddrSpec(destination, N.NetworkUDP)
		if err != nil {
			return nil, E.Cause(err, "convert destination address")
		}
		streamConn, err := o.client.DialContext(ctx, destinationAddr)
		if err != nil {
			return nil, err
		}
		return &streamer{
			PacketConn: mierucommon.NewUDPAssociateWrapper(mierucommon.NewPacketOverStreamTunnel(streamConn)),
			Remote:     destination,
		}, nil
	default:
		return nil, os.ErrInvalid
	}
}

func (o *Outbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	ctx, metadata := adapter.ExtendContext(ctx)
	metadata.Outbound = o.Tag()
	metadata.Destination = destination
	o.logger.InfoContext(ctx, "outbound mieru packet connection to ", destination)

	destinationAddr, err := mieruUDPAssociateDestination(destination)
	if err != nil {
		return nil, E.Cause(err, "convert destination address")
	}
	streamConn, err := o.client.DialContext(ctx, destinationAddr)
	if err != nil {
		return nil, err
	}
	return mierucommon.NewUDPAssociateWrapper(mierucommon.NewPacketOverStreamTunnel(streamConn)), nil
}

func (o *Outbound) Close() error {
	return o.client.Stop()
}

type mieruDialer struct {
	dialer                   N.Dialer
	udpServer                M.Socksaddr
	deferUDPServerResolution bool
}

func newMieruDialer(options option.MieruOutboundOptions, dialer N.Dialer) mieruDialer {
	mieruDialer := mieruDialer{dialer: dialer}
	if shouldDeferMieruUDPServerResolution(options, dialer) {
		mieruDialer.udpServer = options.ServerOptions.Build()
		mieruDialer.deferUDPServerResolution = true
	}
	return mieruDialer
}

func (d mieruDialer) DialContext(ctx context.Context, network string, address string) (net.Conn, error) {
	return d.dialer.DialContext(ctx, network, M.ParseSocksaddr(address))
}

func (d mieruDialer) ListenPacket(ctx context.Context, network string, laddr string, raddr string) (net.PacketConn, error) {
	if raddr == "" {
		return nil, E.New("missing mieru remote packet address")
	}
	destination := M.ParseSocksaddr(raddr)
	if !d.deferUDPServerResolution {
		return d.dialer.ListenPacket(ctx, destination)
	}
	serverDestination := d.udpServer
	serverDestination.Port = destination.Port
	conn, err := d.dialer.ListenPacket(ctx, serverDestination)
	if err != nil {
		return nil, err
	}
	return &mieruUDPServerPacketConn{
		PacketConn:         conn,
		serverDestination:  serverDestination,
		virtualDestination: destination,
	}, nil
}

var (
	_ mierucommon.Dialer       = mieruDialer{}
	_ mierucommon.PacketDialer = mieruDialer{}
)

type mieruUDPServerPacketConn struct {
	net.PacketConn
	serverDestination  M.Socksaddr
	virtualDestination M.Socksaddr
}

func (c *mieruUDPServerPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, _, err := c.PacketConn.ReadFrom(p)
	if err != nil {
		return 0, nil, err
	}
	return n, c.virtualDestination.UDPAddr(), nil
}

func (c *mieruUDPServerPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	return c.PacketConn.WriteTo(p, c.serverDestination)
}

type streamer struct {
	net.PacketConn
	Remote net.Addr
}

var _ net.Conn = (*streamer)(nil)

func (s *streamer) Read(b []byte) (int, error) {
	n, _, err := s.PacketConn.ReadFrom(b)
	return n, err
}

func (s *streamer) Write(b []byte) (int, error) {
	return s.WriteTo(b, s.Remote)
}

func (s *streamer) RemoteAddr() net.Addr {
	return s.Remote
}

func buildMieruClientConfig(options option.MieruOutboundOptions, dialer mieruDialer, resolver mierucommon.DNSResolver) (*mieruclient.ClientConfig, error) {
	if err := validateMieruOutboundOptions(options); err != nil {
		return nil, err
	}

	transportProtocol, err := mieruTransportProtocol(options.Transport)
	if err != nil {
		return nil, err
	}

	server := &mierupb.ServerEndpoint{}
	if options.ServerPort != 0 {
		server.PortBindings = append(server.PortBindings, &mierupb.PortBinding{
			Port:     proto.Int32(int32(options.ServerPort)),
			Protocol: transportProtocol,
		})
	}
	for _, portRange := range options.ServerPortRanges {
		server.PortBindings = append(server.PortBindings, &mierupb.PortBinding{
			PortRange: proto.String(portRange),
			Protocol:  transportProtocol,
		})
	}
	serverAddress := options.ServerOptions.Build()
	if serverAddress.IsFqdn() {
		server.DomainName = proto.String(serverAddress.Fqdn)
	} else {
		server.IpAddress = proto.String(serverAddress.Addr.String())
	}

	config := &mieruclient.ClientConfig{
		Profile: &mierupb.ClientProfile{
			ProfileName: proto.String("singlink"),
			User: &mierupb.User{
				Name:     proto.String(options.UserName),
				Password: proto.String(options.Password),
			},
			Servers: []*mierupb.ServerEndpoint{server},
		},
		Dialer:       dialer,
		PacketDialer: dialer,
		Resolver:     resolver,
		DNSConfig: &mierucommon.ClientDNSConfig{
			BypassDialerDNS: true,
		},
	}
	if options.Multiplexing != "" {
		multiplexing, loaded := mierupb.MultiplexingLevel_value[options.Multiplexing]
		if !loaded {
			return nil, E.New("unknown multiplexing: ", options.Multiplexing)
		}
		config.Profile.Multiplexing = &mierupb.MultiplexingConfig{
			Level: mierupb.MultiplexingLevel(multiplexing).Enum(),
		}
	}
	if options.TrafficPattern != "" {
		trafficPattern, err := mierutp.Decode(options.TrafficPattern)
		if err != nil {
			return nil, E.Cause(err, "decode traffic_pattern")
		}
		config.Profile.TrafficPattern = trafficPattern
	}
	return config, nil
}

func validateMieruOutboundOptions(options option.MieruOutboundOptions) error {
	if options.Server == "" {
		return E.New("missing server")
	}
	if options.ServerPort == 0 && len(options.ServerPortRanges) == 0 {
		return E.New("missing server_port or server_ports")
	}
	for _, portRange := range options.ServerPortRanges {
		if _, _, err := parseMieruPortRange(portRange); err != nil {
			return E.Cause(err, "server_ports[", portRange, "]")
		}
	}
	if _, err := mieruTransportProtocol(options.Transport); err != nil {
		return err
	}
	if err := validateMieruUser(options.UserName, options.Password); err != nil {
		return err
	}
	if options.Multiplexing != "" {
		if _, ok := mierupb.MultiplexingLevel_value[options.Multiplexing]; !ok {
			return E.New("unknown multiplexing: ", options.Multiplexing)
		}
	}
	if options.TrafficPattern != "" {
		trafficPattern, err := mierutp.Decode(options.TrafficPattern)
		if err != nil {
			return E.Cause(err, "decode traffic_pattern")
		}
		if err = mierutp.Validate(trafficPattern); err != nil {
			return E.Cause(err, "validate traffic_pattern")
		}
	}
	return nil
}

type mieruRouterResolver struct {
	router       adapter.DNSRouter
	queryOptions adapter.DNSQueryOptions
}

func shouldDeferMieruUDPServerResolution(options option.MieruOutboundOptions, outboundDialer N.Dialer) bool {
	if !strings.EqualFold(options.Transport, N.NetworkUDP) || !options.ServerIsDomain() || options.DialerOptions.Detour == "" {
		return false
	}
	_, isResolveDialer := outboundDialer.(dialer.ResolveDialer)
	return !isResolveDialer
}

func newMieruResolver(ctx context.Context, options option.MieruOutboundOptions, outboundDialer N.Dialer) mierucommon.DNSResolver {
	if shouldDeferMieruUDPServerResolution(options, outboundDialer) {
		return virtualMieruResolver{}
	}
	resolveDialer, isResolveDialer := outboundDialer.(dialer.ResolveDialer)
	if !isResolveDialer {
		return &net.Resolver{}
	}
	dnsRouter := service.FromContext[adapter.DNSRouter](ctx)
	if dnsRouter == nil {
		return &net.Resolver{}
	}
	return mieruRouterResolver{
		router:       dnsRouter,
		queryOptions: resolveDialer.QueryOptions(),
	}
}

type virtualMieruResolver struct{}

func (virtualMieruResolver) LookupIP(_ context.Context, network string, host string) ([]net.IP, error) {
	switch network {
	case "", "ip", "ip4":
		return []net.IP{net.IPv4(127, 0, 0, 1)}, nil
	case "ip6":
		return []net.IP{net.IPv6loopback}, nil
	default:
		return nil, net.UnknownNetworkError(network)
	}
}

var _ mierucommon.DNSResolver = virtualMieruResolver{}

func (r mieruRouterResolver) LookupIP(ctx context.Context, network string, host string) ([]net.IP, error) {
	queryOptions := r.queryOptions
	var ipv4Only bool
	var ipv6Only bool
	switch network {
	case "", "ip":
	case "ip4":
		ipv4Only = true
		queryOptions.Strategy = C.DomainStrategyIPv4Only
	case "ip6":
		ipv6Only = true
		queryOptions.Strategy = C.DomainStrategyIPv6Only
	default:
		return nil, net.UnknownNetworkError(network)
	}
	addresses, err := r.router.Lookup(ctx, strings.TrimSuffix(host, "."), queryOptions)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		if ipv4Only && !address.Is4() {
			continue
		}
		if ipv6Only && !address.Is6() {
			continue
		}
		ips = append(ips, append(net.IP(nil), address.AsSlice()...))
	}
	return ips, nil
}

var _ mierucommon.DNSResolver = mieruRouterResolver{}

func socksAddrToNetAddrSpec(socksaddr M.Socksaddr, network string) (mierumodel.NetAddrSpec, error) {
	var netAddr mierumodel.NetAddrSpec
	if !socksaddr.IsValid() {
		return netAddr, fmt.Errorf("empty destination")
	}
	if err := netAddr.From(socksaddr); err != nil {
		return netAddr, err
	}
	netAddr.Net = network
	return netAddr, nil
}

func mieruUDPAssociateDestination(destination M.Socksaddr) (mierumodel.NetAddrSpec, error) {
	if destination.IsValid() {
		return socksAddrToNetAddrSpec(destination, N.NetworkUDP)
	}
	return mierumodel.NetAddrSpec{
		AddrSpec: mierumodel.AddrSpec{
			IP:   net.IPv4zero,
			Port: 0,
		},
		Net: N.NetworkUDP,
	}, nil
}
