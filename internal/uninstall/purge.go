package uninstall

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HomeState describes what root (typically ~/.moltnet, or a MOLTNET_HOME
// override) actually is on disk before anything is removed: a real
// directory holding data, a symlink (and what it resolves to, if
// anything), or nothing at all. InspectHomeDir and PurgeHomeDir share this
// so the CLI layer can describe an accurate --purge confirmation prompt
// before asking, and an accurate outcome after acting, from the same
// underlying check.
type HomeState struct {
	// Existed is true when there was something at root to act on. False
	// means root was already gone.
	Existed bool
	// IsSymlink is true when root itself is a symlink rather than a real
	// directory.
	IsSymlink bool
	// SymlinkTarget is the resolved target when IsSymlink is true and the
	// target still exists; empty when IsSymlink is true but the symlink is
	// itself dangling (its target no longer exists).
	SymlinkTarget string
}

// InspectHomeDir reports root's HomeState without removing anything, so
// callers can build an accurate --purge confirmation prompt (PLAN.md phase
// 5 item 2: name what --purge is really about to do, before asking) ahead
// of calling PurgeHomeDir.
func InspectHomeDir(root string) (HomeState, error) {
	return inspectHome(root)
}

func inspectHome(root string) (HomeState, error) {
	info, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return HomeState{}, nil
		}
		return HomeState{}, fmt.Errorf("inspect %q: %w", root, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return HomeState{Existed: true}, nil
	}
	target, evalErr := filepath.EvalSymlinks(root)
	if evalErr != nil {
		target = ""
	}
	return HomeState{Existed: true, IsSymlink: true, SymlinkTarget: target}, nil
}

// PurgeResult reports what PurgeHomeDir actually did, built from the same
// HomeState InspectHomeDir would have reported beforehand.
type PurgeResult struct {
	HomeState
}

// PurgeHomeDir removes root (the ~/.moltnet home directory, or a
// MOLTNET_HOME install-state override) entirely, deleting every network's
// config, message history, and credentials — unless root is itself a
// symlink. In that case only the link is removed; the resolved target is
// left untouched, since following an operator-controlled symlink to delete
// whatever it happens to point at is too dangerous to do unconditionally.
// Callers must have already confirmed this with the operator — PLAN.md
// phase 5 item 2 requires --purge to always be confirmed, always listing
// the networks that will be destroyed, before this runs. A missing root is
// not an error: the returned PurgeResult.Existed is simply false.
//
// Before touching anything, PurgeHomeDir refuses to run at all when root
// resolves to something far broader than a moltnet install directory —
// the user's actual $HOME, an ancestor of $HOME (such as / or /Users), or
// the filesystem root — since a misresolved MOLTNET_HOME (e.g. accidentally
// set to $HOME or /) combined with --purge --yes would otherwise silently
// os.RemoveAll the operator's entire home directory. This check lives here
// rather than in the CLI layer so every caller of PurgeHomeDir, present and
// future, gets it for free.
func PurgeHomeDir(root string) (PurgeResult, error) {
	if strings.TrimSpace(root) == "" {
		return PurgeResult{}, fmt.Errorf("moltnet home directory is empty")
	}
	if reason := dangerousPurgeReason(root); reason != "" {
		return PurgeResult{}, fmt.Errorf("refusing to purge %q: %s", filepath.Clean(root), reason)
	}

	state, err := inspectHome(root)
	if err != nil {
		return PurgeResult{}, err
	}
	if !state.Existed {
		return PurgeResult{}, nil
	}

	if state.IsSymlink {
		if err := os.Remove(root); err != nil {
			return PurgeResult{}, fmt.Errorf("remove symlink %q: %w", root, err)
		}
		return PurgeResult{HomeState: state}, nil
	}

	if err := os.RemoveAll(root); err != nil {
		return PurgeResult{}, fmt.Errorf("remove %q: %w", root, err)
	}
	return PurgeResult{HomeState: state}, nil
}

// dangerousPurgeReason reports, in one human-readable sentence, why root is
// too broad to hand to os.RemoveAll — or "" when root looks like an
// ordinary moltnet install directory. It never touches the filesystem
// beyond os.UserHomeDir(): every other check here is pure path arithmetic,
// so it is safe to call before root is known to exist.
func dangerousPurgeReason(root string) string {
	cleanRoot := filepath.Clean(root)
	if absolute, err := filepath.Abs(cleanRoot); err == nil {
		cleanRoot = absolute
	}

	if isFilesystemRoot(cleanRoot) {
		return fmt.Sprintf("%s is the filesystem root", cleanRoot)
	}

	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	cleanHome := filepath.Clean(home)

	if cleanRoot == cleanHome {
		return fmt.Sprintf("%s is the user's home directory", cleanRoot)
	}
	if isAncestorDir(cleanRoot, cleanHome) {
		return fmt.Sprintf("%s is an ancestor of the user's home directory (%s)", cleanRoot, cleanHome)
	}
	return ""
}

// isFilesystemRoot reports whether path is its own parent — true only for
// "/" on darwin/linux, the two platforms internal/service (and therefore
// `moltnet uninstall`) supports.
func isFilesystemRoot(path string) bool {
	return path == filepath.Dir(path)
}

// isAncestorDir reports whether parent strictly contains child (parent !=
// child, and child is somewhere underneath it) using filepath.Rel rather
// than string prefixing, so "/Users2" is never mistaken for an ancestor of
// "/Users2Extra" and a MOLTNET_HOME nested inside $HOME (a legitimate,
// narrower override) is never mistaken for containing $HOME.
func isAncestorDir(parent string, child string) bool {
	if parent == child {
		return false
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
