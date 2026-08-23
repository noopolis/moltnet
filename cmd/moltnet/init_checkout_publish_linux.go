package main

import (
	"errors"

	"golang.org/x/sys/unix"
)

// publishNoReplace is writeFileIfMissing's (init_checkout.go) no-clobber
// publish step on Linux. renameat2 with RENAME_NOREPLACE is an atomic,
// kernel-level rename that refuses with EEXIST the instant destPath already
// exists in any form -- regular file, directory, or symlink, dangling or
// not -- without ever following it, exactly preserving the same refusal
// semantics the old O_CREATE|O_EXCL open gave writeFileIfMissing before it
// needed same-directory-temp-plus-publish for atomicity against a
// truncated write on interruption (see writeFileIfMissing's doc comment
// for the full picture). Unlike a plain os.Rename, it never silently
// replaces an existing destination.
//
// It reports (true, nil) once tempPath has been published to destPath
// (tempPath no longer exists under its original name either way: the
// rename itself consumes it), and (false, nil) when destPath already
// existed and nothing was written -- writeFileIfMissing's existing
// "already exists" contract. Any other error is returned as-is.
func publishNoReplace(tempPath, destPath string) (bool, error) {
	err := unix.Renameat2(unix.AT_FDCWD, tempPath, unix.AT_FDCWD, destPath, unix.RENAME_NOREPLACE)
	if err == nil {
		return true, nil
	}
	// TODO(setup-p2): only EEXIST is mapped to the expected "already
	// existed" outcome; EINVAL/ENOSYS/EOPNOTSUPP (RENAME_NOREPLACE
	// unsupported by the filesystem or kernel -- 9p/virtiofs, NFS, older
	// FUSE, kernels <3.15) propagate straight up as a hard init failure
	// instead. Confirmed working on overlayfs and tmpfs, so not urgent. Fix
	// shape: on those specific errnos, fall through to the Darwin
	// link()+unlink() path (init_checkout_publish_darwin.go) instead of
	// returning the error.
	if errors.Is(err, unix.EEXIST) {
		return false, nil
	}
	return false, err
}
