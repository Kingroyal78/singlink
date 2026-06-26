//go:build with_wireguard

package include

import (
	"github.com/singlink/singlink/adapter/endpoint"
	"github.com/singlink/singlink/protocol/wireguard"
)

func registerWireGuardEndpoint(registry *endpoint.Registry) {
	wireguard.RegisterEndpoint(registry)
}
