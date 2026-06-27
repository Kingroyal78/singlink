//go:build with_gvisor

package tailscale

import (
	"net"
	"reflect"
	"syscall"
	"testing"

	"github.com/sagernet/tailscale/net/netmon"
	"github.com/sagernet/tailscale/net/netns"
)

func TestGlobalHookManagerKeepsOtherInterfaceGetter(t *testing.T) {
	manager := newTailscaleGlobalHookManager()
	t.Cleanup(func() {
		netmon.RegisterInterfaceGetter(nil)
	})

	firstID := manager.registerInterfaceGetter(func() ([]netmon.Interface, error) {
		return []netmon.Interface{{Interface: &net.Interface{Name: "first"}}}, nil
	})
	secondID := manager.registerInterfaceGetter(func() ([]netmon.Interface, error) {
		return []netmon.Interface{{Interface: &net.Interface{Name: "second"}}}, nil
	})

	interfaces, err := manager.interfaceGetter()
	if err != nil {
		t.Fatal(err)
	}
	if got := interfaces[0].Name; got != "second" {
		t.Fatalf("active getter = %q, want second", got)
	}

	manager.unregisterInterfaceGetter(firstID)

	interfaces, err = manager.interfaceGetter()
	if err != nil {
		t.Fatal(err)
	}
	if got := interfaces[0].Name; got != "second" {
		t.Fatalf("active getter after unregistering first = %q, want second", got)
	}

	manager.unregisterInterfaceGetter(secondID)
}

func TestGlobalHookManagerKeepsOtherControlFunc(t *testing.T) {
	manager := newTailscaleGlobalHookManager()
	t.Cleanup(func() {
		netns.SetControlFunc(nil)
	})

	var calls []string
	firstID := manager.registerControlFunc(func(string, string, syscall.RawConn) error {
		calls = append(calls, "first")
		return nil
	})
	secondID := manager.registerControlFunc(func(string, string, syscall.RawConn) error {
		calls = append(calls, "second")
		return nil
	})

	if err := manager.controlFunc("tcp", "example.com:443", nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"second"}) {
		t.Fatalf("calls = %v, want [second]", calls)
	}

	manager.unregisterControlFunc(firstID)

	if err := manager.controlFunc("tcp", "example.com:443", nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"second", "second"}) {
		t.Fatalf("calls = %v, want [second second]", calls)
	}

	manager.unregisterControlFunc(secondID)
}
