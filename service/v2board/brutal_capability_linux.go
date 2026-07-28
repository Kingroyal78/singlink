//go:build linux

package v2board

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func detectTCPBrutalCapability() error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, unix.IPPROTO_TCP)
	if err != nil {
		return fmt.Errorf("create TCP Brutal probe socket: %w", err)
	}
	defer unix.Close(fd)

	if err := unix.SetsockoptString(fd, unix.IPPROTO_TCP, unix.TCP_CONGESTION, "brutal"); err != nil {
		return fmt.Errorf("set TCP_CONGESTION=brutal on probe socket: %w", err)
	}
	return nil
}
