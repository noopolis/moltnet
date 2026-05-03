package bridgeconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
