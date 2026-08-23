package main

import (
	"fmt"
	"regexp"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// canonicalRoomIDPattern is the grammar `moltnet init --room` enforces,
// deliberately narrower than protocol.ValidateRoomID's permissive
// `^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$` (pkg/protocol/validate.go), which
// still allows ':'. PLAN.md's setup-wizard spec is explicit about why: that
// permissive grammar has produced four separate defects in components that
// split or concatenate on ':', and --room is a second place (alongside the
// wizard's own network-id prompt) where a brand-new id is born rather than
// merely referenced -- exactly the moment to hold the line, without
// touching protocol.ValidateRoomID itself, which existing rooms and
// commands still rely on to accept a legacy id that already contains one.
const maxCanonicalRoomIDLength = 128

var canonicalRoomIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// validateCanonicalRoomID rejects a --room value that does not match
// canonicalRoomIDPattern or exceeds maxCanonicalRoomIDLength. Every id this
// function accepts is also accepted by protocol.ValidateRoomID (the
// permissive grammar is a strict superset: lowercase letters and digits are
// within `[A-Za-z0-9]`, and '.', '_', '-' are within
// `[A-Za-z0-9._:-]`), so callers never need to run both.
func validateCanonicalRoomID(id string) error {
	if len(id) > maxCanonicalRoomIDLength {
		return fmt.Errorf("--room %q: id must be %d characters or fewer", id, maxCanonicalRoomIDLength)
	}
	if !canonicalRoomIDPattern.MatchString(id) {
		return fmt.Errorf("--room %q: id must match ^[a-z0-9][a-z0-9._-]*$ (lowercase letters, digits, '.', '_', '-'; no ':')", id)
	}
	return nil
}

// resolveInitRoomIDs validates and deduplicates runInit's --room flag
// values (repeatable; comma-splitting and trimming already handled by
// repeatedStringFlag.Set), returning []string{starterRoomID} when none were
// given -- runInit's existing single-room default, unchanged.
//
// Passing any --room replaces "general" rather than appending to it, unless
// "general" is itself one of the given values: that is exactly this
// function's behavior with no special-casing at all, since the "replace"
// rule only ever means "the given list is the whole list" -- there is
// nothing here to merge with a default that this function simply never
// falls back to once any value is given.
func resolveInitRoomIDs(values []string) ([]string, error) {
	ids := protocol.UniqueTrimmedStrings(values)
	if len(ids) == 0 {
		if len(values) > 0 {
			// --room was passed at least once (values is non-empty), but
			// every value trimmed/deduped away to nothing -- e.g.
			// `--room ""` or `--room " , "`. Falling through to the same
			// []string{starterRoomID} default as --room never having been
			// given at all would make that indistinguishable from the
			// operator's evidently-intended-but-empty value, so this is an
			// error instead.
			return nil, fmt.Errorf("--room: no non-empty room id given (got %q)", values)
		}
		return []string{starterRoomID}, nil
	}
	for _, id := range ids {
		if err := validateCanonicalRoomID(id); err != nil {
			return nil, err
		}
	}
	return ids, nil
}
