//go:build with_dhcp

package include

import (
	"github.com/singlink/singlink/dns"
	"github.com/singlink/singlink/dns/transport/dhcp"
)

func registerDHCPTransport(registry *dns.TransportRegistry) {
	dhcp.RegisterTransport(registry)
}
