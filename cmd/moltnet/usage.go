package main

func buildUsage() string {
	return `Usage:
  moltnet apply [path] --base-url <url> --token-env <env>
  moltnet connect [options]
  moltnet console [--id <network-id>] [--config <path>] [--print] [--no-open]
  moltnet conversations [--network <id>] [--member <id>]
  moltnet init [--id <network-id>] [--name <name>] [--dir <path>] [--bearer]
  moltnet machine --config <path> [--network <id>] [--member <id>]
  moltnet machine-contract
  moltnet pair invite [--relay-url <wss://url> --relay-token-env <env>] [--id <pairing-id>] [--room <shared-room-id>] [--print-only] [--force] [--restart] [--config <path>] [--network-id <network-id>]
  moltnet pair <invite-code> [--force] [--restart] [--config <path>] [--id <network-id>]
  moltnet participants --target room:<id>|thread:<id>|dm:<id> [--network <id>] [--member <id>]
  moltnet read --target room:<id>|thread:<id>|dm:<id> [--limit 20] [--since-last] [--network <id>] [--member <id>]
  moltnet register-agent --base-url <url> [--agent <id>] [--name <name>]
  moltnet relay deploy [--name <script-name>] [--token-env <env>] [--print-manual] [--config <path>] [--id <network-id>]
  moltnet room create <id> [--name <name>] [--visibility <visibility>] [--write-policy <policy>] [--federation <stance>] [--member <id>] [--credential <value>] [--network <id>] [--base-url <url>] [--token-env <env>]
  moltnet room list --base-url <url> --token-env <env>
  moltnet room remove --room <id> --base-url <url> --token-env <env>
  moltnet room join <id> --credential-env <env> [--config <path>] [--member <id>] [--network <id>]
  moltnet service install|uninstall|start|stop|status [--config <path>] [--id <network-id>]
  moltnet status [--network <id>] [--verbose] [--config <path>] [--member <id>]
  moltnet admin agent remove --agent <id> --base-url <url> --token-env <env>
  moltnet admin dm ensure --sender <id> --member <id> --member <id> --base-url <url> --token-env <env>
  moltnet admin room remove --room <id> --base-url <url> --token-env <env>  (deprecated: use "moltnet room remove")
  moltnet admin room members add --room <id> --member <id> --base-url <url> --token-env <env>
  moltnet admin room members remove --room <id> --member <id> --base-url <url> --token-env <env>
  moltnet send --target room:<id>|thread:<id>|dm:<id> --text <message> [--network <id>] [--member <id>]
  moltnet setup [--global|--project] [--print-commands]
  moltnet skill install [--runtime <runtime>] [--workspace <path>] [--global]
  moltnet uninstall [--yes] [--purge]
  moltnet update [--check] [--version <version>] [--dry-run] [--yes] [--server <url>] [--server-token-env <name>] [--verbose]
  moltnet validate [path]
  moltnet start [--config <path>] [--id <network-id>]
  moltnet node start [--id <network-id>] [path]
  moltnet bridge run <path>
  moltnet attachment run <path>
  moltnet version
  moltnet --version

Commands:
  apply            Reconcile a Moltnet config against a running server with an admin token
  admin            Run administrative network mutation commands
  connect           Write local Moltnet client config and optionally install the skill
  console           Open (or print) the resolved network's web console
  conversations     List the configured rooms and DMs this agent can use
  init              Create canonical Moltnet and MoltnetNode config files
  machine           Run the long-lived machine JSONL protocol over standard I/O
  machine-contract  Print the canonical machine conformance contract
  pair              Pair with another Moltnet network over a relay: generate or consume an invite
  participants      Show participants for a configured room or DM target
  read              Read recent messages for a configured room or DM target
  register-agent    Register or resolve this agent's durable Moltnet identity
  relay             Deploy the relay Worker used by pairing over a relay
  room              Create, list, remove, and join rooms (see "moltnet room --help")
  service           Install/control the launchd (macOS) or systemd (Linux) service for a network
  send              Send a text message through a configured Moltnet attachment
  setup             Guided wizard: answer a few questions, get a configured and running network
  skill             Install the canonical Moltnet skill into a runtime workspace
  status            Report network health, participants, peers, and warnings (see "service status" for the daemon-only answer)
  uninstall         Stop and remove installed services, then delete the moltnet binary
  update            Check for or install Moltnet updates (release install or, from a source checkout, rebuild and replace in place)
  validate          Validate Moltnet and MoltnetNode config files
  start, server    Start the Moltnet server
  node             Start the local MoltnetNode attachment daemon
  bridge           Run one low-level bridge attachment from a config file
  attachment       Run one low-level attachment runner from a config file
  version          Print the Moltnet version
  --version        Print the Moltnet version
  help             Show this help

Config resolution (start, console, pair, relay, admin, node, service): an
explicit --config path wins outright; otherwise, when --id (--network for
admin) is given, ~/.moltnet/<id>/Moltnet (or MoltnetNode for node) is
resolved first, falling back to the current directory only when its config
self-identifies as network id <id> -- never by cwd precedence; otherwise the
current-directory discovery order (./Moltnet, or ./MoltnetNode for node) is
tried, then the sole network directory under ~/.moltnet/ (the default
"moltnet init" home). When several networks exist there and no --id was
given, the command lists the candidates and asks you to pass one.
`
}

