package uninstall

import (
	"os"
	"path/filepath"
	"strings"
)

// binaryName is the executable moltnet looks for on PATH when warning about
// other installed copies. Windows is out of scope: internal/service (and
// therefore `moltnet uninstall`) only supports darwin and linux.
const binaryName = "moltnet"

// OtherCopies scans each directory in pathEnv (a $PATH-style,
// os.PathListSeparator-joined string) for an executable file named
// "moltnet" that is not currentBinary, and returns their paths in PATH
// order, deduplicated. Both currentBinary and each candidate are
// symlink-resolved before comparison, so a PATH entry that is just a
// symlink to the binary actually running is not reported as "another
// copy".
//
// OtherCopies tolerates being called after currentBinary has already been
// removed from disk: a candidate that resolves back to that now-deleted
// path simply fails os.Stat and is silently skipped, the same as any other
// nonexistent PATH entry — see DanglingSymlinks for the complementary scan
// that catches that specific case instead of dropping it.
func OtherCopies(pathEnv string, currentBinary string) []string {
	resolvedCurrent := resolveIfPossible(currentBinary)

	var others []string
	seen := map[string]bool{}
	for _, dir := range filepath.SplitList(pathEnv) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}

		candidate := filepath.Join(dir, binaryName)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Mode().Perm()&0o111 == 0 {
			continue
		}

		resolvedCandidate := resolveIfPossible(candidate)
		if resolvedCandidate == resolvedCurrent || seen[resolvedCandidate] {
			continue
		}
		seen[resolvedCandidate] = true
		others = append(others, candidate)
	}
	return others
}

// DanglingSymlinks scans pathEnv for a "moltnet" symlink that no longer
// resolves — most commonly a PATH entry such as ~/.local/bin/moltnet that
// pointed at removedBinary, the moltnet binary `moltnet uninstall` just
// deleted. Call this AFTER removing the binary, not before: OtherCopies
// resolves through os.Stat, which fails on a now-dangling symlink exactly
// like it fails on "nothing here", so a symlink to the deleted binary
// silently disappears from OtherCopies' results instead of being reported —
// this function exists to catch that case and warn about it explicitly. A
// candidate counts as dangling when EvalSymlinks fails to resolve it (the
// usual case: the target file is simply gone) or, defensively, when it
// still resolves but lands back on removedBinary itself.
func DanglingSymlinks(pathEnv string, removedBinary string) []string {
	resolvedRemoved := resolveIfPossible(removedBinary)

	var dangling []string
	seen := map[string]bool{}
	for _, dir := range filepath.SplitList(pathEnv) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}

		candidate := filepath.Join(dir, binaryName)
		if seen[candidate] {
			continue
		}

		info, err := os.Lstat(candidate)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}

		resolved, evalErr := filepath.EvalSymlinks(candidate)
		if evalErr == nil && resolved != resolvedRemoved {
			continue
		}
		seen[candidate] = true
		dangling = append(dangling, candidate)
	}
	return dangling
}

// resolveIfPossible symlink-resolves path, falling back to an absolute path
// and finally the original value when resolution fails (a dangling symlink,
// a permission-denied parent directory), so a single unresolvable PATH
// entry never aborts the whole scan.
func resolveIfPossible(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return path
}
