package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// gitignoreCredentialEntries are the exact lines offerGitignoreCredentialEntries
// offers to add to a git checkout's .gitignore (PLAN.md phase 7.0's
// "Corollary work, same change"): Moltnet holds this network's admin
// credential in plaintext, MoltnetNode is written alongside it, .moltnet/ is
// the sqlite storage directory defaultMoltnetConfig's own
// storage.sqlite.path defaults into, and ".moltnet-init-*.tmp" is
// writeFileIfMissing's (below) own same-directory staging pattern -- every
// one of these lands at repo root under `init --dir <repo>` and none of them
// belong in version control.
//
// The temp-file glob closes a real leak, reproduced live: an init
// interrupted between os.CreateTemp and the atomic publish (SIGKILL, power
// loss -- exactly the class writeFileIfMissing's own atomic-rewrite exists
// to survive) leaves a ".moltnet-init-*.tmp" file sitting at repo root
// holding the same freshly minted [observe, write, admin] operator token
// serverPath itself was about to get. Before this entry existed, that
// leftover was invisible to gitignoreCredentialEntries' other three lines
// and got swept straight into a `git add -A` alongside the .gitignore this
// very init run had just written -- a plaintext admin credential staged for
// commit. See writeFileIfMissing's own pre-check below for the other half of
// this fix: on the resume path (serverPath already exists), serverContents
// is left empty by runInit (init.go) whenever serverExists is true, so the
// temp file the pre-check now skips would have held "", never a token --
// the pre-check's value there is purely avoiding a pointless create/write/
// remove on a no-op run, not closing a leak. The pre-check does close one
// genuine token-bearing case on this same no-op path, though: serverPath
// present as a symlink or directory reads as serverExists == false (only a
// plain regular file counts), so serverContents is still built with a fresh
// token even though writeFileIfMissing's Lstat pre-check is about to refuse
// the write outright -- exactly the case this glob entry protects. On the
// genuinely fresh path (serverPath does not exist in any form), the
// token-bearing temp file is created exactly as before this fix; this
// ".moltnet-init-*.tmp" gitignore entry is that path's only defense against
// an interrupted init leaving it sitting at repo root.
var gitignoreCredentialEntries = []string{"Moltnet", "MoltnetNode", ".moltnet/", ".moltnet-init-*.tmp"}

// checkoutWarning returns a non-empty warning when root itself contains any
// of checkoutMarkers, or when root has no marker of its own but sits inside
// an ancestor directory that is a git checkout root (gitCheckoutRoot,
// below) -- either way suggesting it is a source checkout rather than a
// runtime install directory (PLAN.md: "warn before writing").
//
// PLAN.md phase 7.0's "Corollary work, same change": once plain `init`
// started minting a live [observe, write, admin] operator token into every
// fresh Moltnet config (defaultMoltnetConfig, templates.go), this warning's
// old "unusual for a runtime install" wording was no longer the actionable
// half of the story for the one target this warning exists for --
// `--dir <path>` into a project checkout is the single init target whose
// written config can reach a remote through an ordinary `git add -A` /
// `git commit`, since nothing else in this command's output flow ever
// prints or transmits that credential. The warning now names it directly
// rather than leaving an operator to infer "config" means "secret."
//
// P1 (round 2): the marker loop below only ever looked directly under root,
// so `init --dir repo/agents` inside a checkout whose ".git" lives only at
// `repo/` -- not `repo/agents/` -- got no warning and no .gitignore entry at
// all, strictly worse than the pre-fix root case, which at least warned
// generically. The subdirectory branch names the discovered checkout root
// explicitly (root itself carries no marker to point at), so the operator
// knows exactly which .gitignore offerGitignoreCredentialEntries is about
// to write into.
func checkoutWarning(root string) string {
	var found []string
	for _, marker := range checkoutMarkers {
		if info, err := os.Stat(filepath.Join(root, marker)); err == nil && info != nil {
			found = append(found, marker)
		}
	}
	if len(found) > 0 {
		return fmt.Sprintf(
			"  %s %s looks like a source checkout (found %s); Moltnet here will hold this network's admin credential in plaintext — did you mean a different --dir, or the default ~/.moltnet/ home?",
			yellow("warning:"), root, strings.Join(found, ", "),
		)
	}

	if gitRoot, ok := gitCheckoutRoot(root); ok {
		return fmt.Sprintf(
			"  %s %s is inside the git checkout at %s; Moltnet here will hold this network's admin credential in plaintext — did you mean a different --dir, or the default ~/.moltnet/ home?",
			yellow("warning:"), root, gitRoot,
		)
	}

	return ""
}

