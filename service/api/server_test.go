package api

import (
	"net/netip"
	"testing"

	"github.com/sagernet/sing/common/json/badoption"
	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"
)

func TestNewServiceRejectsNonLoopbackWithoutSecret(t *testing.T) {
	addr := badoption.Addr(netip.IPv4Unspecified())
	_, err := NewService(t.Context(), log.NewNOPFactory().Logger(), "test", option.APIServiceOptions{
		ListenOptions: option.ListenOptions{Listen: &addr},
	})
	if err == nil {
		t.Fatal("expected non-loopback api without secret to fail")
	}
}
