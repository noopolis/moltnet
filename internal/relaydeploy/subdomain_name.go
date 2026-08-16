package relaydeploy

import (
	"fmt"
	"regexp"
	"strings"
)

// maxWorkersDevSubdomainNameLength is the workers.dev subdomain length
// limit implied by wrangler's own client-side naming rule (see
// workersDevSubdomainNamePattern below): the name becomes a DNS label in
// <name>.<subdomain>.workers.dev, and DNS labels cap out at 63 characters.
const maxWorkersDevSubdomainNameLength = 63

// workersDevSubdomainNamePattern is wrangler's own de-facto workers.dev
// subdomain naming rule, enforced client-side before it ever calls the
// Cloudflare API: lowercase letters, digits, and hyphens only, 1-63
// characters, never starting or ending with a hyphen. Cloudflare itself
// publishes no official naming rules for the account-level workers.dev
// subdomain; this mirrors wrangler-core's own SUBDOMAIN_REGEXP (workers-sdk,
// packages/wrangler/src/deploy/subdomain.ts:157 at the time of writing),
// the closest thing to a documented contract. It is lowercase-only —
// callers must run a user-typed name through NormalizeWorkersDevSubdomainName
// first, matching wrangler's own lowercase-before-validate behavior.
var workersDevSubdomainNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidateWorkersDevSubdomainName reports whether name satisfies
// workersDevSubdomainNamePattern above. It exists so the interactive claim
// prompt (cmd/moltnet's `relay deploy`, on ErrWorkersDevSubdomainUnclaimed)
// can reject an obviously invalid name locally, before ever spending a
// round trip on Client.ClaimWorkersDevSubdomain. Callers must normalize a
// user-typed name with NormalizeWorkersDevSubdomainName before calling
// this: an uppercase letter is rejected here, not silently accepted.
func ValidateWorkersDevSubdomainName(name string) error {
	if name == "" {
		return fmt.Errorf("subdomain name must not be empty")
	}
	if len(name) > maxWorkersDevSubdomainNameLength {
		return fmt.Errorf("subdomain name must be %d characters or fewer, got %d", maxWorkersDevSubdomainNameLength, len(name))
	}
	if !workersDevSubdomainNamePattern.MatchString(name) {
		return fmt.Errorf("subdomain name %q must contain only lowercase letters, digits, and hyphens, and must not start or end with a hyphen", name)
	}
	return nil
}

// NormalizeWorkersDevSubdomainName lowercases name so a user-typed
// subdomain name validates and claims the same way regardless of case.
// Cloudflare's account-level workers.dev subdomain is effectively
// case-insensitive in practice (it becomes a DNS label, and DNS labels
// are case-insensitive), but workersDevSubdomainNamePattern's rule is
// lowercase-only, matching wrangler's own lowercase-before-validate
// behavior — so callers must call this before
// ValidateWorkersDevSubdomainName and before sending the name to
// Client.ClaimWorkersDevSubdomain, not after.
func NormalizeWorkersDevSubdomainName(name string) string {
	return strings.ToLower(name)
}
