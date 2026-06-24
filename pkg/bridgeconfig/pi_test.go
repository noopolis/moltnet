package bridgeconfig

import (
	"strings"
	"testing"
)

func TestPiRuntimeConfigValidate(t *testing.T) {
	t.Parallel()

	valid := Config{
		Version: VersionV1,
		Agent:   AgentConfig{ID: "researcher"},
		Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
		Runtime: RuntimeConfig{
			Kind:       RuntimePi,
			ControlURL: "http://127.0.0.1:9000/control",
		},
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	missingControlURL := valid
	missingControlURL.Runtime.ControlURL = ""
	err := missingControlURL.Validate()
	if err == nil || !strings.Contains(err.Error(), "runtime.control_url is required for pi") {
		t.Fatalf("expected missing control url error, got %v", err)
	}

	invalidControlURL := valid
	invalidControlURL.Runtime.ControlURL = "ws://127.0.0.1:9000/control"
	err = invalidControlURL.Validate()
	if err == nil || !strings.Contains(err.Error(), `runtime.control_url scheme "ws" is unsupported`) {
		t.Fatalf("expected invalid control url scheme error, got %v", err)
	}
}
