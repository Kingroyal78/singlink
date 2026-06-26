//go:build with_acme

package include

import (
	"github.com/singlink/singlink/adapter/certificate"
	"github.com/singlink/singlink/service/acme"
)

func registerACMECertificateProvider(registry *certificate.Registry) {
	acme.RegisterCertificateProvider(registry)
}
