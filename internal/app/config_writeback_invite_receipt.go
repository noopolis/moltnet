package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// InviteReceipt is the plaintext record `pair invite` (cmd/moltnet/pair_invite.go)
// persists next to the Moltnet config so the exact code it is about to print
// survives an interruption between WritePairing's commit and the
// fmt.Fprintln that shows the code to the operator. Before this file
// existed, the code and its expiry were never written anywhere
// (SETUP.md's "no journal" claim, exception #1): the pairing landed in
// config, but the one secret the peer actually needs to consume it was gone
// the instant the process died before printing.
//
// Fields are plain strings/time.Time, not protocol.SecretString, for the
// same reason PairingWriteback (config_writeback.go) uses plain strings: the
// receipt's only purpose is to hand the exact code back on request, and a
// redacted round-trip would defeat that.
type InviteReceipt struct {
	PairingID string    `json:"pairing_id"`
	Code      string    `json:"code"`
	Exp       time.Time `json:"exp"`
}

// Expired reports whether receipt's invite has already passed its expiry as
// of now. A zero Exp (should not happen for a receipt this package wrote,
// but defensive against a hand-edited or pre-validation file) is never
// treated as expired -- Validate on the decoded invite is still the
// authority that actually enforces expiry when the code is used.
func (receipt InviteReceipt) Expired(now time.Time) bool {
	return !receipt.Exp.IsZero() && now.After(receipt.Exp)
}

// InviteReceiptPath returns the path WriteInviteReceipt/LoadInviteReceipt/
// RemoveInviteReceipt use for pairingID's receipt, alongside the Moltnet
// config's other per-network runtime state (the .moltnet/ convention
// relaydeploy.CredentialsPath and register-agent's identity.json already
// use). It is exported for callers (tests, `pair invite show`) that want the
// absolute path without performing an operation; the three functions above
// derive the identical relative path themselves via inviteReceiptRelPath so
// they can resolve it through an os.Root (see atomicWriteSecretFileInRoot's
// doc comment) instead of the plain absolute path this function returns.
//
// The filename is a SHA-256 of pairingID -- pairingID is hashed raw, not
// trimmed. Pairing ids are operator- or invite-supplied strings with no
// filename-safety grammar of their own -- WritePairing accepts any string as
// pairing.ID, and even *network* ids only recently gained a restrictive
// grammar (SETUP.md's Q2). Deriving a path straight from an unvalidated id
// would let a crafted --id (or an invite's pairing_id field, which `pair
// <code>` also feeds into a local pairing.ID with no round trip to confirm
// it) escape .moltnet/invites/ via "..", "/", or similar. Hashing sidesteps
// that without this package inventing a new pairing-id validation policy it
// does not own.
//
// Trimming used to happen here, before hashing: that meant " friend " and
// "friend" hashed to the same file, so writing one clobbered the other's
// receipt and removing one deleted the other's -- reachable from the network
// side, since `pair <code>` writes an invite's pairing_id into pairings[]
// verbatim, with no trim of its own (unlike `pair invite --id`, which does
// trim before this function ever sees the id). Any forgiveness for an
// operator's own typo'd whitespace belongs at the CLI boundary that reads
// --id/positional pairing-id arguments (see runPairInviteShow/runPairRevoke,
// cmd/moltnet/pair_invite.go and pair_revoke.go), not inside the hash that
// two different callers' ids must not collide through.
func InviteReceiptPath(configPath, pairingID string) string {
	return filepath.Join(filepath.Dir(configPath), inviteReceiptRelPath(pairingID))
}

// inviteReceiptRelPath is InviteReceiptPath's path, relative to
// filepath.Dir(configPath) instead of joined onto it -- the form
// WriteInviteReceipt/LoadInviteReceipt/RemoveInviteReceipt actually resolve
// through an os.Root rooted at that directory.
func inviteReceiptRelPath(pairingID string) string {
	sum := sha256.Sum256([]byte(pairingID))
	name := hex.EncodeToString(sum[:]) + ".json"
	return filepath.Join(".moltnet", "invites", name)
}

