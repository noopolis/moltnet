package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/automation"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/rooms"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestAutomationHTTPAndSQLiteRoundTrip(t *testing.T) {
	db, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	manager, err := automation.New(filepath.Join(t.TempDir(), "automation.json"), "local")
	if err != nil {
		t.Fatal(err)
	}
	service := rooms.NewService(rooms.ServiceConfig{NetworkID: "local", AllowHumanIngress: true, Store: db, Messages: db, Broker: events.NewBroker(db), Automation: manager})
	defer service.Close()
	owner := authn.TokenConfig{ID: "operator", Value: "owner-test", Scopes: []authn.Scope{authn.ScopeObserve, authn.ScopeWrite, authn.ScopeAdmin}}
	observer := authn.TokenConfig{ID: "observer", Value: "observer-test", Scopes: []authn.Scope{authn.ScopeObserve}}
	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeOpen, PublicRead: true, AgentRegistration: authn.AgentRegistrationOpen, Tokens: []authn.TokenConfig{owner, observer}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := authn.WithClaims(authn.WithMode(context.Background(), authn.ModeOpen), authn.NewStaticClaims(owner))
	_, err = service.CreateRoomContext(ctx, protocol.CreateRoomRequest{ID: "general", Visibility: "public", WritePolicy: "members", Members: []string{"codex", "grok"}})
	if err != nil {
		t.Fatal(err)
	}
	tokens := map[string]string{}
	for _, id := range []string{"codex", "grok"} {
		result, err := service.RegisterAgentContext(authn.WithMode(context.Background(), authn.ModeOpen), protocol.RegisterAgentRequest{RequestedAgentID: id, Name: id})
		if err != nil {
			t.Fatal(err)
		}
		tokens[id] = result.AgentToken
		service.AgentConnected(context.Background(), protocol.Actor{Type: "agent", ID: id, NetworkID: "local"})
	}
	handler := NewHTTPHandler(service, policy)
	call := func(method, path, token, origin, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:8787"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		} else if origin != "" {
			r.AddCookie(&http.Cookie{Name: authn.CookieName, Value: owner.Value})
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	base := "/v1/rooms/general/automation"
	for _, token := range []string{"", "observer-test", tokens["codex"]} {
		w := call("POST", base+"/discussion", token, "", `{"topic":"test","participants":["codex","grok"],"max_rounds":1}`)
		if w.Code < 400 {
			t.Fatalf("non-owner started discussion: %d", w.Code)
		}
	}
	w := call("POST", base+"/stop", "", "https://evil.example", `{}`)
	if w.Code != 403 {
		t.Fatal("cross-origin owner cookie accepted", w.Code)
	}
	w = call("POST", base+"/discussion", "owner-test", "", `{"topic":"test","participants":["codex","grok"],"max_rounds":1}`)
	if w.Code != 201 {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	var view protocol.RoomAutomation
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	d := view.Discussion
	for turn, id := range []string{"codex", "grok"} {
		page, err := service.ListRoomMessagesContext(ctx, "general", protocol.PageRequest{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		last := page.Messages[len(page.Messages)-1]
		if last.Wake == nil || len(last.Wake.Recipients) != 1 || last.Wake.Recipients[0] != id {
			t.Fatal("SQLite dropped server wake metadata", last)
		}
		query := fmt.Sprintf("%s/wake?message_id=%s&agent_id=%s", base, last.ID, id)
		if w := call("GET", query, tokens[id], "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"allowed":true`) {
			t.Fatal("wake not eligible", w.Code, w.Body.String())
		}
		payload := protocol.SendMessageRequest{ID: fmt.Sprint("reply", turn), From: protocol.Actor{Type: "agent", ID: id}, Target: last.Target, ReplyTo: last.ID, DiscussionID: d.ID, Parts: []protocol.Part{{Kind: "text", Text: "my contribution"}}}
		body, _ := json.Marshal(payload)
		w := call("POST", "/v1/messages", tokens[id], "", string(body))
		if w.Code != 202 {
			t.Fatalf("reply: %d %s", w.Code, w.Body.String())
		}
	}
	w = call("GET", base, "owner-test", "", "")
	_ = json.Unmarshal(w.Body.Bytes(), &view)
	if view.Discussion.Active || view.Discussion.CompletedTurns != 2 {
		t.Fatal("round limit failed", w.Body.String())
	}
	page, err := service.ListRoomMessagesContext(ctx, "general", protocol.PageRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 3 || len(page.Messages[2].Wake.Recipients) != 0 {
		t.Fatal("unexpected messages or next wake", page)
	}
	// Stopped/outdated events must not wake a model after reconnect.
	query := fmt.Sprintf("%s/wake?message_id=%s&agent_id=codex", base, page.Messages[0].ID)
	w = call("GET", query, tokens["codex"], "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"allowed":false`) {
		t.Fatal("stale wake admitted", w.Body.String())
	}
}
