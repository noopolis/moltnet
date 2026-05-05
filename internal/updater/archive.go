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
	var binary bytes.Buffer
	var binaryMode os.FileMode
	foundBinary := false
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
			return "", fmt.Errorf("release archive contains unexpected member %q", header.Name)
		}
		if foundBinary {
			return "", fmt.Errorf("release archive contains duplicate %s binary", archiveBinaryName)
		}
		if err := validateArchiveEntryType(header); err != nil {
			return "", err
		}
		if _, err := io.Copy(&binary, tarReader); err != nil {
			return "", err
		}
		binaryMode = header.FileInfo().Mode()
		foundBinary = true
	}

	if !foundBinary {
		return "", fmt.Errorf("release archive did not contain %s", archiveBinaryName)
	}
	return writeExtractedBinary(destinationDir, bytes.NewReader(binary.Bytes()), binaryMode)
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

func validateArchiveEntryType(header *tar.Header) error {
	switch header.Typeflag {
	case tar.TypeReg, tar.TypeRegA:
		return nil
	case tar.TypeSymlink:
		return fmt.Errorf("release archive contains symlink %q", header.Name)
	case tar.TypeLink:
		return fmt.Errorf("release archive contains hardlink %q", header.Name)
	case tar.TypeChar, tar.TypeBlock, tar.TypeFifo:
		return fmt.Errorf("release archive contains device or special file %q", header.Name)
	default:
		return fmt.Errorf("release archive entry %q is not a regular file", header.Name)
	}
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
