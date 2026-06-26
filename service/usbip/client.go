//go:build with_usbip && (linux || (darwin && cgo) || windows)

package usbip

import (
	"context"

	"github.com/singlink/singlink/adapter"
	boxService "github.com/singlink/singlink/adapter/service"
	"github.com/singlink/singlink/common/dialer"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"
	"github.com/sagernet/sing-usbip"
	E "github.com/sagernet/sing/common/exceptions"
)

type ClientService struct {
	boxService.Adapter
	ctx    context.Context
	logger log.ContextLogger
	inner  *usbip.ClientService
}

func NewClientService(ctx context.Context, logger log.ContextLogger, tag string, options option.USBIPClientServiceOptions) (adapter.Service, error) {
	serviceDialer, err := dialer.NewWithOptions(dialer.Options{
		Context: ctx,
		Options: option.DialerOptions{
			Detour: options.Detour,
		},
		RemoteIsDomain: true,
	})
	if err != nil {
		return nil, E.Cause(err, "create dialer")
	}
	inner, err := usbip.NewClientService(ctx, usbip.ClientOptions{
		Logger:        logger,
		Dialer:        serviceDialer,
		ServerAddress: options.ServerOptions.Build(),
		Devices:       toDeviceMatches(options.Devices),
	})
	if err != nil {
		return nil, err
	}
	return &ClientService{
		Adapter: boxService.NewAdapter(C.TypeUSBIPClient, tag),
		ctx:     ctx,
		logger:  logger,
		inner:   inner,
	}, nil
}

func (s *ClientService) Start(stage adapter.StartStage) error {
	if stage != adapter.StartStateStart {
		return nil
	}
	return s.inner.Start()
}

func (s *ClientService) Close() error {
	return s.inner.Close()
}
