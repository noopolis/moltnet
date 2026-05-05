package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractMoltnetBinaryRejectsUnsafePaths(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "parent", path: "../evil"},
		{name: "absolute", path: "/tmp/evil"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parent := t.TempDir()
			destination := filepath.Join(parent, "extract")
			archive := archiveWithEntries(t, archiveEntry{name: test.path, body: "nope"})

			if _, err := ExtractMoltnetBinary(archive, destination); err == nil {
				t.Fatal("expected unsafe path error")
			}
			if _, err := os.Stat(filepath.Join(parent, "evil")); !os.IsNotExist(err) {
				t.Fatalf("unsafe archive wrote outside destination, stat error = %v", err)
			}
		})
	}
}

func TestExtractMoltnetBinaryRejectsUnexpectedMembers(t *testing.T) {
	destination := t.TempDir()
	archive := archiveWithEntries(t,
		archiveEntry{name: "moltnet", body: "#!/bin/sh\n", mode: 0o755},
		archiveEntry{name: "README", body: "text", mode: 0o644},
	)

	if _, err := ExtractMoltnetBinary(archive, destination); err == nil {
		t.Fatal("expected unexpected member error")
	}
	if _, err := os.Stat(filepath.Join(destination, "moltnet")); !os.IsNotExist(err) {
		t.Fatalf("invalid archive wrote binary, stat error = %v", err)
	}
}

func TestExtractMoltnetBinaryRequiresBinary(t *testing.T) {
	archive := archiveWithEntries(t)
	if _, err := ExtractMoltnetBinary(archive, t.TempDir()); err == nil {
		t.Fatal("expected missing binary error")
	}
}

func TestExtractMoltnetBinaryRejectsUnsafeTypes(t *testing.T) {
	tests := []struct {
		name     string
		typeflag byte
		want     string
	}{
		{name: "symlink", typeflag: tar.TypeSymlink, want: "symlink"},
		{name: "hardlink", typeflag: tar.TypeLink, want: "hardlink"},
		{name: "char device", typeflag: tar.TypeChar, want: "device or special file"},
		{name: "block device", typeflag: tar.TypeBlock, want: "device or special file"},
		{name: "fifo", typeflag: tar.TypeFifo, want: "device or special file"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := archiveWithEntries(t, archiveEntry{
				name:     "moltnet",
				typeflag: test.typeflag,
				linkname: "target",
			})

			if _, err := ExtractMoltnetBinary(archive, t.TempDir()); err == nil {
				t.Fatalf("expected %s rejection", test.want)
			} else if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q in error %v", test.want, err)
			}
		})
	}
}

type archiveEntry struct {
	body     string
	linkname string
	mode     int64
	name     string
	typeflag byte
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
		typeflag := entry.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		size := int64(len(entry.body))
		if typeflag != tar.TypeReg && typeflag != 0 {
			size = 0
		}
		header := &tar.Header{
			Linkname: entry.linkname,
			Mode:     mode,
			Name:     entry.name,
			Size:     size,
			Typeflag: typeflag,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if size > 0 {
			if _, err := tarWriter.Write([]byte(entry.body)); err != nil {
				t.Fatalf("write tar body: %v", err)
			}
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
