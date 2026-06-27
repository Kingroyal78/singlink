package v2rayhttp

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestLateHTTPConnCloseUnblocksRead(t *testing.T) {
	_, writer := io.Pipe()
	conn := NewLateHTTPConn(writer)
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

func TestLateHTTPConnCloseCancelsRoundTrip(t *testing.T) {
	roundTripDone := make(chan error, 1)
	client := &Client{
		transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			roundTripDone <- request.Context().Err()
			return nil, request.Context().Err()
		}),
		http2: true,
		requestURL: url.URL{
			Scheme: "https",
			Host:   "example.com",
			Path:   "/",
		},
		method: http.MethodPut,
	}
	conn, err := client.DialContext(t.Context())
	if err != nil {
		t.Fatalf("dial http2: %v", err)
	}
	if err = conn.Close(); err != nil {
		t.Fatalf("close conn: %v", err)
	}
	select {
	case err = <-roundTripDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("round trip got %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("RoundTrip was not canceled by Close")
	}
}
