package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// TestRunStatusReportsNetworkParticipantsPeersAndWarnings is PLAN.md 7C.1's
// required happy-path test: against a live server, `status` composes
// /healthz, /v1/network (with its embedded NetworkWarning codes), /v1/agents,
// and /v1/pairings into one report.
func TestRunStatusReportsNetworkParticipantsPeersAndWarnings(t *testing.T) {
	workspace := t.TempDir()
	writeClientConfigFixture(t, workspace, clientconfig.Config{
		Version: clientconfig.VersionV1,
		Attachments: []clientconfig.AttachmentConfig{
			{
				Auth:      clientconfig.AuthConfig{Mode: "none"},
				BaseURL:   "http://127.0.0.1:8787",
				MemberID:  "alpha",
				NetworkID: "local_lab",
				Rooms:     []bridgeconfig.RoomBinding{{ID: "general"}},
			},
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/healthz":
			_ = json.NewEncoder(response).Encode(map[string]string{"status": "ok"})
		case "/v1/network":
			_ = json.NewEncoder(response).Encode(protocol.Network{
				ID:      "local_lab",
				Name:    "Local Lab",
				Version: "test",
				Warnings: []protocol.NetworkWarning{
					{Severity: "warning", Code: "pairings.degraded", Message: "1 pairing degraded."},
				},
			})
		case "/v1/agents":
			_ = json.NewEncoder(response).Encode(protocol.AgentPage{
				Agents: []protocol.AgentSummary{{ID: "alpha", NetworkID: "local_lab", Connected: true}},
			})
		case "/v1/pairings":
			_ = json.NewEncoder(response).Encode(protocol.PairingPage{
				Pairings: []protocol.Pairing{{ID: "pair_1", RemoteNetworkID: "friend"}},
			})
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	rewriteClientConfigBaseURL(t, workspace, server.URL)

	cwd := mustGetwd(t)
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"status"}, "test"); err != nil {
			t.Fatalf("run() status error = %v", err)
		}
	})

	for _, want := range []string{
		`"running": true`,
		`"Local Lab"`,
		`"pairings.degraded"`,
		`"alpha"`,
		`"pair_1"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("status output missing %q: %s", want, output)
		}
	}
	if strings.Contains(output, `"metrics"`) {
		t.Fatalf("status output without --verbose must not include metrics: %s", output)
	}
}

// TestRunStatusVerboseAddsMetrics checks --verbose's one additional leg: an
// admin-scoped credential fetches and parses /metrics into the summary view.
func TestRunStatusVerboseAddsMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/healthz":
			_ = json.NewEncoder(response).Encode(map[string]string{"status": "ok"})
		case "/v1/network":
			_ = json.NewEncoder(response).Encode(protocol.Network{ID: "fixture-net"})
		case "/v1/agents":
			_ = json.NewEncoder(response).Encode(protocol.AgentPage{})
		case "/v1/pairings":
			_ = json.NewEncoder(response).Encode(protocol.PairingPage{})
		case "/metrics":
			response.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
			_, _ = response.Write([]byte("# HELP moltnet_active_attachments x\n# TYPE moltnet_active_attachments gauge\nmoltnet_active_attachments 3\nmoltnet_store_health 1\nmoltnet_http_requests_total{method=\"GET\",route=\"/v1/network\",status=\"200\"} 5\n"))
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}

	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("HOME", t.TempDir())
	writeOperatorFallbackServerConfig(t, directory+"/Moltnet", parsed.Host, []operatorFallbackToken{
		{id: "operator", value: "operator-secret", scopes: []string{"observe", "write", "admin", "pair"}},
	})

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"status", "--verbose"}, "test"); err != nil {
			t.Fatalf("run() status --verbose error = %v", err)
		}
	})

	for _, want := range []string{
		`"active_attachments": 3`,
		`"store_healthy": true`,
		`"http_requests_total": 5`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("status --verbose output missing %q: %s", want, output)
		}
	}
}

// TestRunStatusSurfacesTruncatedParticipantsAndPeers is F6's fix: status
// calls ListAgents/ListPairings with a zero PageRequest (the server's own
// default page size applies, internal/transport/helpers.go's readLimit),
// never paging status to exhaustion, so it must surface PageInfo.HasMore
// instead of silently reporting an incomplete "who is in it" as if it were
// the whole roster.
func TestRunStatusSurfacesTruncatedParticipantsAndPeers(t *testing.T) {
	workspace := t.TempDir()
	writeClientConfigFixture(t, workspace, clientconfig.Config{
		Version: clientconfig.VersionV1,
		Attachments: []clientconfig.AttachmentConfig{
			{
				Auth:      clientconfig.AuthConfig{Mode: "none"},
				BaseURL:   "http://127.0.0.1:8787",
				MemberID:  "alpha",
				NetworkID: "local_lab",
			},
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/healthz":
			_ = json.NewEncoder(response).Encode(map[string]string{"status": "ok"})
		case "/v1/network":
			_ = json.NewEncoder(response).Encode(protocol.Network{ID: "local_lab"})
		case "/v1/agents":
			_ = json.NewEncoder(response).Encode(protocol.AgentPage{
				Agents: []protocol.AgentSummary{{ID: "alpha", NetworkID: "local_lab"}},
				Page:   protocol.PageInfo{HasMore: true},
			})
		case "/v1/pairings":
			_ = json.NewEncoder(response).Encode(protocol.PairingPage{
				Pairings: []protocol.Pairing{{ID: "pair_1", RemoteNetworkID: "friend"}},
				Page:     protocol.PageInfo{HasMore: true},
			})
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	rewriteClientConfigBaseURL(t, workspace, server.URL)

	cwd := mustGetwd(t)
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"status"}, "test"); err != nil {
			t.Fatalf("run() status error = %v", err)
		}
	})

	for _, want := range []string{
		`"participants_truncated": true`,
		`"peers_truncated": true`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("status output missing %q: %s", want, output)
		}
	}
}

// TestRunStatusDaemonDownReportsCleanly is PLAN.md 7C.1's required "the
// daemon is not running" test: status must report that clearly, with no
// error return and no dumped transport error text (e.g. "dial tcp",
// "connection refused").
func TestRunStatusDaemonDownReportsCleanly(t *testing.T) {
	workspace := t.TempDir()
	// A server that is immediately closed still hands out a real,
	// syntactically valid URL that nothing is listening on -- exactly what
	// a stopped/never-started Moltnet daemon looks like from the outside.
	deadServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadServer.Close()

	writeClientConfigFixture(t, workspace, clientconfig.Config{
		Version: clientconfig.VersionV1,
		Attachments: []clientconfig.AttachmentConfig{
			{
				Auth:      clientconfig.AuthConfig{Mode: "none"},
				BaseURL:   deadServer.URL,
				MemberID:  "alpha",
				NetworkID: "local_lab",
			},
		},
	})

	cwd := mustGetwd(t)
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"status"}, "test"); err != nil {
			t.Fatalf("run() status (daemon down) error = %v, want nil (a clean report, not a command failure)", err)
		}
	})

	if !strings.Contains(output, `"running": false`) {
		t.Fatalf("status output = %q, want running: false", output)
	}
	for _, leaked := range []string{"dial tcp", "connection refused", "connect:"} {
		if strings.Contains(output, leaked) {
			t.Fatalf("status output leaked a raw transport error (%q): %s", leaked, output)
		}
	}
}

// TestProbeStatusHealthDistinguishesTimeoutFromRefusal is F3's required
// regression (review round 2, confirmed live): probeStatusHealth used to
// collapse every Do() error into reachable == false, exactly like a genuine
// connection-refused dead server. pair_revoke.go's printPairRevokeAftercare
// trusts reachable == false as "no server running" and skips its "still
// running, still trusts the revoked credential" warning -- so a hung or
// overloaded server (which answers neither with a refusal nor within
// statusHealthProbeTimeout) produced a false-negative "definitely not
// running" and let a revoke print a clean success line while the peer was
// still fully authenticated. A clean refusal must still report
// reachable == false; anything else (timeout included) must report
// reachable == true (just unhealthy, since it never actually answered).
func TestProbeStatusHealthDistinguishesTimeoutFromRefusal(t *testing.T) {
	t.Run("connection refused reports unreachable", func(t *testing.T) {
		deadServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		deadServer.Close()

		reachable, healthy := probeStatusHealth(context.Background(), deadServer.URL)
		if reachable {
			t.Fatalf("probeStatusHealth() reachable = true for a connection-refused dead server, want false")
		}
		if healthy {
			t.Fatalf("probeStatusHealth() healthy = true for an unreachable server, want false")
		}
	})

	t.Run("timeout reports reachable but unhealthy", func(t *testing.T) {
		hung := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			<-hung
		}))
		// LIFO order matters here: server.Close() blocks until its handler
		// goroutine returns, and that goroutine is parked on <-hung, so hung
		// must close before Close() is called or the test deadlocks.
		defer server.Close()
		defer close(hung)

		// probeStatusHealth derives its own deadline from
		// statusHealthProbeTimeout (3s) via context.WithTimeout(ctx, ...),
		// which always takes the earlier of the two deadlines -- passing an
		// already-short-deadlined ctx here triggers the same timeout path
		// without the test waiting 3 real seconds for it.
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		reachable, healthy := probeStatusHealth(ctx, server.URL)
		if !reachable {
			t.Fatalf("probeStatusHealth() reachable = false for a hung server that timed out, want true (it may still be running)")
		}
		if healthy {
			t.Fatalf("probeStatusHealth() healthy = true for a timed-out probe, want false")
		}
	})
}

// TestRunStatusNoCredentialDegradesGracefully is PLAN.md 7C.1's required "no
// credential resolves" test: on a machine with no client config and no local
// server config at all, status must fail with one clear, actionable message
// -- never a panic or a raw type dump.
func TestRunStatusNoCredentialDegradesGracefully(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("HOME", t.TempDir())

	err := run(context.Background(), []string{"status"}, "test")
	if err == nil {
		t.Fatal("run() status with no config anywhere = nil error, want a clear refusal")
	}
	if !strings.Contains(err.Error(), "moltnet init") {
		t.Fatalf("run() status error = %q, want it to name `moltnet init` as the remedy", err.Error())
	}
}
