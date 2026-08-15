package relaydeploy

import (
	"context"
	"net"
	"time"
)

const dnsLookupTimeout = 3 * time.Second

// resolveHostname is Deploy's default post-deploy DNS check, used whenever
// Options.ResolveHostname is nil. Tests inject their own Options.ResolveHostname
// stub instead of touching this package-level default, so parallel tests
// never share mutable state.
var resolveHostname = lookupHostname

// lookupHostname reports whether hostname currently resolves. A freshly
// enabled workers.dev route can take a few minutes to propagate, so a false
// result means "not yet", not "deploy failed".
func lookupHostname(ctx context.Context, hostname string) bool {
	lookupCtx, cancel := context.WithTimeout(ctx, dnsLookupTimeout)
	defer cancel()

	addrs, err := (&net.Resolver{}).LookupHost(lookupCtx, hostname)
	return err == nil && len(addrs) > 0
}
