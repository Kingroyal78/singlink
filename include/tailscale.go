//go:build with_tailscale

package include

import (
	"github.com/singlink/singlink/adapter/certificate"
	"github.com/singlink/singlink/adapter/endpoint"
	"github.com/singlink/singlink/adapter/service"
	"github.com/singlink/singlink/dns"
	"github.com/singlink/singlink/protocol/tailscale"
	"github.com/singlink/singlink/service/derp"
)

func registerTailscaleEndpoint(registry *endpoint.Registry) {
	tailscale.RegisterEndpoint(registry)
}

func registerTailscaleTransport(registry *dns.TransportRegistry) {
	tailscale.RegistryTransport(registry)
}

func registerTailscaleCertificateProvider(registry *certificate.Registry) {
	tailscale.RegisterCertificateProvider(registry)
}

func registerDERPService(registry *service.Registry) {
	derp.Register(registry)
}
