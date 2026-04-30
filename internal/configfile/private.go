package configfile

import (
	"fmt"
	"os"
)

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
