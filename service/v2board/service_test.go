package v2board

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sagernet/sing/common/json/badoption"
	"github.com/sagernet/sing/service"
	"github.com/singlink/singlink/adapter"
	"github.com/singlink/singlink/option"
	"github.com/singlink/singlink/route"
)

func TestNewServiceRejectsDuplicateNodeTags(t *testing.T) {
	_, err := NewService(newServiceTestContext(&fakeInboundManager{}), nil, "panel", option.V2BoardServiceOptions{
		APIHost: "https://panel.example",
		APIKey:  "secret",
		Nodes: []option.V2BoardNodeOptions{
			{Tag: "duplicate", NodeID: 1, NodeType: "vless"},
			{Tag: "duplicate", NodeID: 2, NodeType: "vless"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate tag duplicate") {
		t.Fatalf("expected duplicate tag error, got %v", err)
	}
}

func TestNewServiceRejectsExistingInboundTag(t *testing.T) {
	manager := &fakeInboundManager{
		existingTags: map[string]adapter.Inbound{
			"existing": fakeInbound{tag: "existing"},
		},
	}

	_, err := NewService(newServiceTestContext(manager), nil, "panel", option.V2BoardServiceOptions{
		APIHost: "https://panel.example",
		APIKey:  "secret",
		Nodes: []option.V2BoardNodeOptions{
			{Tag: "existing", NodeID: 1, NodeType: "vless"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "tag conflicts with existing inbound: existing") {
		t.Fatalf("expected existing inbound tag error, got %v", err)
	}
}

func TestResolveNodeOptionsRejectsNegativeDurations(t *testing.T) {
	_, err := resolveNodeOptions("panel", 0, option.V2BoardServiceOptions{
		APIHost:      "https://panel.example",
		APIKey:       "secret",
		PullInterval: badoption.Duration(-time.Second),
	}, option.V2BoardNodeOptions{
		NodeID:   1,
		NodeType: "vless",
	})
	if err == nil || !strings.Contains(err.Error(), "pull_interval must not be negative") {
		t.Fatalf("expected negative pull interval error, got %v", err)
	}

	_, err = resolveNodeOptions("panel", 0, option.V2BoardServiceOptions{
		APIHost: "https://panel.example",
		APIKey:  "secret",
		Timeout: badoption.Duration(-time.Second),
	}, option.V2BoardNodeOptions{
		NodeID:   1,
		NodeType: "vless",
	})
	if err == nil || !strings.Contains(err.Error(), "timeout must not be negative") {
		t.Fatalf("expected negative timeout error, got %v", err)
	}

	_, err = resolveNodeOptions("panel", 0, option.V2BoardServiceOptions{
		APIHost:        "https://panel.example",
		APIKey:         "secret",
		ErrorBodyLimit: -1,
	}, option.V2BoardNodeOptions{
		NodeID:   1,
		NodeType: "vless",
	})
	if err == nil || !strings.Contains(err.Error(), "error_body_limit must not be negative") {
		t.Fatalf("expected negative error body limit error, got %v", err)
	}

	_, err = resolveNodeOptions("panel", 0, option.V2BoardServiceOptions{
		APIHost:           "https://panel.example",
		APIKey:            "secret",
		UserListBodyLimit: -1,
	}, option.V2BoardNodeOptions{
		NodeID:   1,
		NodeType: "vless",
	})
	if err == nil || !strings.Contains(err.Error(), "user_list_body_limit must not be negative") {
		t.Fatalf("expected negative user list body limit error, got %v", err)
	}
}

func TestResolveNodeOptionsClampsSmallIntervals(t *testing.T) {
	effective, err := resolveNodeOptions("panel", 0, option.V2BoardServiceOptions{
		APIHost:      "https://panel.example",
		APIKey:       "secret",
		PullInterval: badoption.Duration(time.Millisecond),
		PushInterval: badoption.Duration(2 * time.Millisecond),
	}, option.V2BoardNodeOptions{
		NodeID:   1,
		NodeType: "vless",
	})
	if err != nil {
		t.Fatal(err)
	}
	if effective.PullInterval != minimumInterval || effective.PushInterval != minimumInterval {
		t.Fatalf("small intervals were not clamped: %#v", effective)
	}
}

func TestResolveNodeOptionsInheritsAndOverridesBodyLimits(t *testing.T) {
	effective, err := resolveNodeOptions("panel", 0, option.V2BoardServiceOptions{
		APIHost:           "https://panel.example",
		APIKey:            "secret",
		ErrorBodyLimit:    1024,
		UserListBodyLimit: 2048,
	}, option.V2BoardNodeOptions{
		NodeID:   1,
		NodeType: "vless",
	})
	if err != nil {
		t.Fatal(err)
	}
	if effective.ErrorBodyLimit != 1024 || effective.UserListBodyLimit != 2048 {
		t.Fatalf("body limits were not inherited: %#v", effective)
	}

	effective, err = resolveNodeOptions("panel", 0, option.V2BoardServiceOptions{
		APIHost:           "https://panel.example",
		APIKey:            "secret",
		ErrorBodyLimit:    1024,
		UserListBodyLimit: 2048,
	}, option.V2BoardNodeOptions{
		NodeID:            1,
		NodeType:          "vless",
		ErrorBodyLimit:    4096,
		UserListBodyLimit: 8192,
	})
	if err != nil {
		t.Fatal(err)
	}
	if effective.ErrorBodyLimit != 4096 || effective.UserListBodyLimit != 8192 {
		t.Fatalf("node body limits did not override service limits: %#v", effective)
	}
}

func TestServiceCloseWaitsForControllerRunBeforeFinalNotReady(t *testing.T) {
	statusCalled := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		statusCalled <- struct{}{}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	controller := &Controller{
		ctx:             ctx,
		inbound:         &fakeInboundManager{},
		tracker:         &TrafficTracker{nodes: make(map[string]*nodeTraffic)},
		client:          newTestClientWithStyle(t, server.URL, APIStyleUniProxy, "shadowsocks"),
		options:         effectiveNodeOptions{Tag: "node"},
		node:            &NodeInfo{Type: "shadowsocks", Common: &ServerConfig{ConfigRevision: "sha256:abcdef"}},
		inboundReady:    true,
		statusReady:     true,
		appliedRevision: "sha256:abcdef",
		appliedFeatures: append([]string(nil), shadowsocksAppliedFeatures...),
	}
	service := &Service{
		ctx:         ctx,
		cancel:      cancel,
		controllers: []*Controller{controller},
	}
	runDone := make(chan struct{})
	service.wg.Add(1)
	go func() {
		defer service.wg.Done()
		<-runDone
	}()

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- service.Close()
	}()
	<-ctx.Done()

	select {
	case <-statusCalled:
		t.Fatal("final not-ready status was sent before the controller run exited")
	case <-time.After(50 * time.Millisecond):
	}

	close(runDone)
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-statusCalled:
	case <-time.After(time.Second):
		t.Fatal("final not-ready status was not sent after the controller run exited")
	}
}

func newServiceTestContext(inbound adapter.InboundManager) context.Context {
	ctx := context.Background()
	ctx = service.ContextWith[adapter.InboundManager](ctx, inbound)
	ctx = service.ContextWith[adapter.Router](ctx, &route.Router{})
	return ctx
}
