package controlauth

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/netip"
	"strings"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/singlink/singlink/option"
)

func IsLoopbackListenOptions(options option.ListenOptions) bool {
	addr := options.Listen.Build(netip.AddrFrom4([4]byte{127, 0, 0, 1}))
	return addr.IsLoopback()
}

func IsLoopbackListenAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		if strings.Contains(address, ":") {
			return false
		}
		host = address
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if host == "" {
		return false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return addr.IsLoopback()
}

func RequireSecretForNonLoopback(serviceName string, secret string, options option.ListenOptions) error {
	if secret != "" || IsLoopbackListenOptions(options) {
		return nil
	}
	return E.New(serviceName, " secret is required when listen is not loopback")
}

func RequireSecretForNonLoopbackAddress(serviceName string, secret string, address string) error {
	if secret != "" || IsLoopbackListenAddress(address) {
		return nil
	}
	return E.New(serviceName, " secret is required when listen is not loopback")
}

func TokenFingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8])
}
