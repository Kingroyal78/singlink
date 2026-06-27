package httpclient

import (
	"fmt"
	"testing"
)

func TestHTTP2FallbackAuthorityIsolation(t *testing.T) {
	transport := &http2FallbackTransport{fallbackAuthority: make(map[string]struct{})}

	transport.markH2Fallback("a.example:443")
	if !transport.isH2Fallback("a.example:443") {
		t.Fatal("a.example:443 should be marked")
	}
	if transport.isH2Fallback("b.example:443") {
		t.Fatal("b.example:443 must remain unmarked after marking a.example")
	}

	transport.markH2Fallback("b.example:443")
	if !transport.isH2Fallback("b.example:443") {
		t.Fatal("b.example:443 should be marked after explicit mark")
	}
	if !transport.isH2Fallback("a.example:443") {
		t.Fatal("a.example:443 mark must survive marking another authority")
	}
}

func TestHTTP2FallbackEmptyAuthorityNoOp(t *testing.T) {
	transport := &http2FallbackTransport{fallbackAuthority: make(map[string]struct{})}

	transport.markH2Fallback("")
	if len(transport.fallbackAuthority) != 0 {
		t.Fatalf("empty authority must not be stored, got %d entries", len(transport.fallbackAuthority))
	}
	if transport.isH2Fallback("") {
		t.Fatal("isH2Fallback must be false for empty authority")
	}
}

func TestHTTP2FallbackAuthorityCapacity(t *testing.T) {
	transport := &http2FallbackTransport{fallbackAuthority: make(map[string]struct{})}

	for index := range maxFallbackAuthorityEntries + 10 {
		transport.markH2Fallback(fmt.Sprintf("authority-%d.example:443", index))
	}
	if len(transport.fallbackAuthority) > maxFallbackAuthorityEntries {
		t.Fatalf("fallback authority map grew to %d entries, want at most %d", len(transport.fallbackAuthority), maxFallbackAuthorityEntries)
	}
	if transport.isH2Fallback("authority-0.example:443") {
		t.Fatal("oldest fallback authority should be evicted")
	}
	if !transport.isH2Fallback(fmt.Sprintf("authority-%d.example:443", maxFallbackAuthorityEntries+9)) {
		t.Fatal("newest fallback authority should be retained")
	}
}
