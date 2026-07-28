//go:build !linux

package v2board

import "fmt"

func detectTCPBrutalCapability() error {
	return fmt.Errorf("TCP Brutal is only supported on Linux")
}
