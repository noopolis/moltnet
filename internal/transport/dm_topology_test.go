package transport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/rooms"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestHTTPDMTopologyConflictIsFixedRedactedClientError(t *testing.T) {
	memory := store.NewMemoryStore()
	service := rooms.NewService(rooms.ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		Store:             memory,
		Messages:          memory,
		Broker:            events.NewBroker(),
	})
	if _, err := service.SendMessage(protocol.SendMessageRequest{
		ID: "msg-private",
		Target: protocol.Target{
			Kind:           protocol.TargetKindDM,
			DMID:           "dm-private",
			ParticipantIDs: []string{"alpha", "beta"},
		},
		From:  protocol.Actor{Type: "human", ID: "alpha"},
		Parts: []protocol.Part{{Kind: protocol.PartKindFile, Filename: "private.txt"}},
	}); err != nil {
		t.Fatalf("seed DM: %v", err)
	}

	policy, err := authn.NewPolicy(authn.Config{
		Mode: authn.ModeBearer,
		Tokens: []authn.TokenConfig{{
			ID:     "gamma-writer",
			Value:  "gamma-secret",
			Scopes: []authn.Scope{authn.ScopeWrite},
			Agents: []string{"gamma"},
		}},
	})
	if err != nil {
		t.Fatalf("NewPolicy(): %v", err)
	}
	handler := NewHTTPHandler(service, policy)
	body := `{"id":"msg-private","target":{"kind":"dm","dm_id":"dm-private","participant_ids":["beta","gamma"]},"from":{"type":"agent","id":"gamma"},"parts":[{"kind":"file","filename":"stolen.txt"}]}`
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer gamma-secret")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-Id", "req_test")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	const expected = "{\"code\":\"unprocessable_entity\",\"error\":\"direct message participant topology is invalid\",\"request_id\":\"req_test\"}\n"
	if response.Code != http.StatusUnprocessableEntity || response.Body.String() != expected {
		t.Fatalf("response status=%d body=%q", response.Code, response.Body.String())
	}
	conversation, ok, err := memory.GetDirectConversationContext(request.Context(), "dm-private")
	if err != nil || !ok || conversation.MessageCount != 1 || len(conversation.ParticipantIDs) != 2 {
		t.Fatalf("conversation changed = %#v, %v, %v", conversation, ok, err)
	}
	messages, err := memory.ListDMMessagesContext(request.Context(), "dm-private", protocol.PageRequest{Limit: 10})
	if err != nil || len(messages.Messages) != 1 || messages.Messages[0].ID != "msg-private" {
		t.Fatalf("messages changed = %#v, %v", messages, err)
	}
	artifacts, err := memory.ListArtifactsContext(request.Context(), protocol.ArtifactFilter{DMID: "dm-private"}, protocol.PageRequest{Limit: 10})
	if err != nil || len(artifacts.Artifacts) != 1 || artifacts.Artifacts[0].Filename != "private.txt" {
		t.Fatalf("artifacts changed = %#v, %v", artifacts, err)
	}
}