// gitCheckoutRoot walks upward from root (root included) looking for the
// nearest ancestor directory that is itself a git checkout root (a ".git"
// entry directly under it), so a target several levels inside a git working
// tree -- `init --dir repo/agents` when only `repo/.git` exists -- is still
// recognized as living inside version control, not just a target that
// happens to be a repo root itself (the old isGitCheckoutRoot, which this
// replaces: every prior isGitCheckoutRoot(x) call site now reads
// gitCheckoutRoot(x) with a discard on the returned root).
//
// The walk is bounded at the filesystem root and at $HOME (round 2's P1
// fix): once the climb reaches the user's home directory, that is the last
// directory checked -- it never climbs to $HOME's parent looking for a
// repo enclosing the home directory itself, and obviously never above "/".
// $HOME is still checked at that final step, so a dotfiles checkout at ~
// is correctly reported for anything nested under it; only climbing
// further up is out of scope. A root outside $HOME (e.g. under /tmp, as
// every test in this package uses via t.TempDir()) still walks all the way
// to "/".
func gitCheckoutRoot(root string) (string, bool) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	home, homeErr := os.UserHomeDir()
	if homeErr == nil {
		home = filepath.Clean(home)
	}

	dir := filepath.Clean(abs)
	for {
		if info, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil && info != nil {
			return dir, true
		}
		if home != "" && dir == home {
			return "", false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// prefixedGitignoreCredentialEntries renders gitignoreCredentialEntries for
// gitRoot/root, prefixing every line with root's path relative to gitRoot
// when root is a descendant of it (round 2's P1 fix: gitCheckoutRoot found
// ".git" only in an ancestor) rather than gitRoot itself. The prefix (e.g.
// "agents/") makes the ignore rule actually match where
// Moltnet/MoltnetNode/.moltnet/ land -- a bare "Moltnet" entry in the
// checkout's top-level .gitignore would not match a nested
// `repo/agents/Moltnet` at all, since gitignore patterns with no interior
// slash match a basename at any depth but ones with a slash are anchored,
// and these need exactly this location anchored so this rule can never
// accidentally ignore an unrelated file elsewhere in the tree named
// "Moltnet".
//
// Shared by offerGitignoreCredentialEntries (the write path, below) and
// runInit's own could-not-update-.gitignore fallback message (init.go): an
// operator told to add these lines by hand after a write failure needs to
// see the exact same (correctly prefixed) entries offerGitignoreCredentialEntries
// would have written on success, not a separate hardcoded, unprefixed list
// that silently drops the subdirectory anchor a nested --dir target needs.
func prefixedGitignoreCredentialEntries(gitRoot, root string) []string {
	prefix := ""
	if rel, err := filepath.Rel(gitRoot, root); err == nil && rel != "." && rel != "" {
		prefix = filepath.ToSlash(rel) + "/"
	}

	entries := make([]string, len(gitignoreCredentialEntries))
	for index, entry := range gitignoreCredentialEntries {
		entries[index] = prefix + entry
	}
	return entries
}

// offerGitignoreCredentialEntries appends any of
// prefixedGitignoreCredentialEntries(gitRoot, root)'s entries missing from
// gitRoot's .gitignore (creating the file if it does not exist yet) and
// returns exactly the entries it added, so the caller can report them
// truthfully -- an empty, nil-error result means every entry was already
// covered and nothing was written. Idempotent: an entry already present
// anywhere in the file (exact line match, after trimming surrounding
// whitespace) is left alone, so a rerun that finds every entry already
// there writes nothing.
func offerGitignoreCredentialEntries(gitRoot, root string) ([]string, error) {
	entries := prefixedGitignoreCredentialEntries(gitRoot, root)

	path := filepath.Join(gitRoot, ".gitignore")

	contents, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}

	existing := make(map[string]bool, len(entries))
	for _, line := range strings.Split(string(contents), "\n") {
		existing[strings.TrimSpace(line)] = true
	}

	var missing []string
	for _, entry := range entries {
		if !existing[entry] {
			missing = append(missing, entry)
		}
	}
	if len(missing) == 0 {
		return nil, nil
	}

	updated := string(contents)
	if updated != "" && !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}
	for _, entry := range missing {
		updated += entry + "\n"
	}

	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return nil, fmt.Errorf("write %q: %w", path, err)
	}
	return missing, nil
}

