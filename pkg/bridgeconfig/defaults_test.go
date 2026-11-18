package bridgeconfig

import (
	"reflect"
	"testing"
)

func TestRuntimeConfigNormalized(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   RuntimeConfig
		want RuntimeConfig
	}{
		{
			name: "daimon remains explicit",
			in: RuntimeConfig{
				Kind:       RuntimeDaimon,
				ControlURL: "http://127.0.0.1:19690",
				TokenEnv:   "DAIMON_TOKEN",
			},
			want: RuntimeConfig{
				Kind:       RuntimeDaimon,
				ControlURL: "http://127.0.0.1:19690",
				TokenEnv:   "DAIMON_TOKEN",
			},
		},
		{
			name: "openclaw gateway",
			in:   RuntimeConfig{Kind: RuntimeOpenClaw},
			want: RuntimeConfig{Kind: RuntimeOpenClaw, GatewayURL: DefaultOpenClawGatewayURL},
		},
		{
			name: "picoclaw event socket",
			in:   RuntimeConfig{Kind: RuntimePicoClaw},
			want: RuntimeConfig{Kind: RuntimePicoClaw, EventsURL: DefaultPicoClawEventsURL},
		},
		{
			name: "picoclaw command from config path",
			in:   RuntimeConfig{Kind: RuntimePicoClaw, ConfigPath: "/tmp/pico/config.json"},
			want: RuntimeConfig{Kind: RuntimePicoClaw, Command: DefaultPicoClawCommand, ConfigPath: "/tmp/pico/config.json"},
		},
		{
			name: "claude code command",
			in:   RuntimeConfig{Kind: RuntimeClaudeCode, WorkspacePath: "/tmp/claude"},
			want: RuntimeConfig{Kind: RuntimeClaudeCode, Command: DefaultClaudeCodeCommand, WorkspacePath: "/tmp/claude"},
		},
		{
			name: "codex command",
			in:   RuntimeConfig{Kind: RuntimeCodex, WorkspacePath: "/tmp/codex"},
			want: RuntimeConfig{Kind: RuntimeCodex, Command: DefaultCodexCommand, WorkspacePath: "/tmp/codex"},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := test.in.Normalized()
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Normalized() = %#v, want %#v", got, test.want)
			}
		})
	}
}
