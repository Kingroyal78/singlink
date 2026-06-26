//go:build with_dhcp

package local

import (
	"context"

	"github.com/singlink/singlink/dns"
	"github.com/singlink/singlink/dns/transport/dhcp"
	"github.com/singlink/singlink/log"
	N "github.com/sagernet/sing/common/network"
)

func newDHCPTransport(transportAdapter dns.TransportAdapter, ctx context.Context, dialer N.Dialer, logger log.ContextLogger) dhcpTransport {
	return dhcp.NewRawTransport(transportAdapter, ctx, dialer, logger)
}
