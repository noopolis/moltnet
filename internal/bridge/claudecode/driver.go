package claudecode

import (
	"strings"

	"github.com/noopolis/moltnet/internal/bridge/clisession"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

type Driver struct{}

func (Driver) Name() string {
	return bridgeconfig.RuntimeClaudeCode
}

func (Driver) DefaultCommand() string {
	return "claude"
}

func (Driver) UsesSessionIDForFirstDelivery() bool {
	return true
}

func (Driver) BuildCommand(config bridgeconfig.Config, delivery clisession.Delivery) (clisession.CommandSpec, error) {
	command := strings.TrimSpace(config.Runtime.Command)
	if command == "" {
		command = "claude"
	}

	args := []string{
		"--print",
		"--session-id",
		delivery.RuntimeSessionID,
		delivery.Prompt,
	}

	return clisession.CommandSpec{
		Command: command,
		Args:    args,
	}, nil
}

func (Driver) ExtractRuntimeSessionID(clisession.CommandResult) string {
	return ""
}
