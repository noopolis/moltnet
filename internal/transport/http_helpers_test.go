package transport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestNewHTTPHandlerStreamWithoutFlusher(t *testing.T) {
	t.Parallel()

	service := &fakeService{
		stream: make(chan protocol.Event),
	}
	defer close(service.stream)

	handler := NewHTTPHandler(service, nil)

	request := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil)
	response := &nonFlushingRecorder{header: make(http.Header)}
	handler.ServeHTTP(response, request)

	if response.code != http.StatusInternalServerError {
		t.Fatalf("unexpected status %d", response.code)
	}
}

type nonFlushingRecorder struct {
	header http.Header
	body   strings.Builder
	code   int
}

func (r *nonFlushingRecorder) Header() http.Header { return r.header }
func (r *nonFlushingRecorder) Write(bytes []byte) (int, error) {
	return r.body.Write(bytes)
}
func (r *nonFlushingRecorder) WriteHeader(status int) { r.code = status }

func TestDecodeJSONIgnoresUnknownFields(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodPost, "/v1/rooms", strings.NewReader(`{"id":"x","extra":true}`))
	var payload protocol.CreateRoomRequest
	response := httptest.NewRecorder()
	if err := decodeJSON(response, request, &payload); err != nil {
		t.Fatalf("unexpected decode error: %v", err)
	}
	if payload.ID != "x" {
		t.Fatalf("unexpected payload %#v", payload)
	}
}

func TestDecodeJSONRejectsLargeBodiesAndTrailingData(t *testing.T) {
	t.Parallel()

	largeBody := `{"id":"x","name":"` + strings.Repeat("a", maxJSONBodyBytes) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/v1/rooms", strings.NewReader(largeBody))
	response := httptest.NewRecorder()
	var payload protocol.CreateRoomRequest
	if err := decodeJSON(response, request, &payload); err == nil {
		t.Fatal("expected body size error")
	}

	request = httptest.NewRequest(http.MethodPost, "/v1/rooms", strings.NewReader(`{"id":"x"}{"id":"y"}`))
	response = httptest.NewRecorder()
	err := decodeJSON(response, request, &payload)
	if err == nil {
		t.Fatal("expected trailing data error")
	}
	if !strings.Contains(err.Error(), "single JSON object") {
		t.Fatalf("unexpected trailing data error %v", err)
	}
}

func TestWriteJSON(t *testing.T) {
	t.Parallel()

	response := httptest.NewRecorder()
	writeJSON(response, http.StatusCreated, map[string]string{"status": "ok"})

	if response.Code != http.StatusCreated {
		t.Fatalf("unexpected status %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), `"status":"ok"`) {
		t.Fatalf("unexpected body %s", response.Body.String())
	}
}

func TestReadLastEventID(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil)
	request.Header.Set("Last-Event-ID", "evt_header")
	if got := readLastEventID(request); got != "evt_header" {
		t.Fatalf("unexpected header last event id %q", got)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/events/stream?last_event_id=evt_query", nil)
	if got := readLastEventID(request); got != "evt_query" {
		t.Fatalf("unexpected query last event id %q", got)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/events/stream?last_event_id=bad%0Aevent", nil)
	if got := readLastEventID(request); got != "" {
		t.Fatalf("expected invalid event id to be rejected, got %q", got)
	}
}

func TestNormalizeSSEEventType(t *testing.T) {
	t.Parallel()

	if got := normalizeSSEEventType(" message.created "); got != "message.created" {
		t.Fatalf("unexpected normalized event type %q", got)
	}
	if got := normalizeSSEEventType("message\ncreated"); got != "" {
		t.Fatalf("expected newline event type rejection, got %q", got)
	}
}

func TestFakeServiceSubscribeClosed(t *testing.T) {
	t.Parallel()

	service := &fakeService{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	select {
	case _, ok := <-service.Subscribe(ctx):
		if ok {
			t.Fatal("expected closed channel")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for closed channel")
	}
}

func TestHealthRoutesReturnServiceUnavailableWhenUnhealthy(t *testing.T) {
	t.Parallel()

	handler := NewHTTPHandler(&fakeService{healthErr: errors.New("store unavailable")}, nil)

	for _, path := range []string{"/healthz", "/readyz"} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		handler.ServeHTTP(response, request)

		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s unexpected status %d", path, response.Code)
		}
	}
}
