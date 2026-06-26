//go:build with_naive_outbound

package include

import (
	"github.com/singlink/singlink/adapter/outbound"
	"github.com/singlink/singlink/protocol/naive"
)

func registerNaiveOutbound(registry *outbound.Registry) {
	naive.RegisterOutbound(registry)
}