// WriteInviteReceipt persists receipt at
// InviteReceiptPath(configPath, receipt.PairingID), atomically (temp file +
// rename in the same directory), mode 0600, refusing a symlinked target --
// the same discipline WritePairing/atomicWriteConfigFile apply to the config
// file itself, since an invite code is exactly as sensitive as the pairing
// token it accompanies (protocol.Invite's doc comment: "the invite's entire
// purpose is to carry plaintext secrets").
func WriteInviteReceipt(configPath string, receipt InviteReceipt) error {
	root, err := os.OpenRoot(filepath.Dir(configPath))
	if err != nil {
		return fmt.Errorf("open %q: %w", filepath.Dir(configPath), err)
	}
	defer func() { _ = root.Close() }()

	relPath := inviteReceiptRelPath(receipt.PairingID)

	contents, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return fmt.Errorf("encode invite receipt: %w", err)
	}
	contents = append(contents, '\n')

	if err := atomicWriteSecretFileInRoot(root, relPath, contents); err != nil {
		return fmt.Errorf("write invite receipt %q: %w", InviteReceiptPath(configPath, receipt.PairingID), err)
	}
	return nil
}

// LoadInviteReceipt reads back the receipt WriteInviteReceipt stored for
// pairingID against the Moltnet config at configPath. ok is false with a nil
// error when nothing was ever written for this id -- a pairing generated
// with --print-only (which never writes a pairing at all), one generated
// before this feature existed, or one RemoveInviteReceipt already cleared
// (`pair revoke`).
//
// A decoded receipt with an empty Code is treated as corrupt and returned
// as an error, not as ok=true: a `null` or `{}` body used to decode to a
// zero-value InviteReceipt with ok=true, so `pair invite show` returned
// err=nil and printed an empty line -- a wizard scraping stdout got "" back
// as "the code," indistinguishable from success. A zero Exp is left alone
// (still never treated as expired -- Expired's own doc comment): that shape
// is the documented, deliberate fallback for a hand-edited or
// pre-validation file, not evidence of corruption on its own.
func LoadInviteReceipt(configPath, pairingID string) (receipt InviteReceipt, ok bool, err error) {
	root, err := os.OpenRoot(filepath.Dir(configPath))
	if err != nil {
		return InviteReceipt{}, false, fmt.Errorf("open %q: %w", filepath.Dir(configPath), err)
	}
	defer func() { _ = root.Close() }()

	relPath := inviteReceiptRelPath(pairingID)

	// root.Open + io.ReadAll stands in for Root.ReadFile, which Go only added
	// in 1.25 -- this package still targets 1.24 (see go.mod). Root.Open
	// keeps the same containment guarantee ReadFile would have: both resolve
	// relPath through root, refusing a symlinked component that points
	// outside it.
	file, err := root.Open(relPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return InviteReceipt{}, false, nil
		}
		return InviteReceipt{}, false, fmt.Errorf("read invite receipt %q: %w", InviteReceiptPath(configPath, pairingID), err)
	}
	contents, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil {
		return InviteReceipt{}, false, fmt.Errorf("read invite receipt %q: %w", InviteReceiptPath(configPath, pairingID), err)
	}
	if err := json.Unmarshal(contents, &receipt); err != nil {
		return InviteReceipt{}, false, fmt.Errorf("decode invite receipt %q: %w", InviteReceiptPath(configPath, pairingID), err)
	}
	if receipt.Code == "" {
		return InviteReceipt{}, false, fmt.Errorf("invite receipt %q is corrupt: missing code", InviteReceiptPath(configPath, pairingID))
	}
	return receipt, true, nil
}

// RemoveInviteReceipt deletes pairingID's receipt file, if any. It is
// RevokePairing's (config_writeback_revoke.go) companion for the receipt the
// way removeAuthToken is for the auth.tokens[] entry: a revoked pairing must
// not leave a live-looking invite code recoverable on disk. Removing a
// receipt that never existed is a silent no-op, matching
// removeAuthToken/stripPairingFromRoomFederations's own "already-legitimately
// -absent state" handling.
func RemoveInviteReceipt(configPath, pairingID string) error {
	root, err := os.OpenRoot(filepath.Dir(configPath))
	if err != nil {
		return fmt.Errorf("open %q: %w", filepath.Dir(configPath), err)
	}
	defer func() { _ = root.Close() }()

	relPath := inviteReceiptRelPath(pairingID)

	if err := root.Remove(relPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("remove invite receipt %q: %w", InviteReceiptPath(configPath, pairingID), err)
	}
	return nil
}

