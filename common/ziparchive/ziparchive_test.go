package ziparchive

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEntryRelativePathRejectsTrimmedTraversal(t *testing.T) {
	_, err := EntryRelativePath("root/../../escape.txt", true)
	if err == nil {
		t.Fatal("expected traversal entry to be rejected")
	}
}

func TestExtractWritesSingleDirectoryArchiveSafely(t *testing.T) {
	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	writer, err := zipWriter.Create("root/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	if err = zipWriter.Close(); err != nil {
		t.Fatal(err)
	}

	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	output := t.TempDir()
	err = Extract(context.Background(), reader.File, output, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(output, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "ok" {
		t.Fatalf("content = %q, want ok", content)
	}
}

func TestExtractRejectsEntryOverLimit(t *testing.T) {
	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	writer, err := zipWriter.Create("root/large.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Write([]byte(strings.Repeat("x", 8))); err != nil {
		t.Fatal(err)
	}
	if err = zipWriter.Close(); err != nil {
		t.Fatal(err)
	}

	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	err = Extract(context.Background(), reader.File, t.TempDir(), Limits{MaxEntryBytes: 4, MaxTotalBytes: 64, MaxFiles: 16})
	if err == nil {
		t.Fatal("expected oversized entry to be rejected")
	}
}
