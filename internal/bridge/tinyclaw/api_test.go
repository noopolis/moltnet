package tinyclaw

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

func TestAPIClient(t *testing.T) {
	t.Parallel()

	var posted tinyclawMessageRequest
	var ackPath string

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer runtime-secret" {
			t.Fatalf("unexpected auth header %q", request.Header.Get("Authorization"))
		}
		switch request.URL.Path {
		case "/api/message":
			if err := json.NewDecoder(request.Body).Decode(&posted); err != nil {
				t.Fatalf("decode inbound: %v", err)
			}
			response.WriteHeader(http.StatusOK)
		case "/api/responses/pending":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`[{"id":7,"sender":"Writer","senderId":"room:research","message":"done","files":["report.md"]}]`))
		case "/api/responses/7/ack":
			ackPath = request.URL.Path
			response.WriteHeader(http.StatusOK)
		case "/api/echo":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"ok":true}`))
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newAPIClient(bridgeconfig.Config{
		Runtime: bridgeconfig.RuntimeConfig{
			Token:       "runtime-secret",
			InboundURL:  server.URL + "/api/message",
			OutboundURL: server.URL + "/api/responses/pending",
			AckURL:      server.URL + "/api/responses/",
		},
	})

	if err := client.postMessage(context.Background(), tinyclawMessageRequest{
		Message: "hello",
		Agent:   "researcher",
	}); err != nil {
		t.Fatalf("postMessage() error = %v", err)
	}
	if posted.Message != "hello" || posted.Agent != "researcher" {
		t.Fatalf("unexpected posted payload %#v", posted)
	}

	responses, err := client.pendingResponses(context.Background())
	if err != nil {
		t.Fatalf("pendingResponses() error = %v", err)
	}
	if len(responses) != 1 || responses[0].ID.String() != "7" {
		t.Fatalf("unexpected responses %#v", responses)
	}

	if err := client.ackResponse(context.Background(), pendingResponseID("7")); err != nil {
		t.Fatalf("ackResponse() error = %v", err)
	}
	if ackPath != "/api/responses/7/ack" {
		t.Fatalf("unexpected ack path %q", ackPath)
	}

	var out map[string]bool
	if err := client.postJSON(context.Background(), server.URL+"/api/echo", map[string]string{"hello": "world"}, &out); err != nil {
		t.Fatalf("postJSON() error = %v", err)
	}
	if !out["ok"] {
		t.Fatalf("unexpected out payload %#v", out)
	}
}

func TestAPIClientErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasPrefix(request.URL.Path, "/bad"):
			response.WriteHeader(http.StatusBadGateway)
		default:
			_, _ = response.Write([]byte(`not-json`))
		}
	}))
	defer server.Close()

	client := &apiClient{
		httpClient:  &http.Client{},
		inboundURL:  server.URL + "/bad",
		outboundURL: server.URL + "/bad",
		ackURL:      server.URL + "/bad",
	}

	if err := client.postMessage(context.Background(), tinyclawMessageRequest{}); err == nil {
		t.Fatal("expected post message error")
	}
	if _, err := client.pendingResponses(context.Background()); err == nil {
		t.Fatal("expected pending responses error")
	}
	if err := client.ackResponse(context.Background(), pendingResponseID("1")); err == nil {
		t.Fatal("expected ack error")
	}
}

func TestAPIClientDecodeErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/pending":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`not-json`))
		case "/echo":
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`not-json`))
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := &apiClient{
		httpClient:  &http.Client{},
		outboundURL: server.URL + "/pending",
	}

	if _, err := client.pendingResponses(context.Background()); err == nil {
		t.Fatal("expected pending decode error")
	}

	var out map[string]bool
	if err := client.postJSON(context.Background(), server.URL+"/echo", map[string]string{"hello": "world"}, &out); err == nil {
		t.Fatal("expected postJSON decode error")
	}

	if err := client.postJSON(context.Background(), ":", map[string]string{"hello": "world"}, nil); err == nil {
		t.Fatal("expected invalid url error")
	}
}

func TestAPIClientAuthorizeWithoutToken(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "http://example.com", nil)
	client := &apiClient{}
	client.authorize(request)
	if request.Header.Get("Authorization") != "" {
		t.Fatalf("expected empty auth header, got %q", request.Header.Get("Authorization"))
	}
}

func TestPendingResponseIDValidation(t *testing.T) {
	t.Parallel()

	var id pendingResponseID
	if err := json.Unmarshal([]byte(`"valid_7"`), &id); err != nil {
		t.Fatalf("expected valid pending response id, got %v", err)
	}
	if id.String() != "valid_7" {
		t.Fatalf("unexpected pending response id %q", id.String())
	}
	if err := json.Unmarshal([]byte(`"../../evil"`), &id); err == nil {
		t.Fatal("expected invalid pending response id error")
	}

	client := &apiClient{ackURL: "http://example.com/api/responses"}
	if err := client.ackResponse(context.Background(), pendingResponseID("../bad")); err == nil {
		t.Fatal("expected ackResponse() to reject invalid pending response id")
	}
}
