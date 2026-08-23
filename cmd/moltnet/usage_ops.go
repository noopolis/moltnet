package main

func buildPairUsage() string {
	return `Usage:
  moltnet pair invite [--relay-url <wss://url> --relay-token-env <env>] [--id <pairing-id>] [--room <shared-room-id>] [--print-only] [--force] [--restart] [--config <path>] [--network-id <network-id>]
  moltnet pair invite show <pairing-id> [--config <path>] [--id <network-id>] [--network-id <network-id>]
  moltnet pair <invite-code> [--force] [--restart] [--config <path>] [--id <network-id>]
  moltnet pair revoke <pairing-id> [--restart] [--config <path>] [--id <network-id>]
  moltnet pair list [--base-url <url>] [--config <path>] [--network <network-id>] [--token <token>] [--token-env <env>] [--token-path <path>]
  moltnet pair show <pairing-id> [--base-url <url>] [--config <path>] [--network <network-id>] [--token <token>] [--token-env <env>] [--token-path <path>]

"pair invite" generates one shareable moltnet-invite:... code for one friend
network against an already-deployed relay, and writes this side's pairings[]
and auth.tokens[] entries. When --relay-url/--relay-token-env are omitted,
it reuses the relay recorded by the last successful "moltnet relay deploy"
(.moltnet/relay.json); either flag, if given, overrides the stored value.
--force overwrites an existing pairing with the same --id instead of
refusing; rerun with a new --id to generate an unrelated invite instead.

"pair invite" also writes a 0600 receipt of the code and its expiry
alongside the config (.moltnet/invites/), committed together with the
pairing, so the code survives an interruption between that commit and this
command printing it. "pair invite show <pairing-id>" reads it back: it
prints the exact code again, refuses once the invite has expired, and prints
nothing (and writes nothing) after "pair revoke" removes the receipt along
with the pairing. Its own --id selects a *network* id (unlike "pair
invite"'s --id, which names the *pairing*); --network-id is accepted as the
same spelled-out alias "pair invite" itself offers for that meaning.

"pair <invite-code>" consumes an invite generated on another network. It
never contacts the relay or Cloudflare; it only writes the mirrored
pairings[] and auth.tokens[] entries.

Only "pair invite" takes --room: each one (repeatable, or comma-separated)
names a room to share with the new pairing, and gets baked into the invite
code as its list of shared rooms. "pair <invite-code>" has no --room of its
own; it creates whichever rooms the invite names. Either way, the room is
created in rooms[] if it does not already exist, and its federation is
extended to allow the pairing: an existing "none" or absent federation
becomes a list naming just this pairing, an existing list gets this pairing
appended if missing, and an existing "all" is left alone. Re-running with
--force never duplicates the pairing's entry in that list.
For each shared room, both commands print the exact "moltnet admin room
members add" command this network should run to grant the paired network's
agent membership. "pair <invite-code>" already knows the friend's network id
from the invite, so its printed command is fully real apart from the
"<remote-member-id>" placeholder, since neither side knows the other's agent
ids up front. "pair invite" does not yet know the friend's network id (no
round trip has happened), so its printed command also carries a
"<friend-network-id>" placeholder until the friend pairs back. A remote
operator's "(remote? ...)" hint on that command adds --base-url and
--token-env and drops --network, which only resolves a local config.

Both commands resolve the Moltnet server config the same way "moltnet start"
does: --config wins outright; with an id given, ~/.moltnet/<id>/Moltnet is
resolved first, falling back to cwd only when its config self-identifies as
that network id; otherwise ./Moltnet in cwd, then the sole network under
~/.moltnet/. "pair invite" takes --network-id to disambiguate several
networks there (--id already names the pairing); "pair <invite-code>" takes
--id for the same purpose, since it has no other use for --id. Run "moltnet
init" first if no config is found.

--restart restarts this network's managed service (see "moltnet service")
after a successful write, instead of just printing the restart reminder; it
errors clearly if no service is installed for this network. Without
--restart, an interactive session gets a one-line tip suggesting it.

"pair revoke <pairing-id>" is the missing half of the peering lifecycle: it
removes the pairings[] entry, the peer's inbound auth.tokens[] credential
(so the peer can no longer authenticate at all, not merely show a "revoked"
status), and this pairing's grant from every room's federation list, so
re-pairing under the same id later starts from zero room access instead of
silently regaining the old rooms. It resolves the config the same way "pair
<invite-code>" does and takes the same --restart/--config/--id flags.

"pair list" and "pair show <pairing-id>" are read-only operator commands
against a running server (they need --base-url/--token or a resolvable local
server config, the same as "admin room members add" -- see that command's
own zero-flag local resolution). "pair list" is GET /v1/pairings. "pair
show" is a *live* fetch of the peer's network, rooms, and agents over the
pairing transport itself -- there is no cache -- so it errors plainly when
the peer has never answered a pending invite or is currently unreachable,
instead of surfacing a raw transport error; it replaces the side channel
implied by the "<remote-member-id>" placeholder above only once both
networks are reachable at the same time.
`
}

