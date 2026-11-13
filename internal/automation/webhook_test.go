package automation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestWebhookBudgetRedactionAndNoRepeat(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("Idempotency-Key") == "" {
			t.Error("missing authentication or idempotency")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if strings.Contains(body["text"].(string), "test-key") {
			t.Error("secret leaked into prompt")
		}
		w.WriteHeader(202)
	}))
	defer server.Close()
	m, _ := New("", "local")
	if err := m.SetWebhook("grok", protocol.WebhookSetting{URL: server.URL, Key: "test-key", Enabled: true, MaxRunsPerDay: 1}); err != nil {
		t.Fatal(err)
	}
	msg := protocol.Message{ID: "msg1", Target: protocol.Target{Kind: "room", RoomID: "general"}, Wake: &protocol.MessageWake{Recipients: []string{"grok"}}}
	if err := m.deliver(context.Background(), "grok", msg, false); err != nil {
		t.Fatal(err)
	}
	if err := m.deliver(context.Background(), "grok", msg, false); err != nil {
		t.Fatal(err)
	}
	msg.ID = "msg2"
	if err := m.deliver(context.Background(), "grok", msg, false); err == nil {
		t.Fatal("daily budget bypassed")
	}
	if calls.Load() != 1 {
		t.Fatal("unexpected runs", calls.Load())
	}
	view, _ := json.Marshal(m.View("general", []string{"grok"}))
	if strings.Contains(string(view), "test-key") || strings.Contains(string(view), server.URL) {
		t.Fatal("public status leaked webhook credentials")
	}
}

func TestWebhookFailuresDisableAndRedirectsNotFollowed(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationCalls.Add(1) }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer redirect.Close()
	m, _ := New("", "local")
	if err := m.SetWebhook("grok", protocol.WebhookSetting{URL: redirect.URL, Key: "private-key", Enabled: true, MaxRunsPerDay: 10}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := m.TestWebhook(context.Background(), "grok"); err == nil || strings.Contains(err.Error(), redirect.URL) {
			t.Fatal("unexpected error", err)
		}
	}
	if m.HasWebhook("grok") || destinationCalls.Load() != 0 {
		t.Fatal("failure disable or redirect boundary failed")
	}
}

func TestWebhookRejectsUnsafeConfiguration(t *testing.T) {
	for _, url := range []string{"http://example.com/hook", "https://user:pass@example.com/hook", "https://example.com/hook?secret=x", "https://example.com/#x", "file:///tmp/hook"} {
		m, _ := New("", "local")
		if err := m.SetWebhook("grok", protocol.WebhookSetting{URL: url, Key: "key", Enabled: true, MaxRunsPerDay: 30}); err == nil {
			t.Fatal("unsafe URL accepted", url)
		}
	}
}
