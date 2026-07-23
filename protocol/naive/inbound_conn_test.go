package naive

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestPaddingReaderReplaceableOnlyAfterFinalFrame(t *testing.T) {
	payload := []byte("abcdefgh")
	padding := []byte{1, 2, 3, 4, 5}
	trailing := []byte("raw")
	frame := make([]byte, 3, 3+len(payload)+len(padding)+len(trailing))
	binary.BigEndian.PutUint16(frame[:2], uint16(len(payload)))
	frame[2] = byte(len(padding))
	frame = append(frame, payload...)
	frame = append(frame, padding...)
	frame = append(frame, trailing...)

	reader := bytes.NewReader(frame)
	paddingReader := &paddingConn{readPadding: paddingCount - 1}
	first := make([]byte, 4)
	n, err := paddingReader.readWithPadding(reader, first)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(first) || !bytes.Equal(first, payload[:len(first)]) {
		t.Fatalf("unexpected first payload: %q", first[:n])
	}
	if paddingReader.readerReplaceable() {
		t.Fatal("reader became replaceable before the final payload and padding were consumed")
	}

	remaining := make([]byte, len(payload)-len(first))
	n, err = paddingReader.readWithPadding(reader, remaining)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(remaining) || !bytes.Equal(remaining, payload[len(first):]) {
		t.Fatalf("unexpected remaining payload: %q", remaining[:n])
	}
	if paddingReader.readerReplaceable() {
		t.Fatal("reader became replaceable before the final padding was consumed")
	}

	next := make([]byte, len(trailing))
	n, err = paddingReader.readWithPadding(reader, next)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(next) || !bytes.Equal(next, trailing) {
		t.Fatalf("unexpected trailing payload: %q", next[:n])
	}
	if !paddingReader.readerReplaceable() {
		t.Fatal("reader did not become replaceable after the final frame")
	}
}
