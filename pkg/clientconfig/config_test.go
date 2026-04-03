package clientconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
  "version": "moltnet.client.v1",
  "agent": {"name": "Alpha", "runtime": "openclaw"},
  "attachments": [
    {
      "agent_name": "Alpha",
      "auth": {"mode": "none"},
      "base_url": "http://127.0.0.1:8787",
      "member_id": "alpha",
      "network_id": "local_lab",
      "rooms": [{"id": "general", "read": "all", "reply": "manual"}]
    }
  ]
}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	config, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if config.Attachments[0].MemberID != "alpha" {
		t.Fatalf("unexpected attachment %#v", config.Attachments[0])
	}
}

func TestLoadFileRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"attachments":[{"auth":{"mode":"none"},"base_url":"http://127.0.0.1:8787","member_id":"alpha","network_id":"local","wat":true}]}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestDiscoverPath(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, ".moltnet", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"attachments":[{"auth":{"mode":"none"},"base_url":"http://127.0.0.1:8787","member_id":"alpha","network_id":"local"}]}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(directory); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	discovered, ok, err := DiscoverPath("")
	if err != nil {
		t.Fatalf("DiscoverPath() error = %v", err)
	}
	if !ok || discovered != DefaultPath {
		t.Fatalf("unexpected discovery path=%q ok=%v", discovered, ok)
	}
}

func TestResolveAttachment(t *testing.T) {
	config := Config{
		Attachments: []AttachmentConfig{
			{Auth: AuthConfig{Mode: "none"}, BaseURL: "http://127.0.0.1:8787", MemberID: "alpha", NetworkID: "a"},
			{Auth: AuthConfig{Mode: "none"}, BaseURL: "http://127.0.0.1:9787", MemberID: "alpha", NetworkID: "b"},
		},
	}

	if _, err := config.ResolveAttachment(""); err == nil {
		t.Fatal("expected explicit network error")
	}
	attachment, err := config.ResolveAttachment("b")
	if err != nil {
		t.Fatalf("ResolveAttachment() error = %v", err)
	}
	if attachment.BaseURL != "http://127.0.0.1:9787" {
		t.Fatalf("unexpected attachment %#v", attachment)
	}
}

func TestResolveTokenFromEnv(t *testing.T) {
	t.Setenv("MOLTNET_TOKEN", "secret")

	attachment := AttachmentConfig{
		Auth:      AuthConfig{Mode: "bearer", TokenEnv: "MOLTNET_TOKEN"},
		BaseURL:   "http://127.0.0.1:8787",
		MemberID:  "alpha",
		NetworkID: "local",
	}
	token, err := attachment.ResolveToken()
	if err != nil {
		t.Fatalf("ResolveToken() error = %v", err)
	}
	if token != "secret" {
		t.Fatalf("unexpected token %q", token)
	}
}

func TestValidateRejectsMissingBearerToken(t *testing.T) {
	err := AttachmentConfig{
		Auth:      AuthConfig{Mode: "bearer"},
		BaseURL:   "http://127.0.0.1:8787",
		MemberID:  "alpha",
		NetworkID: "local",
	}.Validate()
	if err == nil || !strings.Contains(err.Error(), "auth.token or auth.token_env") {
		t.Fatalf("expected bearer auth error, got %v", err)
	}
}
