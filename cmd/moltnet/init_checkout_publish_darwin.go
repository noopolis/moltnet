package main

import (
	"errors"
	"os"
)

// publishNoReplace is writeFileIfMissing's (init_checkout.go) no-clobber
// publish step on Darwin, which has no renameat2/RENAME_NOREPLACE (see the
// Linux sibling file's doc comment for what that call buys elsewhere). The
// portable substitute for "atomically claim this path, never replacing an
// existing entry" is a hard link: link(2) creates destPath pointing at the
// same inode as tempPath, and -- like RENAME_NOREPLACE -- refuses with
// EEXIST the instant destPath already exists in any form, symlink included
// (dangling or not), without ever following it. That is exactly the
// refusal semantics writeFileIfMissing's contract requires (see its own
// doc comment), and exactly what let
// TestWriteFileIfMissingRefusesSymlinkTarget and
// TestWriteFileIfMissingRefusesDanglingSymlink keep passing unchanged
// through this switch from a single O_CREATE|O_EXCL open.
//
// It reports (true, nil) once destPath holds tempPath's content (tempPath
// itself is then unlinked, since the two names would otherwise both point
// at the same inode indefinitely), and (false, nil) when destPath already
// existed and nothing was written -- writeFileIfMissing's existing
// "already exists" contract. Any other error is returned as-is.
func publishNoReplace(tempPath, destPath string) (bool, error) {
	if err := os.Link(tempPath, destPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		return false, err
	}
	// destPath now durably holds the published content; tempPath is a
	// second, now-redundant name for the same inode. Removing it is tidy-up,
	// not part of the no-clobber guarantee itself (which link() above
	// already provided), so a failure here does not turn a successful
	// publish into a reported error -- it would only leave a harmless
	// leftover 0600 temp file, not a correctness problem. The caller's own
	// temp-cleanup defer (writeFileIfMissing) is skipped once this function
	// reports true, so a Remove failure here is not retried or surfaced
	// twice.
	_ = os.Remove(tempPath)
	return true, nil
}
