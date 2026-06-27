package ccm

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type closeIdleTransport struct {
	closed atomic.Bool
}

func (t *closeIdleTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unused")
}

func (t *closeIdleTransport) CloseIdleConnections() {
	t.closed.Store(true)
}

func TestReadUsageTrackingRequestBodyRestoresLargeBody(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), usageTrackingBodyLimit+17)

	bodyBytes, restoredBody, err := readUsageTrackingRequestBody(io.NopCloser(bytes.NewReader(payload)))
	if !errors.Is(err, errUsageTrackingBodyTooLarge) {
		t.Fatalf("expected body too large error, got %v", err)
	}
	if bodyBytes != nil {
		t.Fatalf("expected no tracking bytes for oversized body")
	}

	restoredBytes, err := io.ReadAll(restoredBody)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restoredBytes, payload) {
		t.Fatalf("restored body mismatch: got %d bytes, want %d", len(restoredBytes), len(payload))
	}
}

func TestReadUsageTrackingResponseBodyCopiesLargeBody(t *testing.T) {
	payload := bytes.Repeat([]byte("b"), usageTrackingBodyLimit+17)
	recorder := httptest.NewRecorder()

	bodyBytes, copied, err := readUsageTrackingResponseBody(recorder, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if !copied {
		t.Fatalf("expected oversized response to be copied without tracking")
	}
	if bodyBytes != nil {
		t.Fatalf("expected no tracking bytes for oversized response")
	}
	if !bytes.Equal(recorder.Body.Bytes(), payload) {
		t.Fatalf("copied response mismatch: got %d bytes, want %d", recorder.Body.Len(), len(payload))
	}
}

func TestAggregatedUsageScheduleSaveCoalescesConcurrentAdds(t *testing.T) {
	var activeSaves int32
	var maxActiveSaves int32
	var saveCount int32

	usage := &AggregatedUsage{
		Combinations: make([]CostCombination, 0),
		saveInterval: time.Hour,
		saveHandler: func() error {
			active := atomic.AddInt32(&activeSaves, 1)
			for {
				maxActive := atomic.LoadInt32(&maxActiveSaves)
				if active <= maxActive || atomic.CompareAndSwapInt32(&maxActiveSaves, maxActive, active) {
					break
				}
			}
			atomic.AddInt32(&saveCount, 1)
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt32(&activeSaves, -1)
			return nil
		},
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := usage.AddUsage("claude-sonnet-4-5", contextWindowStandard, 1, 1, 1, 0, 0, 0, 0, ""); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	t.Cleanup(usage.cancelPendingSave)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&saveCount) > 0 && atomic.LoadInt32(&activeSaves) == 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	if got := atomic.LoadInt32(&maxActiveSaves); got != 1 {
		t.Fatalf("max concurrent saves = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&saveCount); got != 1 {
		t.Fatalf("save count = %d, want 1", got)
	}
}

func TestAggregatedUsageCancelPendingSaveWaitsForRunningScheduledSave(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once

	usage := &AggregatedUsage{
		Combinations: make([]CostCombination, 0),
		saveHandler: func() error {
			startedOnce.Do(func() {
				close(started)
			})
			<-release
			return nil
		},
	}

	go usage.runScheduledSave()
	<-started

	done := make(chan struct{})
	go func() {
		usage.cancelPendingSave()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("cancelPendingSave returned while scheduled save was still running")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelPendingSave did not return after scheduled save completed")
	}
}

func TestAggregatedUsageCancelPendingSaveIgnoresFiredCallbackAfterClose(t *testing.T) {
	var saveCount int32

	usage := &AggregatedUsage{
		Combinations: make([]CostCombination, 0),
		saveHandler: func() error {
			atomic.AddInt32(&saveCount, 1)
			return nil
		},
	}

	usage.cancelPendingSave()
	usage.runScheduledSave()

	if got := atomic.LoadInt32(&saveCount); got != 0 {
		t.Fatalf("save count after scheduler close = %d, want 0", got)
	}
}

func TestServiceCloseClosesIdleConnections(t *testing.T) {
	transport := &closeIdleTransport{}
	service := &Service{
		httpClient: &http.Client{Transport: transport},
	}

	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	if !transport.closed.Load() {
		t.Fatal("idle connections were not closed")
	}
}
