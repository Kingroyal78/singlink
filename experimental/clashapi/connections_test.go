package clashapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseConnectionsIntervalBounds(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     time.Duration
		wantOkay bool
	}{
		{name: "default", input: "", want: time.Second, wantOkay: true},
		{name: "minimum", input: "100", want: 100 * time.Millisecond, wantOkay: true},
		{name: "maximum", input: "60000", want: time.Minute, wantOkay: true},
		{name: "below minimum", input: "99", wantOkay: false},
		{name: "zero", input: "0", wantOkay: false},
		{name: "negative", input: "-1", wantOkay: false},
		{name: "above maximum", input: "60001", wantOkay: false},
		{name: "invalid", input: "abc", wantOkay: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := parseConnectionsInterval(test.input)
			if ok != test.wantOkay {
				t.Fatalf("ok = %v, want %v", ok, test.wantOkay)
			}
			if got != test.want {
				t.Fatalf("interval = %v, want %v", got, test.want)
			}
		})
	}
}

func TestConnectionsRejectsInvalidWebsocketIntervalBeforeUpgrade(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/?interval=0", nil)
	request.Header.Set("Upgrade", "websocket")
	recorder := httptest.NewRecorder()

	getConnections(context.Background(), nil)(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}
