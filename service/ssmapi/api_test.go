package ssmapi

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/sagernet/sing/common/json/badoption"
	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"
)

func TestAPIServerRequiresBearerTokenWhenSecretConfigured(t *testing.T) {
	router := chi.NewRouter()
	NewAPIServer(log.NewNOPFactory().Logger(), nil, nil, "secret").Route(router)

	request := httptest.NewRequest(http.MethodGet, "/server/v1/", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}

	request = httptest.NewRequest(http.MethodGet, "/server/v1/", nil)
	request.Header.Set("Authorization", "Bearer secret")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestNewServiceRejectsNonLoopbackWithoutSecret(t *testing.T) {
	addr := badoption.Addr(netip.IPv4Unspecified())
	_, err := NewService(t.Context(), log.NewNOPFactory().Logger(), "test", option.SSMAPIServiceOptions{
		ListenOptions: option.ListenOptions{Listen: &addr},
	})
	if err == nil {
		t.Fatal("expected non-loopback ssmapi without secret to fail")
	}
}
