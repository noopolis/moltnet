package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strings"
)

type requestIDContextKey struct{}

const maxRequestIDLength = 128

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

var baseLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
	Level: slog.LevelInfo,
}))

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDContextKey{}, NormalizeRequestID(requestID))
}

func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDContextKey{}).(string)
	return NormalizeRequestID(value)
}

func Logger(ctx context.Context, component string, attrs ...any) *slog.Logger {
	logger := baseLogger.With("component", component)
	if requestID := RequestID(ctx); requestID != "" {
		logger = logger.With("request_id", requestID)
	}
	if len(attrs) > 0 {
		logger = logger.With(attrs...)
	}
	return logger
}

func NewRequestID() string {
	var buffer [8]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return "req_unknown"
	}
	return "req_" + hex.EncodeToString(buffer[:])
}

func NormalizeRequestID(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > maxRequestIDLength {
		return ""
	}
	if !requestIDPattern.MatchString(trimmed) {
		return ""
	}
	return trimmed
}

func RedactURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return trimmed
	}
	if parsed.Scheme == "" && parsed.Host == "" {
		return trimmed
	}
	if parsed.User == nil {
		return parsed.String()
	}
	parsed.User = nil
	redacted := parsed.String()
	if parsed.Scheme == "" {
		return redacted
	}
	prefix := parsed.Scheme + "://"
	return prefix + "***@" + strings.TrimPrefix(redacted, prefix)
}
