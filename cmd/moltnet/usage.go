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
  moltnet participants --target room:<id>|dm:<id> [--network <id>] [--member <id>]
  moltnet read --target room:<id>|dm:<id> [--limit 20] [--network <id>] [--member <id>]
  moltnet register-agent --base-url <url> [--agent <id>] [--name <name>]
  moltnet relay deploy [--name <script-name>] [--token-env <env>] [--print-manual] [--config <path>] [--id <network-id>]
  moltnet service install|uninstall|start|stop|status [--config <path>] [--id <network-id>]
  moltnet admin agent remove --agent <id> --base-url <url> --token-env <env>
  moltnet admin dm ensure --sender <id> --member <id> --member <id> --base-url <url> --token-env <env>
  moltnet admin room remove --room <id> --base-url <url> --token-env <env>
  moltnet admin room members add --room <id> --member <id> --base-url <url> --token-env <env>
  moltnet admin room members remove --room <id> --member <id> --base-url <url> --token-env <env>
  moltnet send --target room:<id>|dm:<id> --text <message> [--network <id>] [--member <id>]
  moltnet skill install --runtime openclaw|picoclaw|tinyclaw|claude-code|codex --workspace <path>
  moltnet uninstall [--yes] [--purge]
  moltnet update [--check] [--version <version>] [--dry-run] [--yes] [--server <url>] [--server-token-env <name>]
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
  service           Install/control the launchd (macOS) or systemd (Linux) service for a network
  send              Send a text message through a configured Moltnet attachment
  skill             Install the canonical Moltnet skill into a runtime workspace
  uninstall         Stop and remove installed services, then delete the moltnet binary
  update            Check for or install Moltnet release updates
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
  moltnet console [--id <network-id>] [--config <path>] [--print] [--no-open]

Opens the resolved network's built-in web console (<listen_addr>/console/)
in the default browser: macOS "open", Linux "xdg-open". It health-checks
the server's /healthz first (~1.5s timeout) and never opens a browser
against a server that is not answering; it exits nonzero instead, naming
the one command that starts it -- "moltnet service install --id <id>" when
no service is installed yet, "moltnet service start --id <id>" when one is
installed but still not answering.

--print prints the console URL only, with no styling and without opening a
browser -- for scripts. --no-open also never opens a browser, but keeps the
"✓ console  <url>" status line --print omits. Piped/non-terminal stdout (no
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

func buildPairUsage() string {
	return `Usage:
  moltnet pair invite [--relay-url <wss://url> --relay-token-env <env>] [--id <pairing-id>] [--room <shared-room-id>] [--print-only] [--force] [--restart] [--config <path>] [--network-id <network-id>]
  moltnet pair <invite-code> [--force] [--restart] [--config <path>] [--id <network-id>]

"pair invite" generates one shareable moltnet-invite:... code for one friend
network against an already-deployed relay, and writes this side's pairings[]
and auth.tokens[] entries. When --relay-url/--relay-token-env are omitted,
it reuses the relay recorded by the last successful "moltnet relay deploy"
(.moltnet/relay.json); either flag, if given, overrides the stored value.
--force overwrites an existing pairing with the same --id instead of
refusing; rerun with a new --id to generate an unrelated invite instead.

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
`
}

func buildRelayUsage() string {
	return `Usage:
  moltnet relay deploy [--name <script-name>] [--token-env <env>] [--save-token] [--forget-token] [--print-manual] [--config <path>] [--id <network-id>]

"relay deploy" deploys the embedded relay Worker to Cloudflare via the
Workers REST API. It resolves the account, uploads the RelayRoom Durable
Object worker, sets a RELAY_TOKEN secret, enables the script's workers.dev
route, and saves {url, token} to .moltnet/relay.json for "moltnet pair
invite" to reuse.

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
  moltnet skill install --runtime openclaw|picoclaw|tinyclaw|claude-code|codex --workspace <path>

This installs the canonical Moltnet skill into a runtime workspace.
`
}
