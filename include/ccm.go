//go:build with_ccm && (!darwin || cgo)

package include

import (
	"github.com/singlink/singlink/adapter/service"
	"github.com/singlink/singlink/service/ccm"
)

func registerCCMService(registry *service.Registry) {
	ccm.RegisterService(registry)
}
