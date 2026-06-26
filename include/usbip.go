//go:build with_usbip && (linux || (darwin && cgo) || windows)

package include

import (
	"github.com/singlink/singlink/adapter/service"
	"github.com/singlink/singlink/service/usbip"
)

func registerUSBIPServices(registry *service.Registry) {
	usbip.RegisterService(registry)
}
