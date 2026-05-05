package node

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/nodeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type fakeRunner struct {
	run func(ctx context.Context) error
}

func (f *fakeRunner) Run(ctx context.Context) error {
	return f.run(ctx)
}

func noOpPreflight(_ context.Context, _ []bridgeconfig.Config) error {
	return nil
}

func TestNew(t *testing.T) {
	t.Parallel()

	runner, err := New(nodeconfig.Config{
		Version: nodeconfig.VersionV1,
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   "http://127.0.0.1:8787",
			NetworkID: "local",
		},
		Attachments: []nodeconfig.AttachmentConfig{
			{
				Agent: bridgeconfig.AgentConfig{ID: "alpha"},
				Runtime: bridgeconfig.RuntimeConfig{
					Kind:       bridgeconfig.RuntimeOpenClaw,
					GatewayURL: "ws://127.0.0.1:9100/gateway",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if len(runner.configs) != 1 {
		t.Fatalf("unexpected configs %#v", runner.configs)
	}
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	if _, err := New(nodeconfig.Config{}); err == nil {
		t.Fatal("expected invalid config error")
	}
}

func TestRunnerRunSuccess(t *testing.T) {
	t.Parallel()

	runner := &Runner{
		configs: []bridgeconfig.Config{
			{Agent: bridgeconfig.AgentConfig{ID: "alpha"}, Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeOpenClaw}},
			{Agent: bridgeconfig.AgentConfig{ID: "beta"}, Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimePicoClaw}},
		},
		newRunner: func(config bridgeconfig.Config) (attachmentRunner, error) {
			return &fakeRunner{
				run: func(ctx context.Context) error {
					<-ctx.Done()
					return nil
				},
			}, nil
		},
		preflight: noOpPreflight,
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunnerRunFailure(t *testing.T) {
	t.Parallel()

	expected := errors.New("boom")
	runner := &Runner{
		configs: []bridgeconfig.Config{
			{Agent: bridgeconfig.AgentConfig{ID: "alpha"}, Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeOpenClaw}},
		},
		newRunner: func(config bridgeconfig.Config) (attachmentRunner, error) {
			return &fakeRunner{
				run: func(ctx context.Context) error {
					return expected
				},
			}, nil
		},
		preflight: noOpPreflight,
	}

	if err := runner.Run(context.Background()); !errors.Is(err, expected) {
		t.Fatalf("expected %v, got %v", expected, err)
	}
}

func TestRunnerRunFactoryFailure(t *testing.T) {
	t.Parallel()

	expected := errors.New("factory")
	runner := &Runner{
		configs: []bridgeconfig.Config{
			{Agent: bridgeconfig.AgentConfig{ID: "alpha"}, Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeOpenClaw}},
		},
		newRunner: func(config bridgeconfig.Config) (attachmentRunner, error) {
			return nil, expected
		},
		preflight: noOpPreflight,
	}

	if err := runner.Run(context.Background()); !errors.Is(err, expected) {
		t.Fatalf("expected %v, got %v", expected, err)
	}
}

func TestRunnerRunWithoutAttachments(t *testing.T) {
	t.Parallel()

	runner := &Runner{}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunnerRunPreflightFailureDoesNotStartRunners(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		writeNetwork(t, response, protocol.Network{
			ID: "local",
			Protocols: protocol.NetworkProtocols{
				HTTP:   []string{protocol.HTTPProtocolV1},
				Attach: []string{"moltnet.attach.v0"},
			},
			Capabilities: protocol.NetworkCapabilities{
				AttachmentProtocol: "websocket",
				DirectMessages:     true,
				MessagePagination:  "cursor",
			},
		})
	}))
	t.Cleanup(server.Close)

	factoryCalled := false
	runCalled := false
	runner := &Runner{
		configs: []bridgeconfig.Config{{
			Agent:   bridgeconfig.AgentConfig{ID: "alpha"},
			Moltnet: bridgeconfig.MoltnetConfig{BaseURL: server.URL, NetworkID: "local"},
		}},
		newRunner: func(config bridgeconfig.Config) (attachmentRunner, error) {
			factoryCalled = true
			return &fakeRunner{run: func(ctx context.Context) error {
				runCalled = true
				return nil
			}}, nil
		},
	}

	err := runner.Run(context.Background())
	if err == nil {
		t.Fatal("expected compatibility error")
	}
	if factoryCalled || runCalled {
		t.Fatalf("runner started despite preflight failure; factory=%v run=%v", factoryCalled, runCalled)
	}
	if !strings.Contains(err.Error(), protocol.AttachmentProtocolV1) {
		t.Fatalf("unexpected error %q", err.Error())
	}
}

func TestRunnerRunAggregatesPreflightErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		network := compatibleNetwork("local")
		network.Capabilities.DirectMessages = false
		writeNetwork(t, response, network)
	}))
	t.Cleanup(server.Close)

	factoryCalled := false
	runner := &Runner{
		configs: []bridgeconfig.Config{
			{
				Agent:   bridgeconfig.AgentConfig{ID: "alpha"},
				Moltnet: bridgeconfig.MoltnetConfig{BaseURL: server.URL, NetworkID: "remote"},
			},
			{
				Agent:   bridgeconfig.AgentConfig{ID: "beta"},
				Moltnet: bridgeconfig.MoltnetConfig{BaseURL: server.URL, NetworkID: "local"},
				DMs:     &bridgeconfig.DMConfig{Enabled: true},
			},
		},
		newRunner: func(config bridgeconfig.Config) (attachmentRunner, error) {
			factoryCalled = true
			return &fakeRunner{run: func(ctx context.Context) error { return nil }}, nil
		},
	}

	err := runner.Run(context.Background())
	if err == nil {
		t.Fatal("expected aggregated compatibility error")
	}
	if factoryCalled {
		t.Fatal("runner factory was called despite preflight failures")
	}
	text := err.Error()
	for _, want := range []string{
		"attachment alpha",
		"network_id mismatch",
		"attachment beta",
		"direct_messages=true",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("aggregated error missing %q: %s", want, text)
		}
	}
}

func TestPreflightAttachmentsCachesByTokenAndNetwork(t *testing.T) {
	t.Parallel()

	var authMu sync.Mutex
	authCounts := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		auth := request.Header.Get("Authorization")
		authMu.Lock()
		authCounts[auth]++
		authMu.Unlock()
		if auth == "Bearer bad" {
			http.Error(response, "nope", http.StatusForbidden)
			return
		}
		writeNetwork(t, response, compatibleNetwork("local"))
	}))
	t.Cleanup(server.Close)

	err := preflightAttachments(context.Background(), []bridgeconfig.Config{
		{
			Agent:   bridgeconfig.AgentConfig{ID: "alpha"},
			Moltnet: bridgeconfig.MoltnetConfig{BaseURL: server.URL, NetworkID: "local", Token: "good"},
		},
		{
			Agent:   bridgeconfig.AgentConfig{ID: "beta"},
			Moltnet: bridgeconfig.MoltnetConfig{BaseURL: server.URL, NetworkID: "local", Token: "bad"},
		},
		{
			Agent:   bridgeconfig.AgentConfig{ID: "gamma"},
			Moltnet: bridgeconfig.MoltnetConfig{BaseURL: server.URL, NetworkID: "local", Token: "good"},
		},
		{
			Agent:   bridgeconfig.AgentConfig{ID: "delta"},
			Moltnet: bridgeconfig.MoltnetConfig{BaseURL: server.URL, NetworkID: "remote", Token: "good"},
		},
	})
	if err == nil {
		t.Fatal("expected preflight error")
	}

	authMu.Lock()
	goodRequests := authCounts["Bearer good"]
	badRequests := authCounts["Bearer bad"]
	authMu.Unlock()

	if goodRequests != 2 {
		t.Fatalf("expected one good-token request per network, got %d", goodRequests)
	}
	if badRequests != 1 {
		t.Fatalf("expected separate bad-token request, got %d", badRequests)
	}

	text := err.Error()
	for _, want := range []string{"attachment beta", "required scopes", "attachment delta", "network_id mismatch"} {
		if !strings.Contains(text, want) {
			t.Fatalf("preflight error missing %q: %s", want, text)
		}
	}
	for _, unwanted := range []string{"attachment alpha", "attachment gamma"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("preflight error should not include %q: %s", unwanted, text)
		}
	}
}

func TestNewCoreRunner(t *testing.T) {
	t.Parallel()

	runner, err := newCoreRunner(bridgeconfig.Config{
		Agent: bridgeconfig.AgentConfig{ID: "alpha"},
		Moltnet: bridgeconfig.MoltnetConfig{
			BaseURL:   "http://127.0.0.1:8787",
			NetworkID: "local",
		},
		Runtime: bridgeconfig.RuntimeConfig{
			Kind:       bridgeconfig.RuntimeOpenClaw,
			GatewayURL: "ws://127.0.0.1:9100/gateway",
		},
	})
	if err != nil {
		t.Fatalf("newCoreRunner() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("runner.Run() error = %v", err)
	}
}

func TestNewCoreRunnerRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	if _, err := newCoreRunner(bridgeconfig.Config{}); err == nil {
		t.Fatal("expected invalid bridge config error")
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