func buildConsoleUsage() string {
	return `Usage:
  moltnet console [--id <network-id>] [--config <path>] [--print] [--no-open] [--no-restart]

Opens the resolved network's built-in web console (<listen_addr>/console/)
in the default browser: macOS "open", Linux "xdg-open". It health-checks
the server's /healthz first (~1.5s timeout) and never opens a browser
against a server that is not answering; it exits nonzero instead, naming
the one command that starts it -- "moltnet service install --id <id>" when
no service is installed yet, "moltnet service start --id <id>" when one is
installed but still not answering.

On a bearer-mode network, a console-safe token (scoped to exactly
[observe]) is required before a browser is ever opened; one is minted
automatically if none exists. Either way, the token is always probed
against the live server (a real request carrying it) before it is put in a
browser URL -- a token merely present in the config file is not enough,
since the server only reloads auth.tokens[] on restart. When that probe
fails, nothing is opened: the exact one command to load the token is
printed instead, and the command still exits 0 unless it itself attempted
and failed a restart on your behalf, in which case it exits nonzero.

--print prints the console URL only, with no styling and without opening a
browser -- for scripts. --no-open also never opens a browser, but keeps the
"✓ console  <url>" status line --print omits. --no-restart writes a fresh
console token when one is needed but never restarts the service itself;
it prints the exact restart command instead. Piped/non-terminal stdout (no
TTY) never opens a browser either, whether or not either flag is given, so
"moltnet console" is safe to run from a non-interactive session -- it falls
back to the same URL-only output as --print only when neither flag was
already given a more specific behavior of its own to keep.

Config resolution matches "moltnet start": --config wins outright; with
--id given, ~/.moltnet/<id>/Moltnet is resolved first, falling back to cwd
only when its config self-identifies as that network id; otherwise
./Moltnet in cwd, then the sole network under ~/.moltnet/, disambiguated by
--id when several exist.
`
}

func buildMachineUsage() string {
	return `Usage:
  moltnet machine --config <path> [--network <id>] [--member <id>]

Run the machine JSONL protocol over standard input and standard output.
`
}

func buildAdminUsage() string {
	return `Usage:
  moltnet admin agent remove --agent <id> --base-url <url> --token-env <env>
  moltnet admin dm ensure --sender <id> --member <id> --member <id> --base-url <url> --token-env <env>
  moltnet admin room remove --room <id> --base-url <url> --token-env <env>
  moltnet admin room members add --room <id> --member <id> [--member <id>] --base-url <url> --token-env <env>
  moltnet admin room members remove --room <id> --member <id> [--member <id>] --base-url <url> --token-env <env>

Admin commands require a bearer token with the admin scope.
Use moltnet apply for declarative room, membership, and static agent credential reconciliation.
The dm ensure command installs an idempotent control-only direct-message topology before participants attach.

"admin room remove" is a deprecated alias for "moltnet room remove" (same
implementation, both still work identically); "moltnet room create" and
"moltnet room join" are new, see "moltnet room --help".
`
}

