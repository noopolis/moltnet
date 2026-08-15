package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// writeAdminFallbackServerConfig writes a Moltnet server config whose
// server.listen_addr matches host (an httptest server's host:port) and
// whose auth.tokens[] carries one admin-scoped, inline-plaintext token —
// exactly what resolveAdminFromServerConfig derives --base-url/--token
// from.
func writeAdminFallbackServerConfig(t *testing.T, path string, host string, token string) {
	t.Helper()
	contents := fmt.Sprintf(`version: moltnet.v1

network:
  id: acme
  name: Acme Moltnet

server:
  listen_addr: %q

auth:
  mode: bearer
  tokens:
    - id: operator
      value: %s
      scopes: [observe, write, admin]
`, host, token)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

func TestResolveAdminClientFallsBackToServerConfigInCwd(t *testing.T) {
	var authHeaders []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(protocol.RemoveResult{Removed: true, Kind: "agent", ID: "stale-agent", Mode: "soft"})
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	directory := t.TempDir()
	t.Chdir(directory)
	writeAdminFallbackServerConfig(t, filepath.Join(directory, "Moltnet"), parsed.Host, "admin-secret-value")

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"admin", "agent", "remove", "--agent", "stale-agent",
		}, "test"); err != nil {
			t.Fatalf("run() admin agent remove error = %v", err)
		}
	})

	if !strings.Contains(output, `"kind": "agent"`) {
		t.Fatalf("unexpected admin agent remove output %q", output)
	}
	if len(authHeaders) != 1 || authHeaders[0] != "Bearer admin-secret-value" {
		t.Fatalf("unexpected auth headers %#v", authHeaders)
	}
}

func TestResolveAdminClientWithoutAnyConfigStillErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	err := run(context.Background(), []string{"admin", "agent", "remove", "--agent", "stale-agent"}, "test")
	if err == nil || !strings.Contains(err.Error(), "requires --base-url or --config") {
		t.Fatalf("run() admin agent remove error = %v, want the base-url/config guidance", err)
	}
}

func TestResolveAdminClientExplicitBaseURLSkipsFallback(t *testing.T) {
	// Even with a server config sitting in cwd, an explicit --base-url must
	// win outright and never consult it.
	directory := t.TempDir()
	t.Chdir(directory)
	writeAdminFallbackServerConfig(t, filepath.Join(directory, "Moltnet"), "127.0.0.1:1", "unused-token")

	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_ = json.NewEncoder(w).Encode(protocol.RemoveResult{Removed: true, Kind: "agent", ID: "stale-agent", Mode: "soft"})
	}))
	defer server.Close()

	if err := run(context.Background(), []string{
		"admin", "agent", "remove", "--agent", "stale-agent", "--base-url", server.URL,
	}, "test"); err != nil {
		t.Fatalf("run() admin agent remove error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("expected exactly one request to the explicit --base-url server, got %d", requests)
	}
}
