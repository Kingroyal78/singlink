//go:build with_dhcp

package local

import (
	"context"

	N "github.com/sagernet/sing/common/network"
	"github.com/singlink/singlink/dns"
	"github.com/singlink/singlink/dns/transport/dhcp"
	"github.com/singlink/singlink/log"
)

func newDHCPTransport(transportAdapter dns.TransportAdapter, ctx context.Context, dialer N.Dialer, logger log.ContextLogger) dhcpTransport {
	return dhcp.NewRawTransport(transportAdapter, ctx, dialer, logger)
}
