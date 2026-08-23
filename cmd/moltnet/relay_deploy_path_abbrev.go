package main

import (
	"errors"
	"strings"
)

// relay_deploy_path_abbrev.go holds relay_deploy.go's ~-abbreviation
// helpers for a stored-token path, split out purely to keep that file under
// this repo's 400-line limit.

// abbreviatePathInMessage returns err's message with every occurrence of
// rawPath rewritten to its ~-abbreviated form (abbreviateHome), for display
// only (P2-3). This is a presentation-layer transform: it never changes what
// relaydeploy itself returns to its own callers — internal/relaydeploy's own
// tests pin the raw absolute path in the errors it returns — it only cleans
// up the copy this CLI prints or returns as its own top-level error, so a
// path shown here reads consistently with every other ~-abbreviated path in
// this output. A targeted string replace, not a general parser: err or an
// empty rawPath returns err's message (or "") unchanged, and a message that
// never mentions rawPath comes back byte-identical.
func abbreviatePathInMessage(err error, rawPath string) string {
	if err == nil {
		return ""
	}
	if rawPath == "" {
		return err.Error()
	}
	abbreviated := abbreviateHome(rawPath)
	if abbreviated == rawPath {
		return err.Error()
	}
	return strings.ReplaceAll(err.Error(), rawPath, abbreviated)
}

// abbreviatePathError is abbreviatePathInMessage's error-returning
// counterpart, for a returned (rather than printed) error: the rejected-
// stored-token error wrapStoredTokenError (internal/relaydeploy/deploy.go)
// produces, which this CLI returns rather than printing directly, so
// whatever eventually prints it (main.go's top-level error path) shows the
// abbreviated path too. Returns err itself, unchanged, whenever rawPath is
// empty (no stored token was in play) or never appears in err's message —
// which also means every unrelated error (including
// relaydeploy.ErrWorkersDevSubdomainUnclaimed, handled just above every call
// site of this function) passes through with its errors.Is/errors.As chain
// intact; only the one shape that actually names rawPath gets rebuilt as a
// plain error carrying the rewritten text.
func abbreviatePathError(err error, rawPath string) error {
	if err == nil || rawPath == "" {
		return err
	}
	rewritten := abbreviatePathInMessage(err, rawPath)
	if rewritten == err.Error() {
		return err
	}
	return errors.New(rewritten)
}
