package ziparchive

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/service/filemanager"
)

type Limits struct {
	MaxArchiveBytes int64
	MaxEntryBytes   int64
	MaxTotalBytes   int64
	MaxFiles        int
}

var DefaultLimits = Limits{
	MaxArchiveBytes: 64 << 20,
	MaxEntryBytes:   32 << 20,
	MaxTotalBytes:   128 << 20,
	MaxFiles:        4096,
}

func CopyToTemp(ctx context.Context, body io.Reader, pattern string, maxBytes int64) (string, func(), error) {
	tempFile, err := filemanager.CreateTemp(ctx, pattern)
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		os.Remove(tempFile.Name())
	}
	_, copyErr := CopyLimited(tempFile, body, maxBytes)
	closeErr := tempFile.Close()
	if copyErr != nil {
		cleanup()
		return "", nil, copyErr
	}
	if closeErr != nil {
		cleanup()
		return "", nil, closeErr
	}
	return tempFile.Name(), cleanup, nil
}

func Extract(ctx context.Context, files []*zip.File, output string, limits Limits) error {
	trimDir := IsInSingleDirectory(files)
	var (
		totalBytes int64
		fileCount  int
	)
	for _, file := range files {
		if file.FileInfo().IsDir() {
			continue
		}
		relativePath, err := EntryRelativePath(file.Name, trimDir)
		if err != nil {
			return err
		}
		if relativePath == "" {
			continue
		}
		fileCount++
		if limits.MaxFiles > 0 && fileCount > limits.MaxFiles {
			return E.New("archive file count exceeds limit: ", limits.MaxFiles)
		}
		if limits.MaxEntryBytes > 0 && file.UncompressedSize64 > uint64(limits.MaxEntryBytes) {
			return E.New("archive entry size exceeds limit: ", file.Name)
		}
		remainingTotal := limits.MaxTotalBytes - totalBytes
		entryLimit := limits.MaxEntryBytes
		if limits.MaxTotalBytes > 0 && (entryLimit <= 0 || remainingTotal < entryLimit) {
			entryLimit = remainingTotal
		}
		if entryLimit < 0 {
			return E.New("archive total size exceeds limit: ", limits.MaxTotalBytes)
		}
		savePath := filepath.Join(output, relativePath)
		err = filemanager.MkdirAll(ctx, filepath.Dir(savePath), 0o755)
		if err != nil {
			return err
		}
		written, err := extractEntry(ctx, file, savePath, entryLimit)
		if err != nil {
			return err
		}
		totalBytes += written
		if limits.MaxTotalBytes > 0 && totalBytes > limits.MaxTotalBytes {
			return E.New("archive total size exceeds limit: ", limits.MaxTotalBytes)
		}
	}
	return nil
}

func EntryRelativePath(name string, trimDir bool) (string, error) {
	pathElements := strings.Split(name, "/")
	if trimDir {
		if len(pathElements) <= 1 {
			return "", nil
		}
		pathElements = pathElements[1:]
	}
	relativePath := filepath.Join(pathElements...)
	if relativePath == "." || relativePath == "" {
		return "", nil
	}
	if !filepath.IsLocal(relativePath) {
		return "", E.New("invalid archive entry: ", name)
	}
	return relativePath, nil
}

func IsInSingleDirectory(files []*zip.File) bool {
	var singleDirectory string
	for _, file := range files {
		if file.FileInfo().IsDir() {
			continue
		}
		pathElements := strings.Split(file.Name, "/")
		if len(pathElements) < 2 {
			return false
		}
		if singleDirectory == "" {
			singleDirectory = pathElements[0]
		} else if singleDirectory != pathElements[0] {
			return false
		}
	}
	return true
}

func extractEntry(ctx context.Context, zipFile *zip.File, savePath string, maxBytes int64) (int64, error) {
	reader, err := zipFile.Open()
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	writer, err := filemanager.Create(ctx, savePath)
	if err != nil {
		return 0, err
	}
	defer writer.Close()
	return CopyLimited(writer, reader, maxBytes)
}

func CopyLimited(writer io.Writer, reader io.Reader, maxBytes int64) (int64, error) {
	if maxBytes < 0 {
		return 0, E.New("copy size exceeds limit")
	}
	if maxBytes == 0 {
		return io.Copy(writer, reader)
	}
	limitedReader := &io.LimitedReader{R: reader, N: maxBytes}
	written, err := io.Copy(writer, limitedReader)
	if err != nil {
		return written, err
	}
	if limitedReader.N == 0 {
		var probe [1]byte
		n, readErr := reader.Read(probe[:])
		if n > 0 {
			return written, E.New("copy size exceeds limit: ", maxBytes)
		}
		if readErr != nil && readErr != io.EOF {
			return written, readErr
		}
	}
	return written, nil
}
