package include

import (
	"github.com/singlink/singlink/adapter/service"
	"github.com/singlink/singlink/service/oomkiller"
)

func registerOOMKillerService(registry *service.Registry) {
	oomkiller.RegisterService(registry)
}