// writeFileIfMissing atomically creates path with contents at mode 0600 when
// it does not already exist, and no-ops (false, nil) when it does.
//
// This used to be a single O_CREATE|O_EXCL open, replacing an earlier
// separate Stat-then-Write specifically because path holds a live secret as
// of PLAN.md phase 7.0 (both defaultMoltnetConfig and bearerMoltnetConfig
// embed a freshly minted operator token): a Stat check followed by a
// separate write leaves a window where a symlink introduced at path in
// between -- trivially raceable in a world-writable or shared directory,
// and worst under `init --dir .` in one -- makes the write follow it,
// landing the operator token wherever the symlink points, silently, with
// no error. O_CREATE|O_EXCL refuses (EEXIST) the instant path exists in any
// form, symlink included, without ever following it, atomically, closing
// that race.
//
// PLAN.md's setup-wizard spec (Unit 1) requires atomicity against a second
// failure mode that O_CREATE|O_EXCL alone does not cover: an init
// interrupted (SIGINT, SIGKILL, power loss) between opening path and
// finishing the write leaves a truncated, half-written file sitting at
// path -- the exact filename every future `moltnet init`/`validate`/`start`
// discovers and tries to load. The fix is the classic "write elsewhere,
// publish atomically" shape: contents are written and fsynced to a fresh
// temp file in the same directory as path (same filesystem, so the publish
// step below is a single atomic operation, not a cross-device copy), and
// only that already-complete, already-durable temp file is ever published
// to path. Nothing ever observes path holding partial content.
//
// The publish step itself must preserve the exact no-clobber guarantee
// O_CREATE|O_EXCL gave: a plain temp-file-then-os.Rename would silently
// *replace* an existing path on Darwin (POSIX rename(2) semantics), which
// would undo the phase 7.0 fix above the moment two `init`s (or an
// operator and a concurrently-running one) raced -- the second one landing
// would clobber whatever the first had just written, symlink or real file,
// operator token and all. publishNoReplace (init_checkout_publish_linux.go
// / init_checkout_publish_darwin.go) is the per-OS no-replace publish this
// needs instead: renameat2(RENAME_NOREPLACE) on Linux, link()+unlink() on
// Darwin (which has no renameat2) -- both refuse the instant path already
// exists in any form, symlink included, without ever following it, exactly
// matching O_CREATE|O_EXCL's own refusal semantics. That is also what keeps
// TestWriteFileIfMissingRefusesSymlinkTarget and
// TestWriteFileIfMissingRefusesDanglingSymlink passing unchanged: a
// destination that is a symlink (dangling or not) is an existing directory
// entry either way, and gets refused the same way a plain existing file
// does.
//
// The whole sequence, in order: create a same-directory temp file (mode
// 0600, since os.CreateTemp uses O_EXCL itself so this step alone is
// already race-free), write contents, fsync the temp file (durability of
// the content before it is ever visible under path's name), close it,
// publish it to path with no-replace semantics, and -- only on a
// successful publish -- fsync path's directory too (durability of the
// *directory entry* pointing at the new file; without this, a crash right
// after a successful rename/link can still lose the directory entry on
// some filesystems even though the file's own contents are safely on
// disk). Any temp file not consumed by a successful publish (an error at
// any step, or publishNoReplace finding path already taken) is removed via
// the deferred cleanup below, so no stray secret-bearing temp file is ever
// left behind under normal operation.
func writeFileIfMissing(path string, contents string) (bool, error) {
	// Cheap existence pre-check, ahead of ever creating the temp file below:
	// on the no-op path (path already exists -- the common case for a
	// second `moltnet init` against a healthy network) this used to still
	// create a same-directory ".moltnet-init-*.tmp", write contents into it,
	// fsync it, and only then discover via publishNoReplace that path was
	// already taken and clean the temp file back up. For the ordinary
	// resume call this pre-check saves is pointless I/O, not a leak: runInit
	// (init.go) leaves serverContents empty whenever serverExists is true,
	// so that discarded temp file held "", never a token. The pre-check does
	// close one genuine token-bearing case on this same no-op path, though:
	// serverPath present as a symlink or directory reads as
	// serverExists == false (only a plain regular file counts there), so
	// serverContents is still built with a freshly minted operator token
	// even though this Lstat is about to refuse the write outright -- that
	// case is what this pre-check actually keeps off disk. os.Lstat (not
	// Stat) so a symlinked path -- dangling or not -- is caught here too,
	// matching the refusal semantics the rest of this function already
	// gives a symlink via publishNoReplace.
	//
	// For any genuinely fresh contents (the create path below), this is an
	// optimization for the common case, not a substitute for the no-replace
	// publish below: a path created concurrently between this check and the
	// eventual publish is still refused there, exactly as before.
	if _, err := os.Lstat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect %q: %w", path, err)
	}

	dir := filepath.Dir(path)

	tempFile, err := os.CreateTemp(dir, ".moltnet-init-*.tmp")
	if err != nil {
		return false, fmt.Errorf("create temp file for %q: %w", path, err)
	}
	tempPath := tempFile.Name()

	// consumed is flipped to true only once publishNoReplace has taken
	// ownership of tempPath (Linux: renamed away, so no second name is ever
	// left behind; Darwin: link succeeded, leaving a second, now-redundant
	// name at tempPath that init_checkout_publish_darwin.go's own
	// `_ = os.Remove(tempPath)` best-effort-cleans up -- see its doc comment
	// for why a Remove failure there is harmless: tempPath and path are then
	// the same inode with identical 0600 content, so an uncleaned leftover
	// is a redundant directory entry, not a second copy of a secret with
	// different exposure). Every other return path below -- including
	// publishNoReplace itself reporting "path already existed" -- leaves
	// tempPath in place for this defer to clean up.
	consumed := false
	defer func() {
		if !consumed {
			os.Remove(tempPath)
		}
	}()

	if _, err := tempFile.WriteString(contents); err != nil {
		tempFile.Close()
		return false, fmt.Errorf("write temp file for %q: %w", path, err)
	}
	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		return false, fmt.Errorf("sync temp file for %q: %w", path, err)
	}
	if err := tempFile.Close(); err != nil {
		return false, fmt.Errorf("close temp file for %q: %w", path, err)
	}

	published, err := publishNoReplace(tempPath, path)
	if err != nil {
		return false, fmt.Errorf("publish %q: %w", path, err)
	}
	if !published {
		// path already existed by the time publish ran -- created
		// concurrently by another process/goroutine since the caller's own
		// pre-check (if any), or simply already there. writeFileIfMissing's
		// existing "already exists -> (false, nil)" contract, preserved
		// exactly: nothing about path was touched.
		return false, nil
	}
	consumed = true

	// A syncDir failure here must not be reported as though the write never
	// happened: publishNoReplace above already succeeded, so path durably
	// holds contents regardless of whether this directory-entry fsync also
	// succeeds -- returning (false, err) used to abort runInit before it
	// ever reached printInitSummary, misreporting a write that actually
	// happened, and made this function's own "created" bookkeeping wrong on
	// the way out too. Log and continue instead: the file's own durability
	// is unaffected either way, and this is best-effort insurance against a
	// narrow crash window (a crash between the rename/link above and this
	// fsync), not a correctness requirement for the write itself.
	if err := syncDir(dir); err != nil {
		fmt.Fprintf(stdout, "  %s failed to sync directory %q after writing %q (the file itself was written successfully): %v\n", yellow("note:"), dir, path, err)
	}

	return true, nil
}

// syncDir fsyncs dir itself (not its contents), so a durable rename/link
// into dir -- publishNoReplace's job, above -- has its directory entry
// survive a crash immediately after, not just the target file's own
// contents. Opening a directory for read and calling Sync on it is the
// portable way to fsync a directory in Go (there is no os.SyncDir); both
// Linux and Darwin support fsync on a directory file descriptor.
//
// A var, not a func, so a test can force a failure here without needing a
// real filesystem that actually rejects fsync -- there is no portable way
// to make a real directory fail Sync on demand.
var syncDir = func(dir string) error {
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
