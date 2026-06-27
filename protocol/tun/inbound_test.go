package tun

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing/common/x/list"
	"github.com/singlink/singlink/adapter"

	"go4.org/netipx"
)

func TestCloseRouteRuleSetsReleasesRefsAndCallbacksOnce(t *testing.T) {
	routeRuleSet := newRecordingRuleSet("route")
	excludeRuleSet := newRecordingRuleSet("exclude")
	routeCallback := routeRuleSet.RegisterCallback(func(adapter.RuleSet) {})
	excludeCallback := excludeRuleSet.RegisterCallback(func(adapter.RuleSet) {})

	inbound := &Inbound{
		routeRuleSet:                []adapter.RuleSet{routeRuleSet},
		routeRuleSetCallback:        []*list.Element[adapter.RuleSetUpdateCallback]{routeCallback},
		routeExcludeRuleSet:         []adapter.RuleSet{excludeRuleSet},
		routeExcludeRuleSetCallback: []*list.Element[adapter.RuleSetUpdateCallback]{excludeCallback},
		routeRuleSetRefsAdded:       true,
	}

	inbound.closeRouteRuleSets()
	inbound.closeRouteRuleSets()

	if routeRuleSet.decRefs != 1 {
		t.Fatalf("expected one route DecRef, got %d", routeRuleSet.decRefs)
	}
	if excludeRuleSet.decRefs != 1 {
		t.Fatalf("expected one exclude DecRef, got %d", excludeRuleSet.decRefs)
	}
	if routeRuleSet.unregisters != 1 {
		t.Fatalf("expected one route callback unregister, got %d", routeRuleSet.unregisters)
	}
	if excludeRuleSet.unregisters != 1 {
		t.Fatalf("expected one exclude callback unregister, got %d", excludeRuleSet.unregisters)
	}
	if inbound.routeRuleSetRefsAdded {
		t.Fatal("expected refs flag to be reset")
	}
}

func TestUpdateRouteAddressSetAfterCloseReturns(t *testing.T) {
	autoRedirect := newBlockingAutoRedirect()
	inbound := &Inbound{
		autoRedirect: autoRedirect,
	}

	if err := inbound.Close(); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		inbound.updateRouteAddressSet(nil)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("updateRouteAddressSet blocked after Close")
	}
	if updates := autoRedirect.updateCalls.Load(); updates != 0 {
		t.Fatalf("expected no update after Close, got %d", updates)
	}
}

func TestCloseWaitsForInflightRouteAddressSetUpdate(t *testing.T) {
	routeRuleSet := newRecordingRuleSet("route")
	autoRedirect := newBlockingAutoRedirect()
	inbound := &Inbound{
		autoRedirect: autoRedirect,
		routeRuleSet: []adapter.RuleSet{routeRuleSet},
	}

	updateDone := make(chan struct{})
	go func() {
		defer close(updateDone)
		inbound.updateRouteAddressSet(routeRuleSet)
	}()
	<-autoRedirect.entered

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- inbound.Close()
	}()

	select {
	case err := <-closeDone:
		t.Fatalf("Close returned while route address update was still running: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(autoRedirect.release)

	select {
	case <-updateDone:
	case <-time.After(time.Second):
		t.Fatal("route address update did not finish")
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not return after route address update finished")
	}
}

type recordingRuleSet struct {
	name        string
	decRefs     int
	unregisters int
	callbacks   list.List[adapter.RuleSetUpdateCallback]
}

func newRecordingRuleSet(name string) *recordingRuleSet {
	return &recordingRuleSet{name: name}
}

func (s *recordingRuleSet) Name() string {
	return s.name
}

func (s *recordingRuleSet) StartContext(context.Context, *adapter.HTTPStartContext) error {
	return nil
}

func (s *recordingRuleSet) Metadata() adapter.RuleSetMetadata {
	return adapter.RuleSetMetadata{}
}

func (s *recordingRuleSet) ExtractIPSet() []*netipx.IPSet {
	return nil
}

func (s *recordingRuleSet) IncRef() {}

func (s *recordingRuleSet) DecRef() {
	s.decRefs++
}

func (s *recordingRuleSet) Cleanup() {}

func (s *recordingRuleSet) RegisterCallback(callback adapter.RuleSetUpdateCallback) *list.Element[adapter.RuleSetUpdateCallback] {
	return s.callbacks.PushBack(callback)
}

func (s *recordingRuleSet) UnregisterCallback(element *list.Element[adapter.RuleSetUpdateCallback]) {
	s.unregisters++
	s.callbacks.Remove(element)
}

func (s *recordingRuleSet) Close() error {
	return nil
}

func (s *recordingRuleSet) Match(*adapter.InboundContext) bool {
	return false
}

func (s *recordingRuleSet) String() string {
	return s.name
}

type blockingAutoRedirect struct {
	entered     chan struct{}
	release     chan struct{}
	updateOnce  sync.Once
	updateCalls atomic.Int32
	closeCalls  atomic.Int32
}

func newBlockingAutoRedirect() *blockingAutoRedirect {
	return &blockingAutoRedirect{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (a *blockingAutoRedirect) Start() error {
	return nil
}

func (a *blockingAutoRedirect) Close() error {
	a.closeCalls.Add(1)
	return nil
}

func (a *blockingAutoRedirect) UpdateRouteAddressSet() {
	a.updateCalls.Add(1)
	a.updateOnce.Do(func() {
		close(a.entered)
	})
	<-a.release
}
