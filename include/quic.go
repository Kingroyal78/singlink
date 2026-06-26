//go:build with_quic

package include

import (
	"github.com/singlink/singlink/adapter/inbound"
	"github.com/singlink/singlink/adapter/outbound"
	"github.com/singlink/singlink/adapter/service"
	"github.com/singlink/singlink/dns"
	"github.com/singlink/singlink/dns/transport/quic"
	"github.com/singlink/singlink/protocol/hysteria"
	"github.com/singlink/singlink/protocol/hysteria2"
	_ "github.com/singlink/singlink/protocol/naive/quic"
	"github.com/singlink/singlink/protocol/tuic"
	_ "github.com/singlink/singlink/transport/v2rayquic"
)

func registerQUICInbounds(registry *inbound.Registry) {
	hysteria.RegisterInbound(registry)
	tuic.RegisterInbound(registry)
	hysteria2.RegisterInbound(registry)
}

func registerQUICOutbounds(registry *outbound.Registry) {
	hysteria.RegisterOutbound(registry)
	tuic.RegisterOutbound(registry)
	hysteria2.RegisterOutbound(registry)
}

func registerQUICTransports(registry *dns.TransportRegistry) {
	quic.RegisterTransport(registry)
	quic.RegisterHTTP3Transport(registry)
}

func registerQUICServices(registry *service.Registry) {
	hysteria2.RegisterRealmService(registry)
}
