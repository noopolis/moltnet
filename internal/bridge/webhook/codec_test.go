package webhook

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestWebhookAcceptanceAndRestart(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer webhook-test-key" || r.Header.Get("Idempotency-Key") != "msg-1" {
			t.Error("incorrect webhook request")
		}
		data, _ := io.ReadAll(r.Body)
		if strings.Contains(string(data), "private message body") || strings.Contains(string(data), "webhook-test-key") {
			t.Error("message or credential leaked into payload")
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"message":"ack must not be published"}`))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "receipts.json")
	c, err := newCodec(path, "webhook-test-key", 2)
	if err != nil {
		t.Fatal(err)
	}
	config := bridgeconfig.Config{Agent: bridgeconfig.AgentConfig{ID: "grok-bot"}, Runtime: bridgeconfig.RuntimeConfig{ControlURL: server.URL}}
	delivery := loop.ControlDelivery{EventID: "msg-1", Message: "private message body", Target: protocol.Target{Kind: "room", RoomID: "general"}}
	bootstrap, err := c.EncodeRequest(context.Background(), config, loop.ControlDelivery{})
	if err != nil || bootstrap != nil {
		t.Fatal("bootstrap triggered a model run")
	}
	request, err := c.EncodeRequest(context.Background(), config, delivery)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	result, err := c.DecodeResponse(config, delivery, response)
	if err != nil {
		t.Fatal(err)
	}
	if result.Publish || result.Text != "" {
		t.Fatal("webhook ack became an AI reply")
	}
	restarted, err := newCodec(path, "webhook-test-key", 2)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := restarted.EncodeRequest(context.Background(), config, delivery)
	if err != nil || duplicate != nil || calls != 1 {
		t.Fatal("accepted event replayed")
	}
	delivery.EventID = "msg-2"
	if _, err := restarted.EncodeRequest(context.Background(), config, delivery); err != nil {
		t.Fatal(err)
	}
	// An attempted request consumes budget even if no acknowledgement arrives.
	delivery.EventID = "msg-3"
	if _, err := restarted.EncodeRequest(context.Background(), config, delivery); err == nil {
		t.Fatal("daily cap was bypassed")
	}
	restarted, err = newCodec(path, "webhook-test-key", 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.EncodeRequest(context.Background(), config, delivery); err == nil {
		t.Fatal("restart reset daily budget")
	}
	restarted.state.Day = "2000-01-01"
	if _, err := restarted.EncodeRequest(context.Background(), config, delivery); err != nil {
		t.Fatal("new day did not reset budget:", err)
	}
}

func TestWebhookRejectsFailedAcceptance(t *testing.T) {
	c, err := newCodec(filepath.Join(t.TempDir(), "receipts.json"), "test", 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.DecodeResponse(bridgeconfig.Config{}, loop.ControlDelivery{EventID: "bad"}, &http.Response{StatusCode: 401, Status: "401 Unauthorized"})
	if err == nil || c.state.Accepted["bad"] {
		t.Fatal("failed request marked accepted")
	}
}