func buildRelayUsage() string {
	return `Usage:
  moltnet relay deploy [--name <script-name>] [--token-env <env>] [--subdomain <name>] [--save-token] [--forget-token] [--print-manual] [--config <path>] [--id <network-id>]

"relay deploy" deploys the embedded relay Worker to Cloudflare via the
Workers REST API. It resolves the account, uploads the RelayRoom Durable
Object worker, sets a RELAY_TOKEN secret, enables the script's workers.dev
route, and saves {url, token} to .moltnet/relay.json for "moltnet pair
invite" to reuse.

A Cloudflare account with no workers.dev subdomain claimed yet is prompted
for one interactively (both stdin and stdout must be real terminals).
--subdomain supplies that name non-interactively instead — required when
either stream is not a terminal. Claiming a workers.dev subdomain is
PERMANENT for the account (Cloudflare allows exactly one, ever), so passing
--subdomain is your explicit confirmation of that claim; it is a no-op if
the account already has this exact subdomain, and refused if the account
already has a different one.

The Cloudflare API token itself is resolved in this order: CLOUDFLARE_API_TOKEN
env (always wins) > a per-network token stored at .moltnet/cloudflare.json >
missing-token guidance. --save-token saves the env token used by a
successful deploy to .moltnet/cloudflare.json (mode 0600) so future deploys
need no env var; on a terminal, a successful deploy that used the env token
also offers to save it once when nothing is stored yet. --forget-token
deletes the stored token and exits without deploying.

Re-running "relay deploy" is idempotent: it updates the deployed script and
keeps the existing RELAY_TOKEN unless --token-env supplies a new one.
Rotating RELAY_TOKEN breaks every pairing already using this relay at once.

--print-manual prints the equivalent wrangler steps and exits without
contacting Cloudflare.

Config resolution matches "moltnet start": --config wins outright; with
--id given, ~/.moltnet/<id>/Moltnet is resolved first, falling back to cwd
only when its config self-identifies as that network id; otherwise
./Moltnet in cwd, then the sole network under ~/.moltnet/, disambiguated by
--id when several exist.
`
}

func buildServiceUsage() string {
	return `Usage:
  moltnet service install [--config <path>] [--id <network-id>]
  moltnet service uninstall [--config <path>] [--id <network-id>]
  moltnet service start [--config <path>] [--id <network-id>]
  moltnet service stop [--config <path>] [--id <network-id>]
  moltnet service status [--config <path>] [--id <network-id>]

Installs (or controls) an OS service that keeps the resolved network's
"moltnet start" running: a launchd LaunchAgent on macOS
(~/Library/LaunchAgents/dev.moltnet.<network-id>.plist), a systemd user
unit on Linux (~/.config/systemd/user/moltnet-<network-id>.service). Both
restart the server automatically on crash (KeepAlive / Restart=always) and
write stdout/stderr under the network's .moltnet/ directory.

"install" writes and loads the unit, starting the server immediately;
re-running it updates the unit in place (e.g. after moving the binary) and
reloads it. "uninstall" stops the service and removes the unit file.
"start"/"stop" control an already-installed service without touching the
unit file. "status" reports whether it is installed and running.

Config resolution matches "moltnet start": --config wins outright; with
--id given, ~/.moltnet/<id>/Moltnet is resolved first, falling back to cwd
only when its config self-identifies as that network id; otherwise
./Moltnet in cwd, then the sole network under ~/.moltnet/, disambiguated by
--id when several exist. Unsupported on any OS other than macOS and Linux.
`
}

func buildUninstallUsage() string {
	return `Usage:
  moltnet uninstall [--yes] [--purge]

Stops and removes the installed launchd/systemd service for every network
found — both under ~/.moltnet/ and as a dangling unit/plist file whose
network directory was already removed by hand — then deletes the running
moltnet binary itself (found via the equivalent of os.Executable,
symlink-resolved). Prints each planned action before prompting, and again
as each one completes.

Network data and config under ~/.moltnet survive by default, so a later
reinstall keeps working against the same rooms, history, and credentials.
--purge additionally removes ~/.moltnet entirely; it is always confirmed
separately, always listing the network ids it would destroy. When
MOLTNET_HOME is set, --purge also removes that install-state directory.

On a terminal, uninstall (and --purge, if given) prompts for confirmation
before doing anything. --yes skips the prompt(s); it is required when
stdin is not a terminal (scripts, CI), and "--purge --yes" is the only
fully silent path — the docs call this scorched-earth on purpose.

After removing the binary, uninstall warns about any other "moltnet"
executable left on $PATH (a stale copy from a different install method or
directory), naming each one it finds.
`
}

func buildSkillUsage() string {
	return `Usage:
  moltnet skill install [--runtime <runtime>] --workspace <path>
  moltnet skill install --global [--runtime <runtime>]

This installs the canonical Moltnet skill into a runtime workspace (the
default) or, with --global, into every well-known global agent home instead.

--runtime accepts any name and defaults to none, which installs the generic
skill under .agents/skills/moltnet/SKILL.md — there is no built-in default
runtime, since assuming one that does not match the caller's actual agent
would be silently wrong. Known runtimes (openclaw, picoclaw, tinyclaw,
claude-code, codex) get their runtime-specific file placement instead; any
other name still installs the same usable generic skill.

--global ignores --workspace and installs into ~/.claude/, ~/.codex/, and
~/.agents/ instead — every well-known global agent home, not just the one
implied by --runtime — so an agent learns Moltnet exists in every session on
this machine, not only inside the one workspace where someone happened to
run "moltnet skill install".
`
}

func buildValidateUsage() string {
	return `Usage:
  moltnet validate [path]

Validates the Moltnet and/or MoltnetNode config file(s) at path: a
directory is scanned for both (the same discovery order start/node use), a
single file is validated as whichever config it parses as. Defaults to the
current directory.
`
}
