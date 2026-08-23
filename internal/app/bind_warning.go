package app

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
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
// Round 2's P2-4: the registration-open branch also names anonymous read,
// not just write, when it applies -- auth.mode: open forces
// auth.public_read: true alongside agent_registration: open
// (mergeFileConfig, config_load.go), and plain `init`'s own starter config
// (defaultMoltnetConfig, cmd/moltnet/templates.go) writes a "general" room
// with visibility: public. Before this fix, a widened bind on that exact
// default produced a warning naming only write-scoped self-registration --
// silent about the same bind also serving that room's full history over
// REST and its live stream over SSE to any anonymous visitor, no token at
// all. The remedy is identical either way (tighten auth, or bind to
// loopback), so this does not change what an operator has to do about it;
// it only stops describing the exposure incompletely.
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

	message := fmt.Sprintf(
		"listen_addr %q is reachable from outside this machine and auth.agent_registration is \"open\": any host that can reach this address may register its own agent and receive a write-scoped token.",
		listenAddr,
	)
	if config.Auth.PublicRead {
		if roomID := firstPubliclyReadableRoomID(config.Rooms); roomID != "" {
			message += fmt.Sprintf(
				" auth.public_read is also true, so room %q (and any other public room) is anonymously readable too, with no token at all -- both its REST message history and its live SSE stream.",
				roomID,
			)
		}
	}
	// TODO(setup-p2): this advice is inert whenever it actually fires.
	// mergeFileConfig (config_load.go) forcibly resets
	// auth.agent_registration to "open" any time auth.mode is "open" — the
	// only mode this branch ever warns about (the AgentRegistration != Open
	// check above returns "" otherwise) — so an operator who follows this
	// sentence exactly and sets agent_registration to "token" or "disabled"
	// sees no change at all at next load. The real remedies are bind to
	// loopback, or move off auth.mode: open entirely (which requires
	// configuring credentials and room membership together, since the open
	// starter room's write_policy: registered_agents shape depends on the
	// open posture too — see cmd/moltnet's init --listen authoring-time
	// warning, which says this correctly and does not repeat the sentence
	// below verbatim). Fixing the sentence itself is out of scope here: it
	// is also what `moltnet validate` and server-start print today, and
	// changing shared wording out from under those callers is a separate,
	// deliberate change, not a side effect of this one.
	message += InertAgentRegistrationAdviceSuffix
	return message
}

// InertAgentRegistrationAdviceSuffix is the exact trailing sentence
// NonLoopbackAnonymousWriteWarning appends on the agent_registration: open
// branch, above. Exported as the one shared symbol rather than left as an
// inline literal so cmd/moltnet's initNonLoopbackWarning (init_listen.go) --
// the one caller that needs to strip this exact sentence back off and
// substitute its own accurate authoring-time advice -- references this
// constant directly instead of keeping a second, hand-duplicated copy that
// could silently drift out of sync with this one (round 3's fix for a
// mutation-proven gap: two independent literals could reorder differently
// and TrimSuffix would just stop matching, leaving both the stock and the
// corrected advice printed side by side). Every other caller (`moltnet
// validate`, server start) prints the warning this constant is part of
// verbatim, advice included, and must keep doing so until the underlying
// inertness itself is fixed (see this function's own TODO(setup-p2) above).
const InertAgentRegistrationAdviceSuffix = ` Set auth.agent_registration to "token" or "disabled", or bind to a loopback address, unless this is intentional.`

// ValidateListenAddr validates addr as a syntactically well-formed
// server.listen_addr value: a "host:port" pair in net.SplitHostPort's
// grammar (an empty host is fine -- ":8787" binds every interface, exactly
// matching how net/http.Server.ListenAndServe treats it -- but a missing or
// non-numeric port is not). It does not attempt to bind addr or resolve its
// host, and deliberately does not validate the host's own character content
// beyond what SplitHostPort itself requires (bracket balancing for an IPv6
// literal): a stricter DNS-hostname grammar would risk rejecting a real,
// already-working listen_addr this validator has never seen before --
// non-RFC-1123 hostnames (an underscore-containing Docker/Compose service
// name, for instance) are common enough in practice that guessing at that
// grammar here is more likely to break a live config than to catch a real
// typo. It only rejects values that could never be a valid listen_addr
// syntactically, so an operator sees the mistake immediately rather than
// only once the server tries and fails to bind (or, for `moltnet init
// --listen`, not at all until some later `moltnet start`).
//
// Called both from validateServer (config_file.go, on every config load --
// closing the "config_file.go validates none today" gap for listen_addr)
// and directly from `moltnet init --listen` (cmd/moltnet/init.go) before
// any file is written, so the same grammar is enforced whether a bad
// address arrives via a hand-edited config file or a flag.
// TODO(setup-p2): ":http" is a legal service-name port net.Listen itself
// accepts (it resolves service names via the system's services database)
// but strconv.Atoi below rejects outright, since it only ever accepts a
// numeric port. A net.LookupPort("tcp", port) fallback when Atoi fails
// would close the gap, but that call resolves against the local system's
// services database (getservbyname on Unix) -- environment-dependent and
// not guaranteed present in a minimal container, unlike everything else
// this validator checks -- so it is left as a known, narrow gap rather
// than making config validation itself non-portable.
//
// mergeFileConfig (config_load.go) used to carry a second, related gap
// here -- this function trims addr before validating it, so
// " 127.0.0.1:8787 " (leading/trailing whitespace) passed validation, but
// assigned config.ListenAddr untrimmed, so the value that actually reached
// net.Listen at server start still carried the whitespace and failed to
// bind. Fixed: mergeFileConfig now trims server.listen_addr the same way
// mergeEnvConfig's envValue already trims MOLTNET_LISTEN_ADDR, so both
// config sources reach net.Listen identically trimmed.
func ValidateListenAddr(addr string) error {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return fmt.Errorf("must not be empty")
	}
	_, port, err := net.SplitHostPort(trimmed)
	if err != nil {
		return fmt.Errorf("%q is not a valid host:port address: %w", addr, err)
	}
	if strings.TrimSpace(port) == "" {
		return fmt.Errorf("%q is missing a port", addr)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 0 || portNumber > 65535 {
		return fmt.Errorf("%q has an invalid port %q", addr, port)
	}
	return nil
}

// firstPubliclyReadableRoomID returns the ID of the first room in rooms
// whose visibility normalizes to protocol.RoomVisibilityPublic (the exact
// test canReadRoom, internal/rooms/access_policy.go, applies to let an
// anonymous or agent-restricted actor read a room with no membership at
// all), or "" when none does.
func firstPubliclyReadableRoomID(rooms []RoomConfig) string {
	for _, room := range rooms {
		if protocol.NormalizeRoomVisibility(room.Visibility) == protocol.RoomVisibilityPublic {
			return room.ID
		}
	}
	return ""
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
