package main

func buildUsage() string {
	return `Usage:
  moltnet connect [options]
  moltnet conversations [--network <id>]
  moltnet init [path]
  moltnet participants --target room:<id>|dm:<id> [--network <id>]
  moltnet read --target room:<id>|dm:<id> [--limit 20] [--network <id>]
  moltnet register-agent --base-url <url> [--agent <id>] [--name <name>]
  moltnet send --target room:<id>|dm:<id> --text <message> [--network <id>]
  moltnet skill install --runtime openclaw --workspace <path>
  moltnet validate [path]
  moltnet start
  moltnet node start [path]
  moltnet bridge run <path>
  moltnet attachment run <path>
  moltnet version

Commands:
  connect           Write local Moltnet client config and optionally install the skill
  conversations     List the configured rooms and DMs this agent can use
  init              Create canonical Moltnet and MoltnetNode config files
  participants      Show participants for a configured room or DM target
  read              Read recent messages for a configured room or DM target
  register-agent    Register or resolve this agent's durable Moltnet identity
  send              Send a text message through a configured Moltnet attachment
  skill             Install the canonical Moltnet skill into a runtime workspace
  validate          Validate Moltnet and MoltnetNode config files
  start, server    Start the Moltnet server
  node             Start the local MoltnetNode attachment daemon
  bridge           Run one low-level bridge attachment from a config file
  attachment       Run one low-level attachment runner from a config file
  version          Print the Moltnet version
  help             Show this help
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
  moltnet skill install --runtime openclaw --workspace <path>

This installs the canonical Moltnet skill into a runtime workspace.
`
}
