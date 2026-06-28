package v2rayapi

import (
	"testing"

	"github.com/singlink/singlink/log"
	"github.com/singlink/singlink/option"
)

func TestNewServerRejectsNonLoopbackListen(t *testing.T) {
	_, err := NewServer(log.NewNOPFactory().Logger(), option.V2RayAPIOptions{
		Listen: "0.0.0.0:9091",
	})
	if err == nil {
		t.Fatal("expected non-loopback v2ray api listen to fail")
	}
}
