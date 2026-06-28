package controlauth

import (
	"net/netip"
	"testing"

	"github.com/sagernet/sing/common/json/badoption"
	"github.com/singlink/singlink/option"
)

func TestIsLoopbackListenOptions(t *testing.T) {
	if !IsLoopbackListenOptions(option.ListenOptions{}) {
		t.Fatal("default listen options should be loopback")
	}
	addr := badoption.Addr(netip.IPv4Unspecified())
	if IsLoopbackListenOptions(option.ListenOptions{Listen: &addr}) {
		t.Fatal("unspecified listen address should not be loopback-only")
	}
}

func TestIsLoopbackListenAddress(t *testing.T) {
	tests := []struct {
		address string
		want    bool
	}{
		{"127.0.0.1:9090", true},
		{"[::1]:9090", true},
		{"localhost:9090", true},
		{":9090", false},
		{"0.0.0.0:9090", false},
		{"[::]:9090", false},
	}
	for _, test := range tests {
		if got := IsLoopbackListenAddress(test.address); got != test.want {
			t.Fatalf("IsLoopbackListenAddress(%q) = %v, want %v", test.address, got, test.want)
		}
	}
}
