package main

import (
	"fmt"
	"strings"

	"github.com/noopolis/moltnet/internal/app"
	authn "github.com/noopolis/moltnet/internal/auth"
)

// ignoredExistingConfigFlagsNote returns a non-empty "note:" body when
// serverPath already exists and --listen and/or --room was explicitly
// passed on this invocation. Both flags silently no-op once
// writeFileIfMissing (init_checkout.go) finds serverPath already there --
// runInit never rendered a fresh config to apply them to in the first
// place -- and before this fix nothing at all told the operator so: exit 0,
// no note, no warning, config unchanged. A wizard's own port preflight, run
// specifically to avoid colliding with another network already bound to a
// chosen port, would otherwise believe `init --listen <port>` had taken
// effect against an existing config when it never touched it at all.
//
// listenFlagValue is the raw --listen flag string, not
// resolveInitListenAddr's already-defaulted return value: an empty flag
// (the overwhelmingly common case, meaning "no opinion, keep whatever this
// network already has") must never itself count as "explicitly passed."
// roomFlagValues is the raw --room repeatedStringFlag for the same reason --
// resolveInitRoomIDs's own resolved list always defaults to
// []string{starterRoomID} when empty, which would otherwise look
// indistinguishable from an explicit `--room general`.
func ignoredExistingConfigFlagsNote(serverPath string, listenFlagValue string, roomFlagValues []string) string {
	var ignored []string
	if strings.TrimSpace(listenFlagValue) != "" {
		ignored = append(ignored, "--listen")
	}
	if len(roomFlagValues) > 0 {
		ignored = append(ignored, "--room")
	}
	if len(ignored) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"%s ignored: %s already exists and was left untouched; edit server.listen_addr there by hand, or use `moltnet room create` to add a room",
		strings.Join(ignored, " and "), abbreviateHome(serverPath),
	)
}

// existingServerListenAddr returns the server.listen_addr an already-existing
// Moltnet config at serverPath resolves to -- through the exact same full
// load path the running server itself uses, including a MOLTNET_LISTEN_ADDR
// environment override (app.LoadConfigForPath) -- for runInit's MoltnetNode
// base_url computation on the resume path.
//
// Before this fix, a rerun of `moltnet init` against a config that already
// existed still derived nodeBaseURL from --listen's resolved *flag* value
// (defaultInitListenAddr when --listen was not given), so a resumed init
// whose own MoltnetNode scaffold had been deleted regenerated one pointed at
// 127.0.0.1:8787 regardless of what listen_addr the existing config actually
// carried -- silently pointing the freshly rewritten scaffold at whatever
// else happened to own that port. This is exactly the defect the
// port-threading work (consoleBaseURL, console.go) exists to kill, left
// unfixed on the resume path.
//
// fallback is returned, with no error, whenever serverPath cannot be loaded
// (a symlinked or otherwise unreadable config -- e.g. runInit's
// serverIsSymlink dangling case). This function must never turn a
// pre-existing config problem into a new hard failure of `moltnet init` on
// the resume path: applyExistingServerConfigTokens (init_server_config.go)
// now runs before runInit ever writes MoltnetNode (round 3's fix -- see its
// own call-site comment in init.go), so it is genuinely the call that
// surfaces the more specific error or skip note for a genuinely broken
// existing config before any scaffold using this function's silent
// fallback ever reaches disk, and this one derived field is not worth
// failing the whole run over on its own.
//
// TODO(setup-p2): this also resolves through app.LoadConfigForPath, which
// applies a MOLTNET_LISTEN_ADDR environment override -- but the *fresh*
// path (serverExists == false, runInit uses --listen's resolved flag value
// directly instead) never consults that env var at all, so the two paths
// can disagree. Live-verified: with MOLTNET_LISTEN_ADDR=127.0.0.1:9999 set,
// a fresh `init --listen 127.0.0.1:9911` writes both listen_addr and
// base_url as 9911 (env ignored on the fresh path); deleting MoltnetNode and
// rerunning against that same config then writes base_url as 9999 (env
// applied on the resume path, via this function). Worse,
// internal/service/{launchd,systemd}.go emit no environment block for the
// installed service, so a service-run server never sees this env var at
// all -- the regenerated scaffold can point at a port the actual server
// process never binds. Fix shape: read serverPath's listen_addr directly
// without mergeEnvConfig's override, or apply the same env resolution on
// both the fresh and resume paths so they cannot disagree.
func existingServerListenAddr(serverPath string, fallback string) string {
	cfg, err := app.LoadConfigForPath(serverPath, "")
	if err != nil || strings.TrimSpace(cfg.ListenAddr) == "" {
		return fallback
	}
	return cfg.ListenAddr
}

// resolveInitBearerListenRoomFlags validates and resolves runInit's
// --bearer/--listen/--room flags together, before anything else about the
// invocation is touched (see runInit's own call site comment for why this
// runs ahead of the banner).
//
// --bearer --room is rejected outright in v1 (PLAN.md's setup-wizard spec,
// "The init gap"): --bearer creates no rooms today (bearerMoltnetConfig,
// templates.go, rooms: []), and silently reusing the open starter room's
// write_policy: registered_agents shape would produce a room no
// bearer-enrolled static token can write to
// (internal/rooms/access_policy.go's registeredAgentActorCanWrite requires
// an agent-scoped runtime token, which bearer never mints) -- exactly the
// trap a bearer operator passing --room would otherwise walk into with no
// error at all.
func resolveInitBearerListenRoomFlags(bearer bool, listenFlagValue string, roomFlagValues []string) (string, []string, error) {
	if bearer && len(roomFlagValues) > 0 {
		return "", nil, fmt.Errorf("init: --bearer and --room cannot be combined in v1 (--bearer creates no rooms, and the open starter room's write_policy: registered_agents shape is unwritable by a bearer-enrolled static token)")
	}
	listenAddr, err := resolveInitListenAddr(listenFlagValue)
	if err != nil {
		return "", nil, err
	}
	roomIDs, err := resolveInitRoomIDs(roomFlagValues)
	if err != nil {
		return "", nil, err
	}
	return listenAddr, roomIDs, nil
}

