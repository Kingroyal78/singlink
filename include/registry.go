package include

import (
	"context"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/singlink/singlink"
	"github.com/singlink/singlink/adapter"
	"github.com/singlink/singlink/adapter/certificate"
	"github.com/singlink/singlink/adapter/endpoint"
	"github.com/singlink/singlink/adapter/inbound"
	"github.com/singlink/singlink/adapter/outbound"
	"github.com/singlink/singlink/adapter/service"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/dns"
	"github.com/singlink/singlink/dns/transport"
	"github.com/singlink/singlink/dns/transport/fakeip"
	"github.com/singlink/singlink/dns/transport/hosts"
	"github.com/singlink/singlink/dns/transport/local"
	"github.com/singlink/singlink/dns/transport/mdns"
	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"
	"github.com/singlink/singlink/protocol/anytls"
	"github.com/singlink/singlink/protocol/block"
	"github.com/singlink/singlink/protocol/direct"
	"github.com/singlink/singlink/protocol/group"
	"github.com/singlink/singlink/protocol/http"
	"github.com/singlink/singlink/protocol/mieru"
	"github.com/singlink/singlink/protocol/mixed"
	"github.com/singlink/singlink/protocol/naive"
	"github.com/singlink/singlink/protocol/redirect"
	"github.com/singlink/singlink/protocol/shadowsocks"
	"github.com/singlink/singlink/protocol/shadowtls"
	"github.com/singlink/singlink/protocol/socks"
	"github.com/singlink/singlink/protocol/ssh"
	"github.com/singlink/singlink/protocol/tor"
	"github.com/singlink/singlink/protocol/trojan"
	"github.com/singlink/singlink/protocol/tun"
	"github.com/singlink/singlink/protocol/vless"
	"github.com/singlink/singlink/protocol/vmess"
	"github.com/singlink/singlink/service/api"
	originca "github.com/singlink/singlink/service/origin_ca"
	"github.com/singlink/singlink/service/resolved"
	"github.com/singlink/singlink/service/ssmapi"
	"github.com/singlink/singlink/service/v2board"
)

func Context(ctx context.Context) context.Context {
	return box.Context(ctx, InboundRegistry(), OutboundRegistry(), EndpointRegistry(), DNSTransportRegistry(), ServiceRegistry(), CertificateProviderRegistry())
}

func InboundRegistry() *inbound.Registry {
	registry := inbound.NewRegistry()

	tun.RegisterInbound(registry)
	redirect.RegisterRedirect(registry)
	redirect.RegisterTProxy(registry)
	direct.RegisterInbound(registry)

	socks.RegisterInbound(registry)
	http.RegisterInbound(registry)
	mixed.RegisterInbound(registry)

	shadowsocks.RegisterInbound(registry)
	vmess.RegisterInbound(registry)
	trojan.RegisterInbound(registry)
	naive.RegisterInbound(registry)
	shadowtls.RegisterInbound(registry)
	vless.RegisterInbound(registry)
	anytls.RegisterInbound(registry)
	mieru.RegisterInbound(registry)

	registerQUICInbounds(registry)
	registerCloudflaredInbound(registry)
	registerStubForRemovedInbounds(registry)

	return registry
}

func OutboundRegistry() *outbound.Registry {
	registry := outbound.NewRegistry()

	direct.RegisterOutbound(registry)

	block.RegisterOutbound(registry)

	group.RegisterSelector(registry)
	group.RegisterURLTest(registry)

	socks.RegisterOutbound(registry)
	http.RegisterOutbound(registry)
	shadowsocks.RegisterOutbound(registry)
	vmess.RegisterOutbound(registry)
	trojan.RegisterOutbound(registry)
	registerNaiveOutbound(registry)
	tor.RegisterOutbound(registry)
	ssh.RegisterOutbound(registry)
	shadowtls.RegisterOutbound(registry)
	vless.RegisterOutbound(registry)
	anytls.RegisterOutbound(registry)
	mieru.RegisterOutbound(registry)

	registerQUICOutbounds(registry)
	registerStubForRemovedOutbounds(registry)

	return registry
}

func EndpointRegistry() *endpoint.Registry {
	registry := endpoint.NewRegistry()

	registerWireGuardEndpoint(registry)
	registerTailscaleEndpoint(registry)

	return registry
}

func DNSTransportRegistry() *dns.TransportRegistry {
	registry := dns.NewTransportRegistry()

	transport.RegisterTCP(registry)
	transport.RegisterUDP(registry)
	transport.RegisterTLS(registry)
	transport.RegisterHTTPS(registry)
	hosts.RegisterTransport(registry)
	local.RegisterTransport(registry)
	mdns.RegisterTransport(registry)
	fakeip.RegisterTransport(registry)
	resolved.RegisterTransport(registry)

	registerQUICTransports(registry)
	registerDHCPTransport(registry)
	registerTailscaleTransport(registry)

	return registry
}

func ServiceRegistry() *service.Registry {
	registry := service.NewRegistry()

	api.RegisterService(registry)
	resolved.RegisterService(registry)
	ssmapi.RegisterService(registry)
	v2board.RegisterService(registry)

	registerQUICServices(registry)
	registerDERPService(registry)
	registerCCMService(registry)
	registerOCMService(registry)
	registerOOMKillerService(registry)
	registerUSBIPServices(registry)

	return registry
}

func CertificateProviderRegistry() *certificate.Registry {
	registry := certificate.NewRegistry()

	registerACMECertificateProvider(registry)
	registerTailscaleCertificateProvider(registry)
	originca.RegisterCertificateProvider(registry)

	return registry
}

func registerStubForRemovedInbounds(registry *inbound.Registry) {
	inbound.Register[option.ShadowsocksInboundOptions](registry, C.TypeShadowsocksR, func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.ShadowsocksInboundOptions) (adapter.Inbound, error) {
		return nil, E.New("ShadowsocksR is deprecated and removed in sing-box 1.6.0")
	})
}

func registerStubForRemovedOutbounds(registry *outbound.Registry) {
	outbound.Register[option.ShadowsocksROutboundOptions](registry, C.TypeShadowsocksR, func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.ShadowsocksROutboundOptions) (adapter.Outbound, error) {
		return nil, E.New("ShadowsocksR is deprecated and removed in sing-box 1.6.0")
	})
	outbound.Register[option.StubOptions](registry, C.TypeWireGuard, func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.StubOptions) (adapter.Outbound, error) {
		return nil, E.New("WireGuard outbound is deprecated in sing-box 1.11.0 and removed in sing-box 1.13.0, use WireGuard endpoint instead")
	})
}
