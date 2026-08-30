package bridgeconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestMoltnetConfigResolveTokenPrecedence(t *testing.T) {
	t.Setenv("MOLTNET_TOKEN", "env-token")
	tokenPath := filepath.Join(t.TempDir(), "agent.token")
	if err := os.WriteFile(tokenPath, []byte("file-token\n"), 0o600); err != nil {
		t.Fatalf("write token path: %v", err)
	}

	config := MoltnetConfig{
		AuthMode:  AuthModeOpen,
		Token:     "inline-token",
		TokenEnv:  "MOLTNET_TOKEN",
		TokenPath: tokenPath,
	}
	token, ok, err := config.ResolveToken()
	if err != nil {
		t.Fatalf("ResolveToken() inline error = %v", err)
	}
	if !ok || token != "inline-token" {
		t.Fatalf("unexpected inline token %q ok=%v", token, ok)
	}

	config.Token = ""
	token, ok, err = config.ResolveToken()
	if err != nil {
		t.Fatalf("ResolveToken() env error = %v", err)
	}
	if !ok || token != "env-token" {
		t.Fatalf("unexpected env token %q ok=%v", token, ok)
	}

	config.TokenEnv = ""
	token, ok, err = config.ResolveToken()
	if err != nil {
		t.Fatalf("ResolveToken() file error = %v", err)
	}
	if !ok || token != "file-token" {
		t.Fatalf("unexpected file token %q ok=%v", token, ok)
	}
}

func TestMoltnetConfigResolveTokenDoesNotFallThrough(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "agent.token")
	if err := os.WriteFile(tokenPath, []byte("file-token\n"), 0o600); err != nil {
		t.Fatalf("write token path: %v", err)
	}

	_, _, err := (MoltnetConfig{
		AuthMode:  AuthModeOpen,
		TokenEnv:  "MISSING_MOLTNET_TOKEN",
		TokenPath: tokenPath,
	}).ResolveToken()
	if err == nil || !strings.Contains(err.Error(), "MISSING_MOLTNET_TOKEN") {
		t.Fatalf("expected missing env error, got %v", err)
	}
}

func TestOpenTokenPathMayBeMissingBeforeClaim(t *testing.T) {
	_, ok, err := (MoltnetConfig{
		AuthMode:  AuthModeOpen,
		TokenPath: filepath.Join(t.TempDir(), "missing.token"),
	}).ResolveToken()
	if err != nil {
		t.Fatalf("ResolveToken() open missing path error = %v", err)
	}
	if ok {
		t.Fatal("expected unresolved token before first open claim")
	}

	_, _, err = (MoltnetConfig{
		AuthMode:  AuthModeBearer,
		TokenPath: filepath.Join(t.TempDir(), "missing.token"),
	}).ResolveToken()
	if err == nil {
		t.Fatal("expected bearer missing token_path error")
	}
}

func TestValidateAuthRequiresOpenTokenSource(t *testing.T) {
	err := (MoltnetConfig{AuthMode: AuthModeOpen}).ValidateAuth()
	if err == nil || !strings.Contains(err.Error(), "token, token_env, or token_path") {
		t.Fatalf("expected open token source error, got %v", err)
	}
}

func TestResolveTokenPathRelativeToConfig(t *testing.T) {
	baseDir := t.TempDir()
	config := Config{
		Moltnet: MoltnetConfig{TokenPath: ".moltnet/alpha.token"},
	}.ResolveTokenPaths(baseDir)

	want := filepath.Join(baseDir, ".moltnet", "alpha.token")
	if config.Moltnet.TokenPath != want {
		t.Fatalf("TokenPath = %q, want %q", config.Moltnet.TokenPath, want)
	}
}

