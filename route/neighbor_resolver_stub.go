//go:build !linux && !darwin

package route

import (
	"os"

	"github.com/sagernet/sing/common/logger"
	"github.com/singlink/singlink/adapter"
)

func newNeighborResolver(_ logger.ContextLogger, _ []string) (adapter.NeighborResolver, error) {
	return nil, os.ErrInvalid
}
