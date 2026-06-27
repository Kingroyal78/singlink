//go:build !with_clash_api

package include

import (
	"context"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/singlink/singlink/adapter"
	"github.com/singlink/singlink/experimental"
	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"
)

func init() {
	experimental.RegisterClashServerConstructor(func(ctx context.Context, logFactory log.ObservableFactory, options option.ClashAPIOptions) (adapter.ClashServer, error) {
		return nil, E.New(`clash api is not included in this build, rebuild with -tags with_clash_api`)
	})
}
