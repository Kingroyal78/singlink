//go:build with_quic

package v2rayquic

import "github.com/singlink/singlink/transport/v2ray"

func init() {
	v2ray.RegisterQUICConstructor(NewServer, NewClient)
}
