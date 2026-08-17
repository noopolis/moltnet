package updater

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// installDirNotWritableError names the exact fix (chmod/chown) for an
// unwritable install directory. Shared by ensureInstallDirWritable's early
// preflight and replaceInstalledBinary's own staging step, so operators see
// identical wording no matter which one catches the problem first.
func installDirNotWritableError(directory string) error {
	return fmt.Errorf("installed binary directory %q is not writable; run `chmod u+w %s` (or fix ownership with `sudo chown $(whoami) %s`), then retry `moltnet update`", directory, directory, directory)
}

// ensureInstallDirWritable probes filepath.Dir(installPath) with a
// create-and-remove temp file. A source update calls this before it pulls
// or builds anything, so an unwritable install directory is refused up
// front instead of only being discovered inside replaceInstalledBinary
// after a full pull and build has already run.
func ensureInstallDirWritable(installPath string) error {
	directory := filepath.Dir(installPath)
	probe, err := os.CreateTemp(directory, ".moltnet-update-probe-*")
	if err != nil {
		return installDirNotWritableError(directory)
	}
	probePath := probe.Name()
	_ = probe.Close()
	_ = os.Remove(probePath)
	return nil
}

// replaceInstalledBinary atomically installs newBinaryPath (a binary freshly
// built from a source checkout) over installPath. It stages the new binary
// into a temp file in installPath's own directory first — so the final
// rename always lands on the same filesystem even when newBinaryPath itself
// lives elsewhere, such as inside a source checkout mounted on a different
// volume than the install directory — then backs up the current installed
// binary with a plain copy (installPath itself is never renamed away or
// unlinked) before a single atomic rename swaps the staged binary in.
// Because installPath keeps its original content until that one rename
// either fully completes or fully fails, a crash or a permission error
// between the backup and the swap always leaves a runnable binary at
// installPath — never nothing, unlike a rename-then-rename sequence that
// unlinks installPath in between.
func replaceInstalledBinary(installPath string, newBinaryPath string) (string, error) {
	if strings.TrimSpace(installPath) == "" {
		return "", errors.New("install path is empty")
	}
	directory := filepath.Dir(installPath)

	builtInfo, err := os.Stat(newBinaryPath)
	if err != nil {
		return "", fmt.Errorf("inspect freshly built binary %q: %w", newBinaryPath, err)
	}
	// Preserve the INSTALLED binary's own executable mode when one exists
	// (an operator may have chmod'd it deliberately — e.g. setgid for a
	// shared install) rather than blindly taking whatever mode the freshly
	// built file happens to carry.
	mode := builtInfo.Mode().Perm() | 0o100
	if installedInfo, statErr := os.Stat(installPath); statErr == nil {
		mode = installedInfo.Mode().Perm() | 0o100
	}

	staged, err := os.CreateTemp(directory, ".moltnet-update-*")
	if err != nil {
		return "", installDirNotWritableError(directory)
	}
	stagedPath := staged.Name()
	keepStaged := false
	defer func() {
		if !keepStaged {
			_ = os.Remove(stagedPath)
		}
	}()

	source, err := os.Open(newBinaryPath)
	if err != nil {
		_ = staged.Close()
		return "", err
	}
	_, copyErr := io.Copy(staged, source)
	_ = source.Close()
	closeErr := staged.Close()
	if copyErr != nil {
		return "", fmt.Errorf("stage freshly built binary: %w", copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("stage freshly built binary: %w", closeErr)
	}
	if err := os.Chmod(stagedPath, mode); err != nil {
		return "", fmt.Errorf("preserve executable bit on staged binary: %w", err)
	}

	backupPath := installPath + ".previous"
	if err := copyRegularFile(installPath, backupPath); err != nil {
		return "", fmt.Errorf("back up existing binary: %w", err)
	}

	if err := os.Rename(stagedPath, installPath); err != nil {
		return "", fmt.Errorf("replace binary: %w", err)
	}
	keepStaged = true
	return backupPath, nil
}

// copyRegularFile copies sourcePath's contents (and permission bits) to
// destinationPath, overwriting it if it already exists.
func copyRegularFile(sourcePath string, destinationPath string) error {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return err
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(destination, source); err != nil {
		_ = destination.Close()
		return err
	}
	if err := destination.Close(); err != nil {
		return err
	}
	return os.Chmod(destinationPath, info.Mode().Perm())
}
