package transport

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/noopolis/moltnet/internal/observability"
)

type transportLoggerFunc func(ctx context.Context, component string, attrs ...any) *slog.Logger

var transportLoggerValue atomic.Value

func init() {
	transportLoggerValue.Store(transportLoggerFunc(func(ctx context.Context, component string, attrs ...any) *slog.Logger {
		return observability.Logger(ctx, component, attrs...)
	}))
}

func transportLogger(ctx context.Context, component string, attrs ...any) *slog.Logger {
	return transportLoggerValue.Load().(transportLoggerFunc)(ctx, component, attrs...)
}

func setTransportLoggerForTest(logger transportLoggerFunc) func() {
	previous := transportLoggerValue.Load().(transportLoggerFunc)
	transportLoggerValue.Store(logger)
	return func() {
		transportLoggerValue.Store(previous)
	}
}

func withObservability(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestID := observability.NormalizeRequestID(request.Header.Get("X-Request-Id"))
		if requestID == "" {
			requestID = observability.NewRequestID()
		}
		response.Header().Set("X-Request-Id", requestID)

		ctx := observability.WithRequestID(request.Context(), requestID)
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: response, status: http.StatusOK, ctx: ctx}
		instrumentedRequest := request.WithContext(ctx)
		next.ServeHTTP(recorder, instrumentedRequest)

		route := instrumentedRequest.Pattern
		if route == "" {
			route = instrumentedRequest.URL.Path
		}

		duration := time.Since(start)
		observability.DefaultMetrics.RecordHTTPRequest(instrumentedRequest.Method, route, recorder.status, duration)

		logger := transportLogger(ctx, "transport.http",
			"method", instrumentedRequest.Method,
			"route", route,
			"path", instrumentedRequest.URL.Path,
			"status", recorder.status,
			"duration_ms", duration.Milliseconds(),
		)
		if recorder.status >= 500 {
			logger.Error("request completed with error")
			return
		}
		if recorder.status >= 400 {
			logger.Warn("request completed with error")
			return
		}
		logger.Info("request completed")
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	ctx    context.Context
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Flush() {
	flusher, ok := r.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}
	flusher.Flush()
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response does not implement http.Hijacker")
	}
	return hijacker.Hijack()
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (r *statusRecorder) Context() context.Context {
	if r == nil || r.ctx == nil {
		return context.Background()
	}
	return r.ctx
}
