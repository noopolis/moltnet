package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestHelperReadersAndWriters(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/v1/rooms?limit=999&before=msg_2&after=msg_1&last_event_id=evt_1", nil)
	request.Header.Set("Last-Event-ID", "evt_header")
	if got := readLimit(request); got != 500 {
		t.Fatalf("unexpected limit %d", got)
	}
	if got := readLastEventID(request); got != "evt_header" {
		t.Fatalf("unexpected last event id %q", got)
	}
	if _, err := readPageRequest(request); err == nil {
		t.Fatal("expected mutually exclusive before/after validation error")
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/rooms?limit=999&before=msg_2", nil)
	page, err := readPageRequest(request)
	if err != nil {
		t.Fatalf("readPageRequest() error = %v", err)
	}
	if page.Before != "msg_2" || page.After != "" || page.Limit != 500 {
		t.Fatalf("unexpected page request %#v", page)
	}

	recorder := httptest.NewRecorder()
	writeJSON(recorder, http.StatusAccepted, map[string]string{"status": "ok"})
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected status %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("unexpected content type %q", got)
	}

	errorRecorder := httptest.NewRecorder()
	writeError(errorRecorder, http.StatusBadRequest, errors.New("bad request"))
	if errorRecorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected error status %d", errorRecorder.Code)
	}
	if !bytes.Contains(errorRecorder.Body.Bytes(), []byte(`"error"`)) {
		t.Fatalf("unexpected error body %q", errorRecorder.Body.String())
	}
	internalRecorder := httptest.NewRecorder()
	internalRecorder.Header().Set("X-Request-Id", "req_123")
	writeError(internalRecorder, http.StatusInternalServerError, errors.New("leak me"))
	if bytes.Contains(internalRecorder.Body.Bytes(), []byte("leak me")) {
		t.Fatalf("expected sanitized internal error, got %q", internalRecorder.Body.String())
	}
	if !bytes.Contains(internalRecorder.Body.Bytes(), []byte(`"code":"internal_error"`)) {
		t.Fatalf("expected machine-readable error code, got %q", internalRecorder.Body.String())
	}
}

func TestDecodeJSONValidation(t *testing.T) {
	t.Parallel()

	type payload struct {
		Name string `json:"name"`
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/rooms", bytes.NewBufferString(`{"name":"research"}`))
	var got payload
	if err := decodeJSON(recorder, request, &got); err != nil {
		t.Fatalf("decodeJSON() error = %v", err)
	}
	if got.Name != "research" {
		t.Fatalf("unexpected payload %#v", got)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/v1/rooms", bytes.NewBufferString(`{"name":"research","extra":true}`))
	if err := decodeJSON(recorder, request, &got); err != nil {
		t.Fatalf("decodeJSON() should ignore unknown fields, got %v", err)
	}

	for _, body := range []string{
		`{"name":"research"}{"name":"second"}`,
	} {
		recorder = httptest.NewRecorder()
		request = httptest.NewRequest(http.MethodPost, "/v1/rooms", bytes.NewBufferString(body))
		if err := decodeJSON(recorder, request, &got); err == nil {
			t.Fatalf("expected decodeJSON() error for body %q", body)
		}
	}
}

func TestResponseFlusherAndStreamingDeadline(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	if _, ok := responseFlusher(recorder); !ok {
		t.Fatal("expected httptest recorder to implement flushing")
	}

	status := &statusRecorder{ResponseWriter: recorder, status: http.StatusOK}
	if _, ok := responseFlusher(status); !ok {
		t.Fatal("expected status recorder to expose flushing")
	}
	if err := clearStreamingWriteDeadline(recorder); err != nil {
		t.Fatalf("clearStreamingWriteDeadline() error = %v", err)
	}
}

func TestWriteJSONEncodesValidJSON(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	writeJSON(recorder, http.StatusOK, protocol.Room{ID: "research"})

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("response body should be valid json: %v", err)
	}
	if payload["id"] != "research" {
		t.Fatalf("unexpected payload %#v", payload)
	}
}

func TestHelperNormalizersAndErrorMetadata(t *testing.T) {
	t.Parallel()

	if got := normalizeEventCursor("bad\ncursor"); got != "" {
		t.Fatalf("expected invalid event cursor to be cleared, got %q", got)
	}
	if got := normalizeEventCursor("evt_123"); got != "evt_123" {
		t.Fatalf("unexpected normalized event cursor %q", got)
	}
	if got := normalizeSSEEventType("message.created"); got != "message.created" {
		t.Fatalf("unexpected normalized sse event type %q", got)
	}
	if got := normalizeSSEEventType("message/created"); got != "message/created" {
		t.Fatalf("expected slash event type to remain valid, got %q", got)
	}
	if got := normalizeSSEEventType("bad\nevent"); got != "" {
		t.Fatalf("expected invalid sse event type to be cleared, got %q", got)
	}
	if got := normalizeSSEEventID("evt_123"); got != "evt_123" {
		t.Fatalf("unexpected normalized sse event id %q", got)
	}
	if got := normalizeSSEEventID("bad\nevent"); got != "" {
		t.Fatalf("expected invalid sse event id to be cleared, got %q", got)
	}

	if got := publicErrorMessage(http.StatusBadRequest, nil); got != "bad request" {
		t.Fatalf("unexpected public error message %q", got)
	}
	if got := publicErrorMessage(http.StatusInternalServerError, errors.New("internal leak")); got != "internal server error" {
		t.Fatalf("unexpected sanitized internal error message %q", got)
	}
	if got := publicErrorMessage(599, nil); got != "internal server error" {
		t.Fatalf("unexpected fallback internal error message %q", got)
	}

	if got := errorCodeForStatus(http.StatusServiceUnavailable); got != "service_unavailable" {
		t.Fatalf("unexpected service unavailable error code %q", got)
	}
	if got := errorCodeForStatus(http.StatusTeapot); got != "internal_error" {
		t.Fatalf("unexpected fallback error code %q", got)
	}
}

func TestWriteErrorUsesResponseContextForLogging(t *testing.T) {
	var buffer lockedBuffer
	restore := setTransportLoggerForTest(func(ctx context.Context, component string, attrs ...any) *slog.Logger {
		logger := slog.New(slog.NewTextHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelInfo})).
			With("component", component)
		if requestID := observability.RequestID(ctx); requestID != "" {
			logger = logger.With("request_id", requestID)
		}
		if len(attrs) > 0 {
			logger = logger.With(attrs...)
		}
		return logger
	})
	defer restore()

	recorder := &statusRecorder{
		ResponseWriter: httptest.NewRecorder(),
		status:         http.StatusOK,
		ctx:            observability.WithRequestID(context.Background(), "req_ctx"),
	}
	recorder.Header().Set("X-Request-Id", "req_ctx")
	writeError(recorder, http.StatusInternalServerError, errors.New("boom"))

	if output := buffer.String(); !bytes.Contains([]byte(output), []byte("request_id=req_ctx")) {
		t.Fatalf("expected request id in log output, got %q", output)
	}
}
