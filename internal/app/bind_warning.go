package app

import (
	"fmt"
	"net"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
)

// NonLoopbackAnonymousWriteWarning returns a non-empty warning when config
// resolves to a listen address reachable from outside this machine while its
// effective auth posture leaves write/admin routes reachable anonymously
// (PLAN.md phase 6a, item 2, broadened by the phase 6a review's P2-3): that
// covers both agent self-registration being open ("any reachable host may
// register an agent") and auth.mode being "none" (every write and admin
// route is anonymous outright, with no registration step needed at all —
// the pre-phase-6a default, and strictly worse than open registration). It
// returns "" when the bind is loopback-only.
//
// The original version of this check keyed only on agent_registration, so a
// non-loopback bind with the old `auth.mode: none` default — every write
// and admin route anonymous — warned about nothing, while a safer bearer
// network with open registration correctly warned. That inversion is the
// bug this broadened check fixes: mode: none is checked first, and on its
// own, before agent_registration is even considered.
//
// config.Auth.AgentRegistration is read post-merge (config_load.go's
// mergeFileConfig already resolves it, including auth.mode: open's implicit
// override), so the registration-open half fires identically however
// registration ended up open: an explicit auth.agent_registration: open, or
// auth.mode: open.
//
// Callers: internal/app.New logs this at server start (app.go), and
// `moltnet validate` (cmd/moltnet/validate.go) prints it so an operator sees
// the exposure before ever starting the server.
func NonLoopbackAnonymousWriteWarning(config Config) string {
	if IsLoopbackListenAddr(config.ListenAddr) {
		return ""
	}

	listenAddr := strings.TrimSpace(config.ListenAddr)
	if config.Auth.Mode == authn.ModeNone {
		return fmt.Sprintf(
			"listen_addr %q is reachable from outside this machine and auth.mode is \"none\": every write and admin route is anonymous, with no token or registration step required at all. Set auth.mode to \"bearer\" or \"open\", or bind to a loopback address, unless this is intentional.",
			listenAddr,
		)
	}

	if config.Auth.AgentRegistration != authn.AgentRegistrationOpen {
		return ""
	}
	return fmt.Sprintf(
		"listen_addr %q is reachable from outside this machine and auth.agent_registration is \"open\": any host that can reach this address may register its own agent and receive a write-scoped token. Set auth.agent_registration to \"token\" or \"disabled\", or bind to a loopback address, unless this is intentional.",
		listenAddr,
	)
}

// IsLoopbackListenAddr reports whether addr (a server.listen_addr value,
// e.g. "127.0.0.1:8787", ":8787", "0.0.0.0:8787", "[::1]:8787") only ever
// accepts direct TCP connections originating on this machine. An address
// with no host component (":8787") or an unparseable host is treated as
// non-loopback — the same "binds every interface" behavior Go's net/http
// gives an empty host, and the conservative default when this can't be
// determined for certain: this function's only job is deciding whether to
// warn, so an ambiguous case should warn rather than stay silent.
//
// A true result here is a narrower claim than "unreachable from off this
// machine": a loopback bind with a reverse proxy running on the same box
// (the topology guides/public-open-networks.md and
// guides/securing-remote-agents.md both recommend) is fully internet
// reachable through that proxy — every request the app process sees still
// carries a loopback RemoteAddr, on the proxy's behalf. This function has no
// request to inspect at server-start / `moltnet validate` time, so it can
// only ever reason about the bind; a live per-request decision (see
// internal/transport/discovery.go's requestIsDirectLoopback, used for the
// GET /install.md anonymous carve-out) has to check the request itself
// instead, precisely because this bind-only signal is not sufficient there.
func IsLoopbackListenAddr(addr string) bool {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return false
	}

	host, _, err := net.SplitHostPort(trimmed)
	if err != nil {
		// No port, or otherwise unparseable as host:port — try the whole
		// value as a bare host (defensive; server.listen_addr is normally
		// always host:port).
		host = trimmed
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}
