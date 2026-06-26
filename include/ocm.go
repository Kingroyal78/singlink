//go:build with_ocm

package include

import (
	"github.com/singlink/singlink/adapter/service"
	"github.com/singlink/singlink/service/ocm"
)

func registerOCMService(registry *service.Registry) {
	ocm.RegisterService(registry)
}
