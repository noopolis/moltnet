package bridgeconfig

import "testing"

func TestRuntimeConfigNormalized(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   RuntimeConfig
		want RuntimeConfig
	}{
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
			name: "tinyclaw local api",
			in:   RuntimeConfig{Kind: RuntimeTinyClaw},
			want: RuntimeConfig{
				Kind:        RuntimeTinyClaw,
				Channel:     DefaultTinyClawChannel,
				InboundURL:  "http://127.0.0.1:3777/api/message",
				OutboundURL: "http://127.0.0.1:3777/api/responses/pending?channel=moltnet",
				AckURL:      "http://127.0.0.1:3777/api/responses",
			},
		},
		{
			name: "tinyclaw derives base from explicit inbound url",
			in: RuntimeConfig{
				Kind:       RuntimeTinyClaw,
				Channel:    "moltnet:local:penny",
				InboundURL: "http://127.0.0.1:3888/api/message",
			},
			want: RuntimeConfig{
				Kind:        RuntimeTinyClaw,
				Channel:     "moltnet:local:penny",
				InboundURL:  "http://127.0.0.1:3888/api/message",
				OutboundURL: "http://127.0.0.1:3888/api/responses/pending?channel=moltnet%3Alocal%3Apenny",
				AckURL:      "http://127.0.0.1:3888/api/responses",
			},
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
			if got != test.want {
				t.Fatalf("Normalized() = %#v, want %#v", got, test.want)
			}
		})
	}
}
