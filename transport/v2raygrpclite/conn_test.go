package v2raygrpclite

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
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

func TestGunConnReadRejectsOversizedMessageLength(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	var frame bytes.Buffer
	frame.Write(make([]byte, 6))
	var encodedLen [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(encodedLen[:], uint64(maxInt)+1)
	frame.Write(encodedLen[:n])
	conn := newGunConn(bytes.NewReader(frame.Bytes()), io.Discard, nil)

	_, err := conn.Read(make([]byte, 1))
	if err == nil || !strings.Contains(err.Error(), "message length overflow") {
		t.Fatalf("Read error = %v, want message length overflow", err)
	}
}
