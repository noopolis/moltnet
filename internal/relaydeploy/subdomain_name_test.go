package relaydeploy

import (
	"strings"
	"testing"
)

func TestValidateWorkersDevSubdomainNameAccepts(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"apresmoi",
		"a",
		"acme-friends",
		"a-b-c-1-2-3",
		strings.Repeat("a", 63),
	} {
		if err := ValidateWorkersDevSubdomainName(name); err != nil {
			t.Fatalf("ValidateWorkersDevSubdomainName(%q) error = %v, want nil", name, err)
		}
	}
}

func TestValidateWorkersDevSubdomainNameRejects(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"too long", strings.Repeat("a", 64)},
		{"leading hyphen", "-acme"},
		{"trailing hyphen", "acme-"},
		{"underscore", "acme_friends"},
		{"dot", "acme.friends"},
		{"space", "acme friends"},
		// Uppercase is no longer accepted directly by Validate — a caller
		// must normalize first (see TestNormalizeWorkersDevSubdomainName
		// below); Validate itself now enforces wrangler's lowercase-only
		// de-facto rule.
		{"uppercase", "ACME"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateWorkersDevSubdomainName(tc.value); err == nil {
				t.Fatalf("ValidateWorkersDevSubdomainName(%q) error = nil, want a validation error", tc.value)
			}
		})
	}
}

// TestNormalizeWorkersDevSubdomainName pins the P2 lowercase-normalization
// fix: a user-typed uppercase (or mixed-case) name is not rejected outright
// — it is lowercased first, matching wrangler's own behavior, and the
// lowercased result is what actually gets validated and sent to
// Client.ClaimWorkersDevSubdomain (cmd/moltnet's interactive claim prompt
// does both, in that order).
func TestNormalizeWorkersDevSubdomainName(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"ACME":         "acme",
		"AcMe-Friends": "acme-friends",
		"apresmoi":     "apresmoi",
	}
	for input, want := range cases {
		got := NormalizeWorkersDevSubdomainName(input)
		if got != want {
			t.Fatalf("NormalizeWorkersDevSubdomainName(%q) = %q, want %q", input, got, want)
		}
		if err := ValidateWorkersDevSubdomainName(got); err != nil {
			t.Fatalf("ValidateWorkersDevSubdomainName(NormalizeWorkersDevSubdomainName(%q)) = %v, want nil", input, err)
		}
	}
}
