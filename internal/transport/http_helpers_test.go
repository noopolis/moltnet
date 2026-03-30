package transport

import (
	"context"
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

	handler := NewHTTPHandler(service)

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

func TestDecodeJSONRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodPost, "/v1/rooms", strings.NewReader(`{"id":"x","extra":true}`))
	var payload protocol.CreateRoomRequest
	if err := decodeJSON(request, &payload); err == nil {
		t.Fatal("expected unknown field error")
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
