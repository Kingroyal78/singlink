package inbound

import (
	"context"
	"errors"
	"testing"

	"github.com/singlink/singlink/adapter"
	"github.com/singlink/singlink/log"
)

func TestCreateClosesNewInboundWhenExistingCloseFails(t *testing.T) {
	oldInbound := &testInbound{tag: "node"}
	newInbound := &testInbound{tag: "node"}
	registry := &testRegistry{inbounds: []adapter.Inbound{oldInbound, newInbound}}
	manager := NewManager(nil, registry, nil)

	if err := manager.Create(context.Background(), nil, nil, "node", "test", nil); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(adapter.StartStateStart); err != nil {
		t.Fatal(err)
	}
	closeErr := errors.New("close failed")
	oldInbound.closeErr = closeErr

	err := manager.Create(context.Background(), nil, nil, "node", "test", nil)
	if !errors.Is(err, closeErr) {
		t.Fatalf("expected existing close error, got %v", err)
	}
	if newInbound.closeCalls != 1 {
		t.Fatalf("new inbound was not closed after failed replacement, close calls: %d", newInbound.closeCalls)
	}
	inbound, loaded := manager.Get("node")
	if !loaded || inbound != oldInbound {
		t.Fatalf("existing inbound should remain registered after failed replacement")
	}
}

func TestCreateClosesNewInboundWhenStartFails(t *testing.T) {
	startErr := errors.New("start failed")
	oldInbound := &testInbound{tag: "node"}
	newInbound := &testInbound{tag: "node", startErr: startErr}
	registry := &testRegistry{inbounds: []adapter.Inbound{oldInbound, newInbound}}
	manager := NewManager(nil, registry, nil)

	if err := manager.Create(context.Background(), nil, nil, "node", "test", nil); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(adapter.StartStateStart); err != nil {
		t.Fatal(err)
	}

	err := manager.Create(context.Background(), nil, nil, "node", "test", nil)
	if !errors.Is(err, startErr) {
		t.Fatalf("expected start error, got %v", err)
	}
	if newInbound.closeCalls != 1 {
		t.Fatalf("new inbound was not closed after failed start, close calls: %d", newInbound.closeCalls)
	}
	if oldInbound.closeCalls != 0 {
		t.Fatalf("old inbound should stay running, close calls: %d", oldInbound.closeCalls)
	}
	inbound, loaded := manager.Get("node")
	if !loaded || inbound != oldInbound {
		t.Fatalf("existing inbound should remain registered after failed start")
	}
}

func TestCloseReturnsInboundCloseError(t *testing.T) {
	closeErr := errors.New("close failed")
	inbound := &testInbound{tag: "node", closeErr: closeErr}
	manager := NewManager(nil, &testRegistry{inbounds: []adapter.Inbound{inbound}}, nil)

	if err := manager.Create(context.Background(), nil, nil, "node", "test", nil); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(adapter.StartStateStart); err != nil {
		t.Fatal(err)
	}
	err := manager.Close()
	if !errors.Is(err, closeErr) {
		t.Fatalf("expected close error, got %v", err)
	}
}

type testRegistry struct {
	inbounds []adapter.Inbound
}

func (r *testRegistry) CreateOptions(string) (any, bool) {
	return nil, true
}

func (r *testRegistry) Create(context.Context, adapter.Router, log.ContextLogger, string, string, any) (adapter.Inbound, error) {
	if len(r.inbounds) == 0 {
		return nil, errors.New("missing test inbound")
	}
	inbound := r.inbounds[0]
	r.inbounds = r.inbounds[1:]
	return inbound, nil
}

type testInbound struct {
	tag        string
	startErr   error
	closeErr   error
	startCalls int
	closeCalls int
}

func (i *testInbound) Start(adapter.StartStage) error {
	i.startCalls++
	return i.startErr
}

func (i *testInbound) Close() error {
	i.closeCalls++
	return i.closeErr
}

func (i *testInbound) Type() string {
	return "test"
}

func (i *testInbound) Tag() string {
	return i.tag
}
