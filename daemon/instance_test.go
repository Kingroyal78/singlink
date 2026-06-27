package daemon

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	box "github.com/singlink/singlink"
)

func TestInstanceCloseCancelsStartWithoutWaitingForStartToReturn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	lifecycle := newBlockingInstanceLifecycle(ctx)
	instance := &Instance{
		ctx:          ctx,
		cancel:       cancel,
		boxLifecycle: lifecycle,
	}

	startDone := make(chan error, 1)
	go func() {
		startDone <- instance.Start()
	}()
	<-lifecycle.started

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- instance.Close()
	}()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel a starting instance promptly")
	}

	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not return after canceling the starting instance")
	}

	select {
	case err := <-startDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled start, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start did not observe cancellation")
	}
}

func TestInstanceCloseOnlyClosesLifecycleOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	lifecycle := newBlockingInstanceLifecycle(ctx)
	instance := &Instance{
		ctx:          ctx,
		cancel:       cancel,
		boxLifecycle: lifecycle,
	}

	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	if closeCalls := lifecycle.closeCalls.Load(); closeCalls != 1 {
		t.Fatalf("expected one lifecycle close, got %d", closeCalls)
	}
}

func TestInstanceBoxReturnsNilAfterClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	boxInstance := &box.Box{}
	instance := &Instance{
		ctx:          ctx,
		cancel:       cancel,
		instance:     boxInstance,
		boxLifecycle: newBlockingInstanceLifecycle(ctx),
	}

	boxSnapshot := instance.Box()
	if boxSnapshot != boxInstance {
		t.Fatal("expected initial box snapshot")
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	if boxSnapshot != boxInstance {
		t.Fatal("expected existing box snapshot to remain stable")
	}
	if instance.Box() != nil {
		t.Fatal("expected Box to return nil after Close")
	}
}

type blockingInstanceLifecycle struct {
	ctx        context.Context
	started    chan struct{}
	startOnce  sync.Once
	closeCalls atomic.Int32
}

func newBlockingInstanceLifecycle(ctx context.Context) *blockingInstanceLifecycle {
	return &blockingInstanceLifecycle{
		ctx:     ctx,
		started: make(chan struct{}),
	}
}

func (l *blockingInstanceLifecycle) Start() error {
	l.startOnce.Do(func() {
		close(l.started)
	})
	<-l.ctx.Done()
	return l.ctx.Err()
}

func (l *blockingInstanceLifecycle) Close() error {
	l.closeCalls.Add(1)
	return nil
}
