package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestRunConnectWritesConfigAndSkill(t *testing.T) {
	workspace := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/skill.md" {
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
		response.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		fmt.Fprintln(response, "---")
		fmt.Fprintln(response, "name: moltnet")
		fmt.Fprintln(response, `description: "generated test skill"`)
		fmt.Fprintln(response, "---")
		fmt.Fprintln(response, "Generated test skill.")
	}))
	defer server.Close()

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"connect",
			"--workspace", workspace,
			"--runtime", "openclaw",
			"--base-url", server.URL,
			"--network-id", "local_lab",
			"--member-id", "alpha",
			"--agent-name", "Alpha",
			"--rooms", "general,research",
			"--enable-dms",
		}, "test"); err != nil {
			t.Fatalf("run() connect error = %v", err)
		}
	})

	configPath := filepath.Join(workspace, ".moltnet", "config.json")
	skillPath := filepath.Join(workspace, "skills", "moltnet", "SKILL.md")
	assertFileExists(t, configPath)
	assertFileExists(t, skillPath)

	config, err := clientconfig.LoadFile(configPath)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if len(config.Attachments) != 1 || config.Attachments[0].MemberID != "alpha" {
		t.Fatalf("unexpected config %#v", config)
	}
	if !strings.Contains(output, "installed skill") || !strings.Contains(output, "wrote Moltnet client config") {
		t.Fatalf("unexpected connect output %q", output)
	}
	skillBytes, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	if !strings.Contains(string(skillBytes), "Generated test skill.") {
		t.Fatalf("expected generated remote skill, got %s", skillBytes)
	}
}

func TestRunConnectInstallsCodexAndClaudeCodeSkills(t *testing.T) {
	for _, test := range []struct {
		runtime string
		path    string
	}{
		{
			runtime: "codex",
			path:    filepath.Join(".agents", "skills", "moltnet", "SKILL.md"),
		},
		{
			runtime: "claude-code",
			path:    filepath.Join(".claude", "skills", "moltnet", "SKILL.md"),
		},
	} {
		test := test
		t.Run(test.runtime, func(t *testing.T) {
			workspace := t.TempDir()

			if err := run(context.Background(), []string{
				"connect",
				"--workspace", workspace,
				"--runtime", test.runtime,
				"--base-url", "http://127.0.0.1:8787",
				"--network-id", "local_lab",
				"--member-id", test.runtime + "_bot",
				"--agent-name", test.runtime + " Bot",
				"--rooms", "research",
			}, "test"); err != nil {
				t.Fatalf("run() connect error = %v", err)
			}

			assertFileExists(t, filepath.Join(workspace, test.path))
			config, err := clientconfig.LoadFile(filepath.Join(workspace, ".moltnet", "config.json"))
			if err != nil {
				t.Fatalf("LoadFile() error = %v", err)
			}
			if config.Agent.Runtime != test.runtime || config.Attachments[0].Runtime != test.runtime {
				t.Fatalf("unexpected runtime config %#v", config)
			}
		})
	}
}

func TestRunConnectFetchesGeneratedSkillWithToken(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("MOLTNET_TOKEN", "write-secret")
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		authHeader = request.Header.Get("Authorization")
		fmt.Fprintln(response, "---")
		fmt.Fprintln(response, "name: moltnet")
		fmt.Fprintln(response, `description: "token scoped"`)
		fmt.Fprintln(response, "---")
		fmt.Fprintln(response, "Token scoped generated skill.")
	}))
	defer server.Close()

	if err := run(context.Background(), []string{
		"connect",
		"--workspace", workspace,
		"--runtime", "codex",
		"--base-url", server.URL,
		"--network-id", "private",
		"--member-id", "alpha",
		"--rooms", "ops",
		"--auth-mode", "bearer",
		"--token-env", "MOLTNET_TOKEN",
	}, "test"); err != nil {
		t.Fatalf("run() connect error = %v", err)
	}

	if authHeader != "Bearer write-secret" {
		t.Fatalf("unexpected auth header %q", authHeader)
	}
	skillBytes, err := os.ReadFile(filepath.Join(workspace, ".codex", "skills", "moltnet", "SKILL.md"))
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	if !strings.Contains(string(skillBytes), "Token scoped generated skill.") {
		t.Fatalf("expected generated skill, got %s", skillBytes)
	}
}

func TestRunConnectRegistrationOpenStoresGeneratedTokenAsOpenAuth(t *testing.T) {
	workspace := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/skill.md":
			response.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			fmt.Fprintln(response, "---")
			fmt.Fprintln(response, "name: moltnet")
			fmt.Fprintln(response, `description: "generated test skill"`)
			fmt.Fprintln(response, "---")
			fmt.Fprintln(response, "Generated test skill.")
		case request.Method == http.MethodPost && request.URL.Path == "/v1/agents/register":
			_ = json.NewEncoder(response).Encode(protocol.AgentRegistration{
				NetworkID:   "public",
				AgentID:     "guest-agent",
				ActorUID:    "actor_public_1",
				ActorURI:    protocol.AgentFQID("public", "guest-agent"),
				DisplayName: "Guest Agent",
				AgentToken:  "magt_v1_generated",
			})
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()

	if err := run(context.Background(), []string{
		"connect",
		"--workspace", workspace,
		"--runtime", "codex",
		"--base-url", server.URL,
		"--network-id", "public",
		"--member-id", "guest-agent",
		"--agent-name", "Guest Agent",
		"--rooms", "guestbook",
		"--auth-mode", "none",
		"--registration", "open",
	}, "test"); err != nil {
		t.Fatalf("run() connect error = %v", err)
	}

	config, err := clientconfig.LoadFile(filepath.Join(workspace, ".moltnet", "config.json"))
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	auth := config.Attachments[0].Auth
	if auth.Mode != "open" || auth.Registration != "open" || auth.Token != "magt_v1_generated" {
		t.Fatalf("generated registration token was not stored as open auth: %#v", auth)
	}
}
