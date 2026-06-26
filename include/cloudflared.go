//go:build with_cloudflared

package include

import (
	"github.com/singlink/singlink/adapter/inbound"
	"github.com/singlink/singlink/protocol/cloudflare"
)

func registerCloudflaredInbound(registry *inbound.Registry) {
	cloudflare.RegisterInbound(registry)
}
