//go:build !with_acme

package include

import (
	"context"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/singlink/singlink/adapter"
	"github.com/singlink/singlink/adapter/certificate"
	C "github.com/singlink/singlink/constant"
	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"
)

func registerACMECertificateProvider(registry *certificate.Registry) {
	certificate.Register[option.ACMECertificateProviderOptions](registry, C.TypeACME, func(ctx context.Context, logger log.ContextLogger, tag string, options option.ACMECertificateProviderOptions) (adapter.CertificateProviderService, error) {
		return nil, E.New(`ACME is not included in this build, rebuild with -tags with_acme`)
	})
}