// resolveInitListenAddr resolves `moltnet init --listen`'s flag value into
// the server.listen_addr runInit should embed in a fresh config: an empty
// flag (the common case) falls back to defaultInitListenAddr
// (templates.go's "127.0.0.1:8787", loopback-only, unchanged from before
// this flag existed), and any non-empty value is syntax-validated via
// app.ValidateListenAddr before ever reaching a template -- PLAN.md's
// setup-wizard spec calls this out explicitly as a gap: "config_file.go
// validates none today," so a malformed --listen used to ride straight
// into the written config with no complaint until some later `moltnet
// start` failed to bind.
func resolveInitListenAddr(flagValue string) (string, error) {
	trimmed := strings.TrimSpace(flagValue)
	if trimmed == "" {
		return defaultInitListenAddr, nil
	}
	if err := app.ValidateListenAddr(trimmed); err != nil {
		return "", fmt.Errorf("--listen: %w", err)
	}
	return trimmed, nil
}

// initNonLoopbackWarning returns a non-empty warning when listenAddr is not
// loopback-only, for runInit to print at authoring time -- before this,
// the only place a non-loopback bind was ever flagged was server start and
// `moltnet validate` (bind_warning.go), neither of which a scripter driving
// `init --listen` non-interactively ever sees (and a launchd log
// effectively hides the former).
//
// bearer always returns "" here: `--bearer --room` is rejected outright
// (runInit), and NonLoopbackAnonymousWriteWarning itself only ever fires
// for auth.mode: none or agent_registration: open, neither of which a
// bearer config carries (bearerMoltnetConfig, templates.go) -- so a bearer
// network's own non-loopback exposure is a different, already-documented
// story (plain HTTP, a static token now crossing the network — see
// PLAN.md's setup-wizard "Q4 — the warning is computed, never static") that
// this specific reused check was never built to describe, on either the
// authoring or the server-start path. Nothing about that changes here.
//
// For the plain (non-bearer) path, this builds the exact app.Config plain
// init is about to author -- auth.mode: open forces agent_registration:
// open and public_read: true (config_load.go's mergeFileConfig; replicated
// by hand here since this runs before any config is ever loaded) — and
// reuses app.NonLoopbackAnonymousWriteWarning rather than writing new
// wording, per PLAN.md's explicit instruction. That function's own closing
// sentence ("Set auth.agent_registration to \"token\" or \"disabled\"") is
// inert for exactly this posture (see its TODO(setup-p2) in
// bind_warning.go): mergeFileConfig resets agent_registration to "open"
// unconditionally whenever mode is "open", so following that advice
// changes nothing. This function strips that known-inert suffix and
// substitutes the accurate authoring-time advice instead, without touching
// the shared warning's own wording -- `moltnet validate` and server start
// still print it unedited, and fixing it there is out of scope for this
// change (see the TODO).
func initNonLoopbackWarning(listenAddr string, bearer bool, roomIDs []string) string {
	if bearer {
		return ""
	}

	cfg := app.Config{
		ListenAddr: listenAddr,
		Auth: authn.Config{
			Mode:              authn.ModeOpen,
			AgentRegistration: authn.AgentRegistrationOpen,
			PublicRead:        true,
		},
		Rooms: starterRoomConfigsForWarning(roomIDs),
	}

	warning := app.NonLoopbackAnonymousWriteWarning(cfg)
	if warning == "" {
		return ""
	}

	// Strips app.InertAgentRegistrationAdviceSuffix -- the exact trailing
	// sentence NonLoopbackAnonymousWriteWarning appends on the
	// agent_registration: open branch (bind_warning.go), referenced here as
	// the one shared symbol rather than a second, literal copy of the
	// sentence: round 3's fix for a mutation-proven gap where a duplicated
	// literal here and in bind_warning.go could drift out of sync (e.g.
	// reordering `"token" or "disabled"` -> `"disabled" or "token"` in one
	// copy but not the other) and TrimSuffix would silently stop matching,
	// leaving the stock inert advice sitting alongside this function's own
	// corrected sentence in the printed warning -- duplicated, contradictory
	// advice that the old two-literals design let ship with a fully green
	// suite. A single shared constant makes that divergence impossible by
	// construction, not just something a test has to keep catching.
	warning = strings.TrimSuffix(warning, app.InertAgentRegistrationAdviceSuffix)
	warning += " Bind to a loopback address, or change auth.mode and configure credentials and room membership together."
	return warning
}

// starterRoomConfigsForWarning renders roomIDs as the app.RoomConfig list
// initNonLoopbackWarning's synthetic app.Config carries, so
// firstPubliclyReadableRoomID (bind_warning.go, unexported to this package)
// can name a room in the warning exactly the way it does at server-start
// time. It applies the same visibility: public shape renderStarterRooms
// (templates.go) writes into the config, for the same reason: they need to
// describe the same rooms plain init is about to author.
func starterRoomConfigsForWarning(roomIDs []string) []app.RoomConfig {
	if len(roomIDs) == 0 {
		roomIDs = []string{starterRoomID}
	}
	rooms := make([]app.RoomConfig, 0, len(roomIDs))
	for _, id := range roomIDs {
		rooms = append(rooms, app.RoomConfig{ID: id, Visibility: "public"})
	}
	return rooms
}