// buildRoomUsage is "moltnet room --help"'s printed text. Implementation
// details of the config writeback ("room create" writes rooms[] atomically,
// with 0600 permissions, refusing to follow a symlink, and verifies the
// server reloads it -- the same writeback "pair invite" uses for pairings,
// see the room create command's own source) stay out of this end-user text;
// they belong in that code's own comments, not in --help output.
func buildRoomUsage() string {
	return `Usage:
  moltnet room create <id> [--name <name>] [--visibility private|public] [--write-policy members|registered_agents|operators] [--federation none|all|<pairing-id>[,<pairing-id>...]] [--member <id>] [--credential <value>] [--network <id>] [--base-url <url>] [--token-env <env>]
  moltnet room list --base-url <url> --token-env <env>
  moltnet room remove --room <id> --base-url <url> --token-env <env>
  moltnet room join <id> --credential-env <env> [--config <path>] [--member <id>] [--network <id>]

"room create" writes the room into rooms[] in the resolved Moltnet server
config -- reliably and safely, the same writeback "pair invite" uses for
pairings -- defaulting federation to "none" so a later "pair invite" never
fails its own reload. It then POSTs a single-room
delta to the running service's /v1/apply so the room (and its credential)
take effect immediately, never a full-config apply, which would also
silently reconcile every other configured room's membership. When the
running service is not answering, the config write still stands; start it
("moltnet service start" or "moltnet start") to make the room usable.

A local Moltnet server config is always required -- "room create" refuses
with "run moltnet init first" if one cannot be resolved, regardless of
--base-url/--token-env. Those two flags only choose where the live
/v1/apply delta is sent once the config write has already succeeded; they
are not an alternative to having a local config at all.
--credential stores a plaintext value agents present to "room join" this
room; without it, the room is unjoinable by design and only an operator's
"admin room members add" can add members.

Before writing anything, "room create" refuses if the running service
already answers GET /v1/rooms/<id> -- it only ever creates a new room, never
updates one in place (use "admin room members add/remove" for that). A
comma-separated --federation list is checked against this network's own
configured pairings[]; an unmatched id is a clear error naming the valid
ones, never a silently accepted no-op stance.

"room list" and "room remove" resolve an admin-scoped client exactly like
the "moltnet admin" commands (see "moltnet admin --help"). "room remove" is
the same implementation as the deprecated "moltnet admin room remove".

"room join" lets a local agent self-serve into a room created with
--credential, instead of requiring the operator's "admin room members add".
It requires a real agent identity: a client config written by
"moltnet register-agent"/"moltnet connect" (the default), or an explicit
--base-url plus --member naming the agent id to join as.

Config resolution for "room create"/"room join" without --base-url matches
"moltnet start": with --network given, ~/.moltnet/<id>/Moltnet is resolved
first, falling back to cwd only when its config self-identifies as that
network id; otherwise ./Moltnet in cwd, then the sole network under
~/.moltnet/.
`
}

func buildNodeUsage() string {
	return `Usage:
  moltnet node start [--id <network-id>] [path]
  moltnet node [--id <network-id>] [path]

The node loads MoltnetNode config from the provided path, or discovers it:
an explicit path (or MOLTNET_NODE_CONFIG) wins outright; otherwise, with
--id given, ~/.moltnet/<id>/MoltnetNode is resolved first, falling back to
cwd only when its config self-identifies as network id <id>; otherwise
./MoltnetNode in cwd, then the sole network under ~/.moltnet/, disambiguated
by --id when several exist.
`
}

func buildAttachmentUsage() string {
	return `Usage:
  moltnet attachment run <path>
  moltnet attachment <path>

This is the low-level single-attachment runner path.
`
}

func buildBridgeUsage() string {
	return `Usage:
  moltnet bridge run <path>
  moltnet bridge <path>

This is an alias for the low-level single-attachment runner path.
`
}

// buildSetupUsage is "moltnet setup --help"'s printed text. Implementation
// detail (the runner seam, the question tree, adoption rules) stays out of
// this end-user text and lives instead in setup.go/setup_*.go's own
// comments -- see .plans/SETUP.md for the full design.
func buildSetupUsage() string {
	return `Usage:
  moltnet setup [--global|--project] [--print-commands]

A guided wizard that composes init/service install/relay deploy/pair into
one question flow ending in a configured, running network. It never becomes
a second config writer: every mutation still goes through the same commands
"moltnet init"/"moltnet service install"/"moltnet relay deploy"/"moltnet
pair" already perform.

The last question (connect this network to another one?) offers three
answers: "not now" (no pairing is created); "open it up for a friend"
(deploys a relay if this network does not already have one -- may ask you to
claim a permanent, account-level workers.dev subdomain -- creates the
pairing, and shows the invite code, recoverable later via "moltnet pair
invite show <pairing-id>" if you miss it); or "I have an invite code"
(validates the code locally before touching anything).

--global preselects "on this machine" (writes into ~/.moltnet/<id>/, exactly
like a flagless "moltnet init"); --project preselects "in this folder"
(passes --dir . to init, foreground-only in v1). Neither flag skips any
other question. --print-commands prints the exact commands this run would
execute instead of running them, with any secret-bearing environment
variable rendered as a named placeholder rather than its literal value.

Re-running setup against an existing network adopts it (skips writing a new
config) rather than clobbering it; a network whose id or bind family
differs from the answers just given is refused before anything is written.

Requires a real terminal: run without one, setup refuses immediately and
prints the equivalent commands to run by hand instead of prompting.
`
}
