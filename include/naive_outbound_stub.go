//go:build !with_naive_outbound

package include

import (
	"context"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/singlink/singlink/adapter"
	"github.com/singlink/singlink/adapter/outbound"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"
)

func registerNaiveOutbound(registry *outbound.Registry) {
	outbound.Register[option.NaiveOutboundOptions](registry, C.TypeNaive, func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.NaiveOutboundOptions) (adapter.Outbound, error) {
		return nil, E.New(`naive outbound is not included in this build, rebuild with -tags with_naive_outbound`)
	})
}
