package main

import "testing"

// TestResolveInitRoomIDsRejectsBlankRoomFlag pins the fix for a real gap:
// `--room ""` and `--room " , "` both trim/dedupe down to zero ids, which
// used to be indistinguishable from --room never having been passed at all
// -- silently authoring the "general" default instead of erroring on the
// operator's evidently-intended-but-empty value. Passing --room at least
// once but ending up with no non-empty id must now be a usage error, not a
// silent fallback.
func TestResolveInitRoomIDsRejectsBlankRoomFlag(t *testing.T) {
	cases := [][]string{
		{""},
		{" , "},
		{"", "  "},
	}
	for _, values := range cases {
		if _, err := resolveInitRoomIDs(values); err == nil {
			t.Fatalf("resolveInitRoomIDs(%q) error = nil, want an error for a --room flag given but empty after trimming", values)
		}
	}
}

// TestResolveInitRoomIDsDefaultsWhenFlagNeverGiven is the control case:
// --room genuinely never passed (an empty slice, not a slice holding empty
// strings) must still default to the starter room, unaffected by the fix
// above.
func TestResolveInitRoomIDsDefaultsWhenFlagNeverGiven(t *testing.T) {
	ids, err := resolveInitRoomIDs(nil)
	if err != nil {
		t.Fatalf("resolveInitRoomIDs(nil) error = %v, want nil", err)
	}
	if len(ids) != 1 || ids[0] != starterRoomID {
		t.Fatalf("resolveInitRoomIDs(nil) = %v, want [%q]", ids, starterRoomID)
	}
}
