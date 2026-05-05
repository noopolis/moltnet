package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const archiveBinaryName = "moltnet"

func ExtractMoltnetBinary(archive []byte, destinationDir string) (string, error) {
	if err := os.MkdirAll(destinationDir, 0o700); err != nil {
		return "", err
	}

	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return "", fmt.Errorf("open release archive: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read release archive: %w", err)
		}
		cleanName, err := cleanArchiveName(header.Name)
		if err != nil {
			return "", err
		}
		if cleanName != archiveBinaryName {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return "", fmt.Errorf("release archive entry %q is not a regular file", header.Name)
		}
		return writeExtractedBinary(destinationDir, tarReader, header.FileInfo().Mode())
	}

	return "", fmt.Errorf("release archive did not contain %s", archiveBinaryName)
}

func cleanArchiveName(name string) (string, error) {
	slashed := filepath.ToSlash(strings.TrimSpace(name))
	if slashed == "" {
		return "", fmt.Errorf("release archive contains an empty path")
	}
	if path.IsAbs(slashed) {
		return "", fmt.Errorf("release archive contains absolute path %q", name)
	}
	clean := path.Clean(slashed)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("release archive contains unsafe path %q", name)
	}
	return clean, nil
}

func writeExtractedBinary(destinationDir string, reader io.Reader, mode os.FileMode) (string, error) {
	path := filepath.Join(destinationDir, archiveBinaryName)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := io.Copy(file, reader); err != nil {
		return "", err
	}
	if mode.Perm()&0o111 == 0 {
		mode = 0o755
	}
	if err := file.Chmod(mode.Perm()); err != nil {
		return "", err
	}
	return path, nil
}
