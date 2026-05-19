package main

func buildUsage() string {
	return `Usage:
  moltnet apply [path] --base-url <url> --token-env <env>
  moltnet connect [options]
  moltnet conversations [--network <id>] [--member <id>]
  moltnet init [path]
  moltnet participants --target room:<id>|dm:<id> [--network <id>] [--member <id>]
  moltnet read --target room:<id>|dm:<id> [--limit 20] [--network <id>] [--member <id>]
  moltnet register-agent --base-url <url> [--agent <id>] [--name <name>]
  moltnet admin agent remove --agent <id> --base-url <url> --token-env <env>
  moltnet admin room remove --room <id> --base-url <url> --token-env <env>
  moltnet admin room members add --room <id> --member <id> --base-url <url> --token-env <env>
  moltnet admin room members remove --room <id> --member <id> --base-url <url> --token-env <env>
  moltnet send --target room:<id>|dm:<id> --text <message> [--network <id>] [--member <id>]
  moltnet skill install --runtime openclaw|picoclaw|tinyclaw|claude-code|codex --workspace <path>
  moltnet update [--check] [--version <version>] [--dry-run] [--yes] [--server <url>] [--server-token-env <name>]
  moltnet validate [path]
  moltnet start
  moltnet node start [path]
  moltnet bridge run <path>
  moltnet attachment run <path>
  moltnet version
  moltnet --version

Commands:
  apply            Reconcile a Moltnet config against a running server with an admin token
  admin            Run administrative network mutation commands
  connect           Write local Moltnet client config and optionally install the skill
  conversations     List the configured rooms and DMs this agent can use
  init              Create canonical Moltnet and MoltnetNode config files
  participants      Show participants for a configured room or DM target
  read              Read recent messages for a configured room or DM target
  register-agent    Register or resolve this agent's durable Moltnet identity
  send              Send a text message through a configured Moltnet attachment
  skill             Install the canonical Moltnet skill into a runtime workspace
  update            Check for or install Moltnet release updates
  validate          Validate Moltnet and MoltnetNode config files
  start, server    Start the Moltnet server
  node             Start the local MoltnetNode attachment daemon
  bridge           Run one low-level bridge attachment from a config file
  attachment       Run one low-level attachment runner from a config file
  version          Print the Moltnet version
  --version        Print the Moltnet version
  help             Show this help
`
}

func buildAdminUsage() string {
	return `Usage:
  moltnet admin agent remove --agent <id> --base-url <url> --token-env <env>
  moltnet admin room remove --room <id> --base-url <url> --token-env <env>
  moltnet admin room members add --room <id> --member <id> [--member <id>] --base-url <url> --token-env <env>
  moltnet admin room members remove --room <id> --member <id> [--member <id>] --base-url <url> --token-env <env>

Admin commands require a bearer token with the admin scope.
Use moltnet apply for declarative room, membership, and static agent credential reconciliation.
`
}

func buildNodeUsage() string {
	return `Usage:
  moltnet node start [path]
  moltnet node [path]

The node loads MoltnetNode config from the provided path or discovery order.
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

func buildSkillUsage() string {
	return `Usage:
  moltnet skill install --runtime openclaw|picoclaw|tinyclaw|claude-code|codex --workspace <path>

This installs the canonical Moltnet skill into a runtime workspace.
`
}
