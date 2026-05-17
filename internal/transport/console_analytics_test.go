package transport

import (
	"strings"
	"testing"
)

func TestConsoleAnalyticsSnippetGoogle(t *testing.T) {
	t.Parallel()

	snippet := consoleAnalyticsSnippet(ConsoleAnalyticsConfig{
		Provider:      "google",
		MeasurementID: "G-ABC123",
	})

	for _, want := range []string{
		"https://www.googletagmanager.com/gtag/js?id=G-ABC123",
		`gtag("config", "G-ABC123", { anonymize_ip: true });`,
	} {
		if !strings.Contains(snippet, want) {
			t.Fatalf("snippet missing %q\n%s", want, snippet)
		}
	}
}

func TestConsoleAnalyticsSnippetSkipsUnsupportedConfig(t *testing.T) {
	t.Parallel()

	testCases := []ConsoleAnalyticsConfig{
		{},
		{Provider: "google"},
		{Provider: "plausible", MeasurementID: "G-ABC123"},
	}
	for _, testCase := range testCases {
		if snippet := consoleAnalyticsSnippet(testCase); snippet != "" {
			t.Fatalf("expected empty snippet for %#v, got %q", testCase, snippet)
		}
	}
}

func TestInjectConsoleAnalytics(t *testing.T) {
	t.Parallel()

	html := "<html><head><title>Moltnet</title></head><body></body></html>"
	got := injectConsoleAnalytics(html, ConsoleAnalyticsConfig{
		Provider:      "google",
		MeasurementID: "G-ABC123",
	})

	if !strings.Contains(got, "googletagmanager.com") {
		t.Fatalf("expected analytics script, got %s", got)
	}
	if !strings.Contains(got, "</head>") || strings.Index(got, "googletagmanager.com") > strings.Index(got, "</head>") {
		t.Fatalf("expected analytics before </head>, got %s", got)
	}
}
