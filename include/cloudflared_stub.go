//go:build !with_cloudflared

package include

import (
	"context"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/singlink/singlink/adapter"
	"github.com/singlink/singlink/adapter/inbound"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"
)

func registerCloudflaredInbound(registry *inbound.Registry) {
	inbound.Register[option.CloudflaredInboundOptions](registry, C.TypeCloudflared, func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.CloudflaredInboundOptions) (adapter.Inbound, error) {
		return nil, E.New(`Cloudflared is not included in this build, rebuild with -tags with_cloudflared`)
	})
}
