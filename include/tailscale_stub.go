//go:build !with_tailscale

package include

import (
	"context"

	"github.com/singlink/singlink/adapter"
	"github.com/singlink/singlink/adapter/certificate"
	"github.com/singlink/singlink/adapter/endpoint"
	"github.com/singlink/singlink/adapter/service"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/dns"
	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"
	E "github.com/sagernet/sing/common/exceptions"
)

func registerTailscaleEndpoint(registry *endpoint.Registry) {
	endpoint.Register[option.TailscaleEndpointOptions](registry, C.TypeTailscale, func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.TailscaleEndpointOptions) (adapter.Endpoint, error) {
		return nil, E.New(`Tailscale is not included in this build, rebuild with -tags with_tailscale`)
	})
}

func registerTailscaleTransport(registry *dns.TransportRegistry) {
	dns.RegisterTransport[option.TailscaleDNSServerOptions](registry, C.DNSTypeTailscale, func(ctx context.Context, logger log.ContextLogger, tag string, options option.TailscaleDNSServerOptions) (adapter.DNSTransport, error) {
		return nil, E.New(`Tailscale is not included in this build, rebuild with -tags with_tailscale`)
	})
}

func registerTailscaleCertificateProvider(registry *certificate.Registry) {
	certificate.Register[option.TailscaleCertificateProviderOptions](registry, C.TypeTailscale, func(ctx context.Context, logger log.ContextLogger, tag string, options option.TailscaleCertificateProviderOptions) (adapter.CertificateProviderService, error) {
		return nil, E.New(`Tailscale is not included in this build, rebuild with -tags with_tailscale`)
	})
}

func registerDERPService(registry *service.Registry) {
	service.Register[option.DERPServiceOptions](registry, C.TypeDERP, func(ctx context.Context, logger log.ContextLogger, tag string, options option.DERPServiceOptions) (adapter.Service, error) {
		return nil, E.New(`DERP is not included in this build, rebuild with -tags with_tailscale`)
	})
}
