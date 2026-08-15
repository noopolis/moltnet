package uninstall

import (
	"fmt"
	"os"
	"strings"
)

// RemoveBinary deletes the file at path — the running moltnet executable,
// resolved by the caller via os.Executable + filepath.EvalSymlinks.
// Removing an executable's own backing file while the process is still
// running is safe on macOS and Linux: the kernel keeps the inode alive
// until the last open file descriptor/mapping closes, so only the
// directory entry disappears, immediately.
//
// os.Remove's *PathError already names path, so its error is returned
// as-is rather than re-wrapped; errors.Is(err, fs.ErrPermission) still
// matches through it, which callers use to print a `sudo rm` fallback (a
// root-owned install directory such as /usr/local/bin) instead of
// crashing.
func RemoveBinary(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("binary path is empty")
	}
	return os.Remove(path)
}
