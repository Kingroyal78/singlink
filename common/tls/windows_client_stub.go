//go:build !windows

package tls

import (
	"context"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	"github.com/singlink/singlink/option"
)

func newWindowsClient(ctx context.Context, logger logger.ContextLogger, serverAddress string, options option.OutboundTLSOptions, allowEmptyServerName bool) (Config, error) {
	return nil, E.New("Windows TLS engine is not available on non-Windows platforms")
}
