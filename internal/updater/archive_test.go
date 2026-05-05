package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractMoltnetBinaryRejectsUnsafePaths(t *testing.T) {
	parent := t.TempDir()
	destination := filepath.Join(parent, "extract")
	archive := archiveWithEntries(t, archiveEntry{name: "../evil", body: "nope"})

	if _, err := ExtractMoltnetBinary(archive, destination); err == nil {
		t.Fatal("expected unsafe path error")
	}
	if _, err := os.Stat(filepath.Join(parent, "evil")); !os.IsNotExist(err) {
		t.Fatalf("unsafe archive wrote outside destination, stat error = %v", err)
	}
}

func TestExtractMoltnetBinaryRequiresBinary(t *testing.T) {
	archive := archiveWithEntries(t, archiveEntry{name: "README", body: "text"})
	if _, err := ExtractMoltnetBinary(archive, t.TempDir()); err == nil {
		t.Fatal("expected missing binary error")
	}
}

type archiveEntry struct {
	body string
	mode int64
	name string
}

func archiveWithEntries(t *testing.T, entries ...archiveEntry) []byte {
	t.Helper()

	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		mode := entry.mode
		if mode == 0 {
			mode = 0o755
		}
		header := &tar.Header{
			Mode: mode,
			Name: entry.name,
			Size: int64(len(entry.body)),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tarWriter.Write([]byte(entry.body)); err != nil {
			t.Fatalf("write tar body: %v", err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return buffer.Bytes()
}
