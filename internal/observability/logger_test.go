package observability

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestNormalizeRequestID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "valid", value: "req_123-abc", want: "req_123-abc"},
		{name: "trimmed", value: "  req_123  ", want: "req_123"},
		{name: "empty", value: "   ", want: ""},
		{name: "invalid chars", value: "req_\n123", want: ""},
		{name: "too long", value: strings.Repeat("a", maxRequestIDLength+1), want: ""},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeRequestID(test.value); got != test.want {
				t.Fatalf("NormalizeRequestID(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

func TestWithRequestIDAndLogger(t *testing.T) {
	var buffer bytes.Buffer
	previous := baseLogger
	baseLogger = slog.New(slog.NewTextHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelInfo}))
	defer func() { baseLogger = previous }()

	ctx := WithRequestID(context.Background(), "req_42")
	Logger(ctx, "transport.http", "route", "GET /v1/network").Info("request completed")

	output := buffer.String()
	if !strings.Contains(output, "request_id=req_42") {
		t.Fatalf("expected request id in log output %q", output)
	}
	if !strings.Contains(output, "component=transport.http") {
		t.Fatalf("expected component in log output %q", output)
	}
	if !strings.Contains(output, "route=\"GET /v1/network\"") {
		t.Fatalf("expected route in log output %q", output)
	}
}

func TestNewRequestID(t *testing.T) {
	t.Parallel()

	value := NewRequestID()
	if !strings.HasPrefix(value, "req_") {
		t.Fatalf("expected request id prefix, got %q", value)
	}
	if len(value) != len("req_")+16 {
		t.Fatalf("unexpected request id length %d", len(value))
	}
}

func TestRedactURL(t *testing.T) {
	t.Parallel()

	if got := RedactURL("https://user:secret@example.com/path"); got != "https://***@example.com/path" {
		t.Fatalf("unexpected redacted url %q", got)
	}
	if got := RedactURL("not a url"); got != "not a url" {
		t.Fatalf("expected raw fallback, got %q", got)
	}
}