// rejectSymlinkedPathIfExists is rejectSymlinkedConfigPath's (config_writeback.go)
// counterpart for a file that may not exist yet: a missing path is not a
// symlink and is fine to create, whereas rejectSymlinkedConfigPath requires
// its target to already exist (every config it guards was always created by
// `init` first, before any writeback ever runs against it).
//
// This checks only relPath's own leaf -- root's own escape check (see
// atomicWriteSecretFileInRoot) is what keeps a symlinked ancestor directory
// (".moltnet", ".moltnet/invites") from being silently traversed to a
// location outside root; the two are independent because a symlinked leaf
// pointing somewhere else *inside* root (e.g. at another pairing's receipt,
// or at the Moltnet config file itself) would satisfy root's escape check
// yet still let a write clobber a file it was never meant to touch.
func rejectSymlinkedPathIfExists(root *os.Root, relPath string) error {
	info, err := root.Lstat(relPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat %q: %w", relPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%q must not be a symlink", relPath)
	}
	return nil
}

// atomicWriteSecretFileInRoot writes contents to relPath inside root
// atomically (temp file in the same directory, then rename), mode 0600,
// creating parent directories (mode 0700) as needed. It mirrors
// atomicWriteConfigFile's (config_writeback.go) discipline for the Moltnet
// config file itself, and relaydeploy.writeSecretFileAtomic's for
// .moltnet/relay.json -- this package does not import internal/relaydeploy
// (a deploy-orchestration package, not a config-writeback one; see
// internal/relaydeploy/CLAUDE.md) for one small helper, so it keeps its own
// copy instead.
//
// Every step but the final rename goes through root (os.OpenRoot(filepath.
// Dir(configPath)) in WriteInviteReceipt) instead of plain os.* calls against
// an absolute path. The difference matters at directory creation: an
// operator's cwd Moltnet config lives in an otherwise-writable directory, and
// pre-creating ".moltnet" (or ".moltnet/invites") as a symlink to somewhere
// else used to make plain os.MkdirAll traverse it without complaint, landing
// every future invite receipt -- plaintext relay URL, room, and relay token
// included -- at the symlink's target instead. os.Root refuses to resolve a
// symlinked path component that points outside the root at all, so that
// traversal now fails closed here instead of succeeding silently. This
// package still targets Go 1.24 (see go.mod), so mkdirAllInRoot and
// chmodInRoot below stand in for Root.MkdirAll/Root.Chmod, both added only in
// 1.25 -- see their doc comments for why they preserve the same containment
// check.
//
// The rename is the one exception: Root.Rename is 1.25+ too, and 1.24's
// *os.Root exposes no rooted rename at all, so this falls back to a plain
// os.Rename against the resolved absolute path, the same pattern
// atomicWriteConfigFile uses for the Moltnet config file itself. That is a
// real, narrow regression versus a true rooted rename: the symlink re-check
// immediately below still refuses an already-symlinked ".moltnet" or
// ".moltnet/invites" -- the case TestWriteInviteReceiptRejectsSymlinked
// InvitesDirectory/Subdirectory exercise -- but the plain os.Rename
// re-resolves the path from scratch instead of reusing an already-verified
// directory descriptor, leaving a brief TOCTOU window (a racing swap of
// ".moltnet"/".moltnet/invites" for a symlink between that check and the
// rename syscall) that Root.Rename would have closed.
func atomicWriteSecretFileInRoot(root *os.Root, relPath string, contents []byte) error {
	dir := filepath.Dir(relPath)
	if err := mkdirAllInRoot(root, dir, 0o700); err != nil {
		return fmt.Errorf("create %q: %w", dir, err)
	}
	// mkdirAllInRoot no-ops on a directory that already exists, so a
	// pre-existing wider-than-0700 ".moltnet/invites" (e.g. 0777) would
	// otherwise stay that way after a successful write here -- the 0600
	// receipt file itself is unreadable by another user, but freely
	// unlinkable and replaceable by one, since a directory's own
	// write/execute bits, not its entries' own modes, are what govern
	// renaming or removing a name inside it. Chmod unconditionally rather
	// than only on the just-created path, so an already-existing directory
	// gets tightened too.
	if err := chmodInRoot(root, dir, 0o700); err != nil {
		return fmt.Errorf("secure %q: %w", dir, err)
	}

	if err := rejectSymlinkedPathIfExists(root, relPath); err != nil {
		return err
	}

	tempName, temp, err := createRootTempFile(root, dir)
	if err != nil {
		return fmt.Errorf("create temporary file in %q: %w", dir, err)
	}
	defer func() { _ = root.Remove(tempName) }()

	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("chmod temporary file %q: %w", tempName, err)
	}
	if _, err := temp.Write(contents); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temporary file %q: %w", tempName, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary file %q: %w", tempName, err)
	}

	// Re-check immediately before the unrooted rename below -- see this
	// function's doc comment for the residual race this narrows but does
	// not close.
	if err := rejectSymlinkedPathIfExists(root, relPath); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(root.Name(), tempName), filepath.Join(root.Name(), relPath)); err != nil {
		return fmt.Errorf("replace %q: %w", relPath, err)
	}
	if err := chmodInRoot(root, relPath, 0o600); err != nil {
		return fmt.Errorf("chmod %q: %w", relPath, err)
	}

	return nil
}

