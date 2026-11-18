package nodeconfig

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

func TestLoadACPAttachment(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultPath)
	contents := `version: moltnet.node.v1
moltnet:
  base_url: http://127.0.0.1:8787
  network_id: local
attachments:
  - agent: {id: opencode_bot}
    runtime:
      kind: acp
      command: opencode
      args: [acp, "literal argument with spaces"]
      workspace_path: ./opencode-workspace
    rooms:
      - {id: research, wake: mentions}
    dms: {enabled: true}
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	configs := config.BridgeConfigs()
	if len(configs) != 1 {
		t.Fatalf("loaded %d attachments, want one", len(configs))
	}
	runtime := configs[0].Runtime
	if runtime.Kind != bridgeconfig.RuntimeACP || runtime.Command != "opencode" || runtime.WorkspacePath != "./opencode-workspace" ||
		!slices.Equal(runtime.Args, []string{"acp", "literal argument with spaces"}) {
		t.Fatalf("wrong ACP config: %+v", runtime)
	}
}
