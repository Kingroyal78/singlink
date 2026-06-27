//go:build with_grpc

package v2ray

import (
	"context"

	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/singlink/singlink/adapter"
	"github.com/singlink/singlink/common/tls"
	"github.com/singlink/singlink/option"
	"github.com/singlink/singlink/transport/v2raygrpc"
	"github.com/singlink/singlink/transport/v2raygrpclite"
)

func NewGRPCServer(ctx context.Context, logger logger.ContextLogger, options option.V2RayGRPCOptions, tlsConfig tls.ServerConfig, handler adapter.V2RayServerTransportHandler) (adapter.V2RayServerTransport, error) {
	if options.ForceLite {
		return v2raygrpclite.NewServer(ctx, logger, options, tlsConfig, handler)
	}
	return v2raygrpc.NewServer(ctx, logger, options, tlsConfig, handler)
}

func NewGRPCClient(ctx context.Context, dialer N.Dialer, serverAddr M.Socksaddr, options option.V2RayGRPCOptions, tlsConfig tls.Config) (adapter.V2RayClientTransport, error) {
	if options.ForceLite {
		return v2raygrpclite.NewClient(ctx, dialer, serverAddr, options, tlsConfig), nil
	}
	return v2raygrpc.NewClient(ctx, dialer, serverAddr, options, tlsConfig)
}
