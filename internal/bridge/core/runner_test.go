package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type fakeAdapter struct {
	name   string
	config bridgeconfig.Config
	err    error
	called bool
}

func (f *fakeAdapter) Name() string { return f.name }
func (f *fakeAdapter) Run(_ context.Context, config bridgeconfig.Config) error {
	f.called = true
	f.config = config
	return f.err
}

func TestSelectAdapter(t *testing.T) {
	t.Parallel()

	for _, runtimeKind := range []string{
		bridgeconfig.RuntimeTinyClaw,
		bridgeconfig.RuntimeOpenClaw,
		bridgeconfig.RuntimePicoClaw,
		bridgeconfig.RuntimePi,
		bridgeconfig.RuntimeClaudeCode,
		bridgeconfig.RuntimeCodex,
	} {
		runtimeKind := runtimeKind
		t.Run(runtimeKind, func(t *testing.T) {
			t.Parallel()

			adapter, err := selectAdapter(runtimeKind)
			if err != nil {
				t.Fatalf("selectAdapter() error = %v", err)
			}
			if adapter.Name() != runtimeKind {
				t.Fatalf("unexpected adapter name %q", adapter.Name())
			}
		})
	}

	if _, err := selectAdapter("weird"); err == nil {
		t.Fatal("expected unsupported adapter error")
	}
}

func TestRunnerRun(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/network" {
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
		writeNetwork(t, response, compatibleNetwork("local"))
	}))
	t.Cleanup(server.Close)

	config := bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: server.URL, NetworkID: "local"},
	}

	adapter := &fakeAdapter{name: "fake"}
	runner := &Runner{config: config, adapter: adapter, preflight: Preflight}

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !adapter.called {
		t.Fatal("expected adapter to be called")
	}
	if adapter.config.Agent.ID != "researcher" {
		t.Fatalf("unexpected config %#v", adapter.config)
	}

	expected := errors.New("boom")
	failing := &Runner{config: config, adapter: &fakeAdapter{name: "fake", err: expected}, preflight: Preflight}
	if err := failing.Run(context.Background()); !errors.Is(err, expected) {
		t.Fatalf("expected %v, got %v", expected, err)
	}
}

func TestRunnerPreflightRejectsIncompatibleNetworkBeforeAdapter(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		writeNetwork(t, response, protocol.Network{
			ID: "local",
			Protocols: protocol.NetworkProtocols{
				HTTP:   []string{protocol.HTTPProtocolV1},
				Attach: []string{"moltnet.attach.v0"},
			},
			Capabilities: protocol.NetworkCapabilities{
				AttachmentProtocol: "sse",
				DirectMessages:     true,
				MessagePagination:  "cursor",
			},
		})
	}))
	t.Cleanup(server.Close)

	adapter := &fakeAdapter{name: "fake"}
	runner := &Runner{
		config: bridgeconfig.Config{
			Agent:   bridgeconfig.AgentConfig{ID: "researcher"},
			Moltnet: bridgeconfig.MoltnetConfig{BaseURL: server.URL, NetworkID: "local"},
		},
		adapter:   adapter,
		preflight: Preflight,
	}

	err := runner.Run(context.Background())
	if err == nil {
		t.Fatal("expected compatibility error")
	}
	if adapter.called {
		t.Fatal("adapter started after deterministic preflight failure")
	}
	text := err.Error()
	if !strings.Contains(text, protocol.AttachmentProtocolV1) ||
		!strings.Contains(text, "attachment_protocol=websocket") {
		t.Fatalf("unexpected error %q", text)
	}
}

func TestPreflightUsesConfiguredMoltnetAuth(t *testing.T) {
	t.Parallel()

	tokenPath := filepath.Join(t.TempDir(), "agent.token")
	if err := os.WriteFile(tokenPath, []byte("secret\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	authCh := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		authCh <- request.Header.Get("Authorization")
		writeNetwork(t, response, compatibleNetwork("local"))
	}))
	t.Cleanup(server.Close)

	err := Preflight(context.Background(), bridgeconfig.Config{
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   server.URL + "/",
			NetworkID: "local",
			TokenPath: tokenPath,
		},
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if gotAuth := <-authCh; gotAuth != "Bearer secret" {
		t.Fatalf("Authorization = %q, want bearer token", gotAuth)
	}
}

func TestValidateNetworkCompatibility(t *testing.T) {
	t.Parallel()

	config := bridgeconfig.Config{
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: "http://moltnet", NetworkID: "local"},
	}
	if err := ValidateNetworkCompatibility(config, compatibleNetwork("local")); err != nil {
		t.Fatalf("ValidateNetworkCompatibility() error = %v", err)
	}

	legacy := protocol.Network{
		ID: "local",
		Capabilities: protocol.NetworkCapabilities{
			AttachmentProtocol: "websocket",
			DirectMessages:     true,
			MessagePagination:  "cursor",
		},
	}
	if err := ValidateNetworkCompatibility(config, legacy); err != nil {
		t.Fatalf("expected legacy capability hints to pass, got %v", err)
	}

	missingLegacyHint := legacy
	missingLegacyHint.Capabilities.MessagePagination = ""
	err := ValidateNetworkCompatibility(config, missingLegacyHint)
	if err == nil || !strings.Contains(err.Error(), "legacy hint message_pagination=cursor is missing") {
		t.Fatalf("expected missing legacy hint error, got %v", err)
	}

	partialModern := legacy
	partialModern.Protocols.HTTP = []string{protocol.HTTPProtocolV1}
	err = ValidateNetworkCompatibility(config, partialModern)
	if err == nil || !strings.Contains(err.Error(), "protocols.attach") || !strings.Contains(err.Error(), "reported none") {
		t.Fatalf("expected partial modern protocol response to fail, got %v", err)
	}

	dmConfig := config
	dmConfig.DMs = &bridgeconfig.DMConfig{Enabled: true}
	dmDisabled := compatibleNetwork("local")
	dmDisabled.Capabilities.DirectMessages = false
	err = ValidateNetworkCompatibility(dmConfig, dmDisabled)
	if err == nil || !strings.Contains(err.Error(), "direct_messages=true") {
		t.Fatalf("expected direct message capability error, got %v", err)
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	config := bridgeconfig.Config{
		Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeOpenClaw},
	}

	runner, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if runner.adapter == nil {
		t.Fatal("expected adapter")
	}
	if runner.config.Runtime.GatewayURL != bridgeconfig.DefaultOpenClawGatewayURL {
		t.Fatalf("expected normalized runtime config, got %#v", runner.config.Runtime)
	}

	config.Runtime.Kind = "bad"
	if _, err := New(config); err == nil {
		t.Fatal("expected unsupported runtime error")
	}
}

func compatibleNetwork(networkID string) protocol.Network {
	return protocol.Network{
		ID: networkID,
		Protocols: protocol.NetworkProtocols{
			HTTP:   []string{protocol.HTTPProtocolV1},
			Attach: []string{protocol.AttachmentProtocolV1},
		},
		Capabilities: protocol.NetworkCapabilities{
			AttachmentProtocol: "websocket",
			DirectMessages:     true,
			MessagePagination:  "cursor",
		},
	}
}

func writeNetwork(t *testing.T, response http.ResponseWriter, network protocol.Network) {
	t.Helper()

	response.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(response).Encode(network); err != nil {
		t.Fatalf("encode network: %v", err)
	}
}
