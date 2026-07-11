package transport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestReportWakeFailedRejectsUnauthorizedAgent(t *testing.T) {
	t.Parallel()

	policy := mustBearerPolicy(t, authn.TokenConfig{
		ID:     "researcher-token",
		Value:  "researcher-secret",
		Scopes: []authn.Scope{authn.ScopeWrite},
		Agents: []string{"researcher"},
	})
	service := &fakeService{network: protocol.Network{ID: "local"}}
	handler := NewHTTPHandler(service, policy)

	request := httptest.NewRequest(http.MethodPost, "/v1/agents/wake-failed", strings.NewReader(`{
		"agent_id":"someone-else",
		"event":{"id":"evt_1","type":"message.created","message":{"id":"msg_1"}}
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer researcher-secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("unexpected status %d, body %s", response.Code, response.Body.String())
	}
	if len(service.failedWakes) != 0 {
		t.Fatalf("expected no wake failure recorded, got %#v", service.failedWakes)
	}
}

func TestReportWakeFailedAllowsScopedAgent(t *testing.T) {
	t.Parallel()

	policy := mustBearerPolicy(t, authn.TokenConfig{
		ID:     "researcher-token",
		Value:  "researcher-secret",
		Scopes: []authn.Scope{authn.ScopeWrite},
		Agents: []string{"researcher"},
	})
	service := &fakeService{network: protocol.Network{ID: "local"}}
	handler := NewHTTPHandler(service, policy)

	request := httptest.NewRequest(http.MethodPost, "/v1/agents/wake-failed", strings.NewReader(`{
		"agent_id":"researcher",
		"event":{"id":"evt_1","type":"message.created","message":{"id":"msg_1"}},
		"attempts":3,
		"classification":"retry_exhausted"
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer researcher-secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("unexpected status %d, body %s", response.Code, response.Body.String())
	}
	if len(service.failedWakes) != 1 || service.failedWakes[0].ID != "evt_1" {
		t.Fatalf("unexpected recorded wake failure %#v", service.failedWakes)
	}
	if len(service.failedWakeDetails) != 1 || service.failedWakeDetails[0].Classification != protocol.WakeFailureClassificationRetryExhausted {
		t.Fatalf("unexpected wake failure details %#v", service.failedWakeDetails)
	}
}
