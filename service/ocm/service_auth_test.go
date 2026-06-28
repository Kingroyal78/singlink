package ocm

import (
	"net/netip"
	"testing"

	"github.com/sagernet/sing/common/json/badoption"
	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"
)

func TestValidateUsersRejectsEmptyAndDuplicateTokens(t *testing.T) {
	if err := validateUsers([]option.OCMUser{{Name: "empty"}}); err == nil {
		t.Fatal("expected empty token to fail")
	}
	if err := validateUsers([]option.OCMUser{{Name: "a", Token: "token"}, {Name: "b", Token: "token"}}); err == nil {
		t.Fatal("expected duplicate token to fail")
	}
}

func TestNewServiceRejectsUnauthenticatedNonLoopback(t *testing.T) {
	addr := badoption.Addr(netip.IPv4Unspecified())
	_, err := NewService(t.Context(), log.NewNOPFactory().Logger(), "test", option.OCMServiceOptions{
		ListenOptions: option.ListenOptions{Listen: &addr},
	})
	if err == nil {
		t.Fatal("expected non-loopback ocm without users to fail")
	}
}
