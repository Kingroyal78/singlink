//go:build !with_v2ray_api

package include

import (
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/singlink/singlink/adapter"
	"github.com/singlink/singlink/experimental"
	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"
)

func init() {
	experimental.RegisterV2RayServerConstructor(func(logger log.Logger, options option.V2RayAPIOptions) (adapter.V2RayServer, error) {
		return nil, E.New(`v2ray api is not included in this build, rebuild with -tags with_v2ray_api`)
	})
}
