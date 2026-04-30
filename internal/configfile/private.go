package configfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func DiscoverPath(explicit string, envKey string, candidates []string, label string) (string, bool, error) {
	if value := strings.TrimSpace(explicit); value != "" {
		return statPath(value, label)
	}
	if envKey != "" {
		if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
			return statPath(value, label)
		}
	}

	for _, candidate := range candidates {
		path, ok, err := statPath(candidate, label)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", false, err
		}
		if ok {
			return path, true, nil
		}
	}

	return "", false, nil
}

func statPath(path string, label string) (string, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false, fmt.Errorf("stat %s %q: %w", label, path, err)
	}
	if info.IsDir() {
		return "", false, fmt.Errorf("%s %q is a directory", label, path)
	}

	return path, true, nil
}

func ValidatePrivateMode(path string, label string, symlinkReason string, modeReason string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat %s %q: %w", label, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s %q must not be a symlink when %s are present", label, path, symlinkReason)
	}

	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s %q must not be group/world readable when %s are present", label, path, modeReason)
	}

	return nil
}

func FormatForPath(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return "json"
	}

	return "yaml"
}
