package v2raygrpclite

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestLateGunConnCloseUnblocksRead(t *testing.T) {
	_, writer := io.Pipe()
	conn := newLateGunConn(writer)
	readErr := make(chan error, 1)
	go func() {
		_, err := conn.Read(make([]byte, 1))
		readErr <- err
	}()

	if err := conn.Close(); err != nil {
		t.Fatalf("close late conn: %v", err)
	}
	select {
	case err := <-readErr:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("read after close got %v, want net.ErrClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Read did not unblock after Close")
	}
}

func TestLateGunConnCloseCallsCancel(t *testing.T) {
	_, writer := io.Pipe()
	canceled := make(chan struct{})
	conn := newLateGunConn(writer, func() {
		close(canceled)
	})

	if err := conn.Close(); err != nil {
		t.Fatalf("close late conn: %v", err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("Close did not call cancel")
	}
}
