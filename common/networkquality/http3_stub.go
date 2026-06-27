//go:build !with_quic

package networkquality

import (
	N "github.com/sagernet/sing/common/network"
	C "github.com/singlink/singlink/constant"
)

func NewHTTP3MeasurementClientFactory(dialer N.Dialer) (MeasurementClientFactory, error) {
	return nil, C.ErrQUICNotIncluded
}
