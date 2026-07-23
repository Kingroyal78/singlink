package v2rayhttp

import (
	std_bufio "bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	boxLog "github.com/singlink/singlink/log"
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

func TestHTTP2ConnWrapperWriteBufferAfterCloseReleasesBuffer(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	conn := NewHTTP2Wrapper(clientConn)
	conn.CloseWrapper()
	buffer := buf.NewSize(4)
	if _, err := buffer.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}

	err := conn.WriteBuffer(buffer)
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("WriteBuffer after Close error = %v, want net.ErrClosed", err)
	}
	if buffer.Cap() != 0 {
		t.Fatalf("buffer was not released: cap = %d", buffer.Cap())
	}
}

type serverTestHandler struct {
	called bool
}

func (h *serverTestHandler) NewConnectionEx(ctx context.Context, conn net.Conn, source M.Socksaddr, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	h.called = true
	conn.Close()
}

type hijackResponseWriter struct {
	header http.Header
	status int
}

func (w *hijackResponseWriter) Header() http.Header {
	return w.header
}

func (w *hijackResponseWriter) Write(data []byte) (int, error) {
	return len(data), nil
}

func (w *hijackResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
}

func (w *hijackResponseWriter) Flush() {
}

func (w *hijackResponseWriter) Hijack() (net.Conn, *std_bufio.ReadWriter, error) {
	clientConn, serverConn := net.Pipe()
	serverConn.Close()
	return clientConn, std_bufio.NewReadWriter(std_bufio.NewReader(bytes.NewReader(nil)), std_bufio.NewWriter(io.Discard)), nil
}

func TestServerRejectsOversizedHTTP1RequestBodyCache(t *testing.T) {
	handler := &serverTestHandler{}
	server := &Server{
		logger:  boxLog.NewNOPFactory().Logger(),
		handler: handler,
		path:    "/",
	}
	body := bytes.NewReader(make([]byte, buf.BufferSize+1))
	request := httptest.NewRequest(http.MethodPost, "http://example.com/", body)
	writer := &hijackResponseWriter{header: http.Header{}}

	server.ServeHTTP(writer, request)
	if writer.status == http.StatusOK {
		t.Fatal("oversized request body cache was accepted")
	}
	if handler.called {
		t.Fatal("handler was called for oversized request body cache")
	}
}
