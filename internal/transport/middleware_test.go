package transport

import (
	"bufio"
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/noopolis/moltnet/internal/observability"
)

type flushWriter struct {
	http.ResponseWriter
	flushed bool
}

func (w *flushWriter) Flush() {
	w.flushed = true
}

type hijackWriter struct {
	http.ResponseWriter
	called bool
}

func (w *hijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.called = true
	return nil, bufio.NewReadWriter(bufio.NewReader(nil), bufio.NewWriter(nil)), nil
}

type lockedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}

func TestStatusRecorderFlushAndUnwrap(t *testing.T) {
	t.Parallel()

	base := &flushWriter{ResponseWriter: httptest.NewRecorder()}
	recorder := &statusRecorder{ResponseWriter: base, status: http.StatusOK}
	recorder.Flush()
	if !base.flushed {
		t.Fatal("expected flush to reach wrapped response writer")
	}
	if recorder.Unwrap() != base {
		t.Fatal("expected unwrap to return wrapped writer")
	}
}

func TestStatusRecorderHijack(t *testing.T) {
	t.Parallel()

	base := &hijackWriter{ResponseWriter: httptest.NewRecorder()}
	recorder := &statusRecorder{ResponseWriter: base, status: http.StatusOK}
	if _, _, err := recorder.Hijack(); err != nil {
		t.Fatalf("Hijack() error = %v", err)
	}
	if !base.called {
		t.Fatal("expected hijack to reach wrapped writer")
	}
}

func TestStatusRecorderContextFallback(t *testing.T) {
	t.Parallel()

	recorder := &statusRecorder{}
	if got := recorder.Context(); got == nil {
		t.Fatal("expected non-nil fallback context")
	}
}

func TestWithObservabilityLogsStatusSeverity(t *testing.T) {
	tests := []struct {
		name   string
		status int
		level  string
	}{
		{name: "info", status: http.StatusOK, level: "INFO"},
		{name: "warn", status: http.StatusNotFound, level: "WARN"},
		{name: "error", status: http.StatusServiceUnavailable, level: "ERROR"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
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

			handler := withObservability(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				response.WriteHeader(test.status)
			}))
			request := httptest.NewRequest(http.MethodGet, "/v1/network", nil)
			request.Header.Set("X-Request-Id", "req_test")
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if got := response.Header().Get("X-Request-Id"); got != "req_test" {
				t.Fatalf("unexpected request id header %q", got)
			}
			output := buffer.String()
			if !strings.Contains(output, "level="+test.level) {
				t.Fatalf("expected log level %s in %q", test.level, output)
			}
			if !strings.Contains(output, "request_id=req_test") {
				t.Fatalf("expected request id in %q", output)
			}
		})
	}
}
