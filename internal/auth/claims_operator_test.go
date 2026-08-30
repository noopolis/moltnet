package auth

import "testing"

// TestClaimsOperator pins the one definition of "operator" that every
// trust-boundary gate in this codebase now shares (Claims.Operator, claims.go).
// Before it existed the same admin+write test was written out five times --
// verbatim copies in internal/rooms/federation_access.go and
// internal/transport/federation_discovery.go, and open-coded inline in
// rooms/access_policy.go's canWriteRoom, rooms/sender_identity.go and
// transport/skill_markdown.go -- so loosening any single one of them still
// passed the whole suite.
//
// The rows that matter most are the two asymmetric ones: [admin] alone and
// [write] alone must both be false. A predicate loosened from `&&` to `||`
// (the realistic divergence) turns an admin-only or a write-only credential
// into an operator, which is exactly how a `[write, pair]` peer credential
// would walk through every federation gate. The per-call-site regressions
// that catch that same mutation behaviorally are
// TestPairScopedCredentialWithWriteScopeIsStillEnforced and
// TestAdminOnlyClaimsAreNotOperatorForWrite (internal/rooms) and
// TestHTTPPairScopedCredentialWithWriteScopeIsStillFiltered
// (internal/transport).
func TestClaimsOperator(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		scopes []Scope
		want   bool
	}{
		{name: "no scopes at all", scopes: nil, want: false},
		{name: "admin only", scopes: []Scope{ScopeAdmin}, want: false},
		{name: "write only", scopes: []Scope{ScopeWrite}, want: false},
		{name: "observe only", scopes: []Scope{ScopeObserve}, want: false},
		{name: "pair only", scopes: []Scope{ScopePair}, want: false},
		{name: "write and pair", scopes: []Scope{ScopeWrite, ScopePair}, want: false},
		{name: "admin and pair", scopes: []Scope{ScopeAdmin, ScopePair}, want: false},
		{name: "observe write attach", scopes: []Scope{ScopeObserve, ScopeWrite, ScopeAttach}, want: false},
		{name: "admin and write", scopes: []Scope{ScopeAdmin, ScopeWrite}, want: true},
		{name: "minted operator token", scopes: []Scope{ScopeObserve, ScopeWrite, ScopeAdmin}, want: true},
		// The legacy four-scope shape `--bearer` init used to mint. F1: it
		// must still read as an operator, not as a peer.
		{name: "legacy four-scope operator token", scopes: []Scope{ScopeObserve, ScopeWrite, ScopeAdmin, ScopePair}, want: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			claims := NewStaticClaims(TokenConfig{ID: "token", Scopes: testCase.scopes})
			if got := claims.Operator(); got != testCase.want {
				t.Fatalf("Claims.Operator() with scopes %v = %t, want %t", testCase.scopes, got, testCase.want)
			}
		})
	}

	if (Claims{}).Operator() {
		t.Fatalf("zero Claims.Operator() = true, want false")
	}
}
