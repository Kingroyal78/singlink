package sniff

import (
	"errors"
	"io"
	"testing"
)

func TestParseQUICCryptoFramesRejectsTruncatedCryptoPayload(t *testing.T) {
	_, _, err := parseQUICCryptoFrames([]byte{
		0x06, // CRYPTO
		0x00, // offset
		0x04, // length
		0x01, 0x02,
	})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("parse error = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestParseQUICCryptoFramesSkipsZeroLengthCryptoPayload(t *testing.T) {
	fragments, frameTypes, err := parseQUICCryptoFrames([]byte{
		0x06, // CRYPTO
		0x00, // offset
		0x00, // length
		0x01, // PING
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fragments) != 0 {
		t.Fatalf("fragments length = %d, want 0", len(fragments))
	}
	if len(frameTypes) != 2 || frameTypes[0] != 0x06 || frameTypes[1] != 0x01 {
		t.Fatalf("frame types = %v, want [6 1]", frameTypes)
	}
}
