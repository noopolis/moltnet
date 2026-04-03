package bridgeconfig

import (
	"fmt"
	"os"
)

func validatePrivateConfigMode(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat bridge config %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("bridge config %q must not be a symlink when tokens are present", path)
	}

	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("bridge config %q must not be group/world readable when tokens are present", path)
	}

	return nil
}
