package bridgeconfig

import (
	"strings"
	"testing"
)

func TestACPValidation(t *testing.T) {
	valid := RuntimeConfig{Kind: RuntimeACP, Command: "opencode", Args: []string{"acp"}, WorkspacePath: "./agent"}
	tests := []struct {
		name string
		edit func(*RuntimeConfig)
		want string
	}{
		{name: "managed ACP"},
		{name: "missing command", edit: func(r *RuntimeConfig) { r.Command = "" }, want: "runtime.command is required for acp"},
		{name: "missing workspace", edit: func(r *RuntimeConfig) { r.WorkspacePath = "" }, want: "runtime.workspace_path is required for acp"},
		{name: "invalid argument", edit: func(r *RuntimeConfig) { r.Args = []string{"acp\x00"} }, want: "runtime.args must not contain a NUL byte"},
		{name: "attached unsupported", edit: func(r *RuntimeConfig) { r.Mode = RuntimeModeAttached }, want: "only supported for claude-code or codex"},
		{name: "args limited to ACP", edit: func(r *RuntimeConfig) { r.Kind = RuntimeCodex }, want: "runtime.args is only supported for acp"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime := valid
			if test.edit != nil {
				test.edit(&runtime)
			}
			config := Config{
				Version: VersionV1, Agent: AgentConfig{ID: "acp-bot"}, Runtime: runtime,
				Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
			}
			err := config.Validate()
			if test.want == "" && err != nil || test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("Validate() = %v, want %q", err, test.want)
			}
		})
	}
}
