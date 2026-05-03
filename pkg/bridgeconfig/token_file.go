package bridgeconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ReadTokenFile(path string) (string, error) {
	tokenPath := strings.TrimSpace(path)
	if tokenPath == "" {
		return "", fmt.Errorf("moltnet token_path is required")
	}

	if err := validateTokenFileForRead(tokenPath); err != nil {
		return "", err
	}

	contents, err := os.ReadFile(tokenPath)
	if err != nil {
		return "", fmt.Errorf("read moltnet token_path %q: %w", tokenPath, err)
	}

	token := strings.TrimSpace(string(contents))
	if token == "" {
		return "", fmt.Errorf("moltnet token_path %q is empty", tokenPath)
	}
	return token, nil
}

func WriteTokenFile(path string, token string) error {
	tokenPath := strings.TrimSpace(path)
	if tokenPath == "" {
		return fmt.Errorf("moltnet token_path is required")
	}

	value := strings.TrimSpace(token)
	if value == "" {
		return fmt.Errorf("moltnet token is empty")
	}

	if err := validateExistingTokenFile(tokenPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	dir := filepath.Dir(tokenPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create moltnet token_path directory %q: %w", dir, err)
	}

	temp, err := os.CreateTemp(dir, ".moltnet-token-*")
	if err != nil {
		return fmt.Errorf("create temporary moltnet token file: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()

	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("chmod temporary moltnet token file: %w", err)
	}
	if _, err := temp.WriteString(value + "\n"); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temporary moltnet token file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary moltnet token file: %w", err)
	}

	if err := os.Rename(tempPath, tokenPath); err != nil {
		return fmt.Errorf("replace moltnet token_path %q: %w", tokenPath, err)
	}
	if err := os.Chmod(tokenPath, 0o600); err != nil {
		return fmt.Errorf("chmod moltnet token_path %q: %w", tokenPath, err)
	}
	return nil
}

func PrepareTokenFileWrite(path string) error {
	tokenPath := strings.TrimSpace(path)
	if tokenPath == "" {
		return fmt.Errorf("moltnet token_path is required")
	}
	if err := validateExistingTokenFile(tokenPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		dir := filepath.Dir(tokenPath)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create moltnet token_path directory %q: %w", dir, err)
		}
		return validateTokenDirectoryWritable(dir)
	}
	return nil
}

func validateTokenDirectoryWritable(dir string) error {
	temp, err := os.CreateTemp(dir, ".moltnet-token-probe-*")
	if err != nil {
		return fmt.Errorf("prepare moltnet token_path directory %q: %w", dir, err)
	}
	tempPath := temp.Name()
	if closeErr := temp.Close(); closeErr != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("close temporary moltnet token file: %w", closeErr)
	}
	if err := os.Remove(tempPath); err != nil {
		return fmt.Errorf("remove temporary moltnet token file: %w", err)
	}
	return nil
}

func validateTokenFileForRead(path string) error {
	if err := validateExistingTokenFile(path); err != nil {
		return err
	}
	return nil
}

func validateExistingTokenFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat moltnet token_path %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("moltnet token_path %q must not be a symlink", path)
	}
	if info.IsDir() {
		return fmt.Errorf("moltnet token_path %q must be a file", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("moltnet token_path %q must not be group/world readable", path)
	}
	return nil
}