// mkdirAllInRoot creates every path component of dir inside root, mirroring
// os.MkdirAll -- os.Root gained an equivalent (Root.MkdirAll) only in Go
// 1.25, and this package still targets 1.24 (see go.mod), so it walks the
// path itself, one root.Mkdir per component.
//
// root.Mkdir("invites", ...) failing with fs.ErrExist doesn't by itself tell
// us "invites" is a directory safe to descend into -- it could be a symlink
// pointing outside root, and a plain "ErrExist means fine" loop would defeat
// the point of using os.Root at all (TestWriteInviteReceiptRejectsSymlinked
// InvitesSubdirectory: ".moltnet" is real, but ".moltnet/invites" is a
// symlink to outside). So on ErrExist this calls root.Stat, not a bare
// continue: Stat follows symlinks but, like every Root method, refuses one
// pointing outside root, catching a symlinked component the same way
// Root.MkdirAll would.
func mkdirAllInRoot(root *os.Root, dir string, perm os.FileMode) error {
	dir = filepath.Clean(dir)
	if dir == "." {
		return nil
	}

	var built string
	for _, part := range strings.Split(dir, string(filepath.Separator)) {
		if built == "" {
			built = part
		} else {
			built = filepath.Join(built, part)
		}
		if err := root.Mkdir(built, perm); err != nil {
			if !errors.Is(err, fs.ErrExist) {
				return err
			}
			info, statErr := root.Stat(built)
			if statErr != nil {
				return statErr
			}
			if !info.IsDir() {
				return fmt.Errorf("%q already exists and is not a directory", built)
			}
		}
	}
	return nil
}

// chmodInRoot sets relPath's mode, standing in for Root.Chmod (Go 1.25+;
// this package still targets 1.24, see go.mod). It resolves relPath through
// root.Open -- the same containment check Root.Chmod would apply -- then
// calls Chmod on the resulting *os.File, which operates on the already-open
// file descriptor (fchmod) rather than re-resolving the path, so it carries
// no extra TOCTOU exposure of its own.
func chmodInRoot(root *os.Root, relPath string, mode os.FileMode) error {
	file, err := root.Open(relPath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	return file.Chmod(mode)
}

// createRootTempFile creates a new, exclusively-owned temporary file inside
// dir (a directory relative to root), mirroring os.CreateTemp's
// random-name-plus-O_EXCL-retry behavior. os.CreateTemp itself only accepts
// a plain directory path, not an os.Root, so keeping the write path fully
// rooted (see atomicWriteSecretFileInRoot's doc comment for why that
// matters) means growing this small equivalent rather than dropping back to
// an unrooted call for just this one step.
func createRootTempFile(root *os.Root, dir string) (name string, file *os.File, err error) {
	for attempt := 0; attempt < 10000; attempt++ {
		var suffix [12]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return "", nil, fmt.Errorf("generate temp file name: %w", err)
		}
		candidate := filepath.Join(dir, ".moltnet-secret-"+hex.EncodeToString(suffix[:]))
		file, err := root.OpenFile(candidate, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return candidate, file, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", nil, err
		}
	}
	return "", nil, fmt.Errorf("could not create unique temporary file in %q after repeated collisions", dir)
}