func TestRuntimeConfigResolveRuntimeToken(t *testing.T) {
	const secret = "daimon-bearer-canary"
	t.Setenv("DAIMON_CONTROL_TOKEN", secret)
	runtime := RuntimeConfig{Kind: RuntimeDaimon, TokenEnv: "DAIMON_CONTROL_TOKEN"}

	token, err := runtime.ResolveRuntimeToken()
	if err != nil {
		t.Fatalf("ResolveRuntimeToken() error = %v", err)
	}
	if token.Reveal() != secret {
		t.Fatal("ResolveRuntimeToken() did not return the environment value")
	}
	encoded, err := json.Marshal(struct {
		Token any `json:"token"`
	}{Token: token})
	if err != nil {
		t.Fatalf("marshal resolved token: %v", err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("serialized resolved token leaked bearer material: %s", encoded)
	}
}

func TestDaimonRuntimeConfigValidation(t *testing.T) {
	base := Config{
		Version: VersionV1,
		Agent:   AgentConfig{ID: "researcher"},
		Moltnet: MoltnetConfig{BaseURL: "http://127.0.0.1:8787", NetworkID: "local"},
		Runtime: RuntimeConfig{Kind: RuntimeDaimon, AgentID: "agent:researcher", ControlURL: "http://127.0.0.1:19690", TokenEnv: "DAIMON_TOKEN", ReceiptStorePath: "/tmp/daimon-receipts.json"},
	}
	tests := []struct {
		name    string
		runtime RuntimeConfig
		ok      bool
	}{
		{name: "valid", runtime: base.Runtime, ok: true},
		{name: "legacy member identity", runtime: RuntimeConfig{Kind: RuntimeDaimon, ControlURL: base.Runtime.ControlURL, TokenEnv: "DAIMON_TOKEN", ReceiptStorePath: base.Runtime.ReceiptStorePath}, ok: true},
		{name: "untrimmed agent id", runtime: RuntimeConfig{Kind: RuntimeDaimon, AgentID: " agent:researcher ", ControlURL: base.Runtime.ControlURL, TokenEnv: "DAIMON_TOKEN"}},
		{name: "invalid agent id", runtime: RuntimeConfig{Kind: RuntimeDaimon, AgentID: "agent/researcher", ControlURL: base.Runtime.ControlURL, TokenEnv: "DAIMON_TOKEN"}},
		{name: "unsupported agent id", runtime: RuntimeConfig{Kind: RuntimePi, AgentID: "agent:researcher", ControlURL: "http://127.0.0.1:9000"}},
		{name: "missing control url", runtime: RuntimeConfig{Kind: RuntimeDaimon, TokenEnv: "DAIMON_TOKEN"}},
		{name: "missing token env", runtime: RuntimeConfig{Kind: RuntimeDaimon, ControlURL: base.Runtime.ControlURL}},
		{name: "inline token", runtime: RuntimeConfig{Kind: RuntimeDaimon, ControlURL: base.Runtime.ControlURL, Token: "secret"}},
		{name: "ambiguous", runtime: RuntimeConfig{Kind: RuntimeDaimon, ControlURL: base.Runtime.ControlURL, Token: "secret", TokenEnv: "DAIMON_TOKEN"}},
		{name: "unsafe env", runtime: RuntimeConfig{Kind: RuntimeDaimon, ControlURL: base.Runtime.ControlURL, TokenEnv: "not-safe"}},
		{name: "relative receipt store", runtime: RuntimeConfig{Kind: RuntimeDaimon, ControlURL: base.Runtime.ControlURL, TokenEnv: "DAIMON_TOKEN", ReceiptStorePath: "receipts.json"}},
		{name: "unsupported receipt store", runtime: RuntimeConfig{Kind: RuntimePi, ControlURL: "http://127.0.0.1:9000", ReceiptStorePath: "/tmp/receipts.json"}},
		{name: "unsupported env source", runtime: RuntimeConfig{Kind: RuntimePi, ControlURL: "http://127.0.0.1:9000", TokenEnv: "PI_TOKEN"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := base
			config.Runtime = test.runtime
			err := config.Validate()
			if (err == nil) != test.ok {
				t.Fatalf("Validate() error = %v, want success %v", err, test.ok)
			}
		})
	}
}

func TestRuntimeConfigResolveRuntimeTokenFailuresAreRedacted(t *testing.T) {
	const secret = "never-print-this-bearer"
	t.Setenv("DAIMON_AMBIGUOUS_TOKEN", secret)
	tests := []RuntimeConfig{
		{Kind: RuntimeDaimon, TokenEnv: "DAIMON_MISSING_TOKEN"},
		{Kind: RuntimeDaimon, Token: protocol.NewSecretString(secret), TokenEnv: "DAIMON_AMBIGUOUS_TOKEN"},
		{Kind: RuntimePi, TokenEnv: "DAIMON_AMBIGUOUS_TOKEN"},
	}
	for _, runtime := range tests {
		_, err := runtime.ResolveRuntimeToken()
		if err == nil {
			t.Fatal("expected runtime token resolution error")
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("runtime token error leaked bearer material: %v", err)
		}
	}
}
