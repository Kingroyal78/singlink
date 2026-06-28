package transport

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestReadLimitedMessageBodyRejectsOversizedContentLength(t *testing.T) {
	response := &http.Response{
		ContentLength: MaxDNSMessageBytes + 1,
		Body:          io.NopCloser(strings.NewReader("")),
	}
	_, err := ReadLimitedMessageBody(response)
	if err == nil {
		t.Fatal("expected oversized content length to fail")
	}
}

func TestReadLimitedMessageBodyRejectsOversizedStream(t *testing.T) {
	response := &http.Response{
		ContentLength: -1,
		Body:          io.NopCloser(strings.NewReader(strings.Repeat("x", MaxDNSMessageBytes+1))),
	}
	_, err := ReadLimitedMessageBody(response)
	if err == nil {
		t.Fatal("expected oversized stream to fail")
	}
}
