package acp

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/noopolis/moltnet/internal/bridge/clisession"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

const protocolVersion = 1

type executor struct {
	lifetime  context.Context
	spec      launchSpec
	startGate chan struct{} // Guards current and closed across initialization.
	current   *agent
	closed    bool
}

type agent struct {
	*connection
	loadSession bool
	mu          sync.Mutex
	sessions    map[string]string
	keysByID    map[string]string
}

type sessionReference struct {
	SessionID string `json:"sessionId"`
}

type sessionSetup struct {
	SessionID  string `json:"sessionId,omitempty"`
	Directory  string `json:"cwd"`
	MCPServers []any  `json:"mcpServers"`
}

type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type sessionPrompt struct {
	sessionReference
	Prompt []textBlock `json:"prompt"`
}

func (*executor) Name() string {
	return bridgeconfig.RuntimeACP
}

func (e *executor) Turn(ctx context.Context, delivery clisession.Delivery) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, clisession.DefaultCommandTimeout)
	defer cancel()

	agent, err := e.getAgent(ctx)
	if err != nil {
		return "", err
	}
	sessionID, err := agent.session(ctx, e.spec.dir, delivery)
	if err != nil {
		return "", err
	}

	params := sessionPrompt{
		sessionReference: sessionReference{SessionID: sessionID},
		Prompt:           []textBlock{{Type: "text", Text: delivery.Prompt}},
	}
	var result struct {
		StopReason string `json:"stopReason"`
	}
	if err := agent.call(ctx, "session/prompt", params, &result, sessionID); err != nil {
		return "", err
	}
	switch result.StopReason {
	case "end_turn", "max_tokens", "max_turn_requests", "refusal":
	case "cancelled":
		return "", fmt.Errorf("ACP agent cancelled the prompt")
	default:
		return "", fmt.Errorf("ACP agent returned invalid stop reason %q", result.StopReason)
	}
	if agent.loadSession {
		return sessionID, nil
	}
	return "", nil
}

func (e *executor) getAgent(ctx context.Context) (*agent, error) {
	select {
	case e.startGate <- struct{}{}:
		defer func() { <-e.startGate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e.closed {
		return nil, fmt.Errorf("ACP executor closed")
	}
	if e.current != nil {
		if !e.current.isClosed() {
			return e.current, nil
		}
		e.current.close()
		e.current = nil
	}

	conn, err := launch(e.lifetime, e.spec)
	if err != nil {
		return nil, err
	}
	params := struct {
		ProtocolVersion    int               `json:"protocolVersion"`
		ClientCapabilities struct{}          `json:"clientCapabilities"`
		ClientInfo         map[string]string `json:"clientInfo"`
	}{
		ProtocolVersion: protocolVersion,
		ClientInfo: map[string]string{
			"name": "moltnet", "title": "Moltnet", "version": "1",
		},
	}
	var result struct {
		ProtocolVersion   int `json:"protocolVersion"`
		AgentCapabilities struct {
			LoadSession bool `json:"loadSession"`
		} `json:"agentCapabilities"`
	}
	if err := conn.call(ctx, "initialize", params, &result, ""); err != nil {
		conn.close()
		return nil, err
	}
	if result.ProtocolVersion != protocolVersion {
		conn.close()
		return nil, fmt.Errorf("ACP agent negotiated protocol version %d, want %d", result.ProtocolVersion, protocolVersion)
	}

	e.current = &agent{
		connection:  conn,
		loadSession: result.AgentCapabilities.LoadSession,
		sessions:    make(map[string]string),
		keysByID:    make(map[string]string),
	}
	return e.current, nil
}

func (e *executor) close() {
	e.startGate <- struct{}{}
	defer func() { <-e.startGate }()
	e.closed = true
	if e.current != nil {
		e.current.close()
		e.current = nil
	}
}

func (a *agent) session(ctx context.Context, dir string, delivery clisession.Delivery) (string, error) {
	a.mu.Lock()
	id := a.sessions[delivery.ContextKey]
	a.mu.Unlock()
	if id != "" {
		return id, nil
	}

	setup := sessionSetup{Directory: dir, MCPServers: []any{}}
	if a.loadSession && delivery.RuntimeSessionID != "" {
		setup.SessionID = delivery.RuntimeSessionID
		if err := a.call(ctx, "session/load", setup, nil, ""); err != nil {
			return "", err
		}
		id = setup.SessionID
	} else {
		var result sessionReference
		if err := a.call(ctx, "session/new", setup, &result, ""); err != nil {
			return "", err
		}
		id = result.SessionID
		if id == "" || id != strings.TrimSpace(id) {
			return "", fmt.Errorf("ACP agent returned an empty or untrimmed session ID")
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if key, exists := a.keysByID[id]; exists && key != delivery.ContextKey {
		return "", fmt.Errorf("ACP agent returned a session ID used by another conversation")
	}
	a.sessions[delivery.ContextKey] = id
	a.keysByID[id] = delivery.ContextKey
	return id, nil
}
