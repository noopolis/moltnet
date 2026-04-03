package observability

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsRenderPrometheus(t *testing.T) {
	t.Parallel()

	metrics := NewMetrics()
	metrics.RecordHTTPRequest("GET", "GET /v1/network", 200, 15*time.Millisecond)
	metrics.RecordHTTPRequest("GET", "GET /v1/network", 200, 50*time.Millisecond)
	metrics.RecordRelay("connected")
	metrics.RecordDroppedEvent()
	metrics.AddActiveAttachments(2)
	metrics.AddActiveSSE(3)
	metrics.RecordStoreHealth(true)

	output := metrics.RenderPrometheus()
	for _, snippet := range []string{
		`moltnet_http_requests_total{method="GET",route="GET /v1/network",status="200"} 2`,
		`moltnet_http_request_duration_seconds_bucket{method="GET",route="GET /v1/network",le="0.025"} 1`,
		`moltnet_http_request_duration_seconds_bucket{method="GET",route="GET /v1/network",le="0.05"} 2`,
		`moltnet_http_request_duration_seconds_count{method="GET",route="GET /v1/network"} 2`,
		`moltnet_active_attachments 2`,
		`moltnet_active_sse_subscribers 3`,
		`moltnet_relay_total{result="connected"} 1`,
		`moltnet_events_dropped_total 1`,
		`moltnet_store_health 1`,
	} {
		if !strings.Contains(output, snippet) {
			t.Fatalf("expected metrics output to contain %q, got %q", snippet, output)
		}
	}
}

func TestMetricsServeHTTP(t *testing.T) {
	t.Parallel()

	metrics := NewMetrics()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/metrics", nil)

	metrics.ServeHTTP(recorder, request)

	if got := recorder.Header().Get("Content-Type"); got != "text/plain; version=0.0.4; charset=utf-8" {
		t.Fatalf("unexpected content type %q", got)
	}
	if !strings.Contains(recorder.Body.String(), "# HELP moltnet_http_requests_total") {
		t.Fatalf("unexpected metrics body %q", recorder.Body.String())
	}
}

func TestMetricHelpers(t *testing.T) {
	t.Parallel()

	if got := routeKey("GET", " GET /v1/network ", 200); got != "GET\x00GET /v1/network\x00200" {
		t.Fatalf("unexpected route key %q", got)
	}
	if method, route, status := splitRouteKey("GET\x00/v1\x00200"); method != "GET" || route != "/v1" || status != "200" {
		t.Fatalf("unexpected split route values %q %q %q", method, route, status)
	}
	if method, route := splitDurationKey("GET\x00/v1"); method != "GET" || route != "/v1" {
		t.Fatalf("unexpected split duration values %q %q", method, route)
	}
	if got := trimFloat(0.25); got != "0.25" {
		t.Fatalf("unexpected trimmed float %q", got)
	}
	if method, route, status := splitRouteKey("bad"); method != "bad" || route != "" || status != "" {
		t.Fatalf("unexpected split route fallback %q %q %q", method, route, status)
	}
	if method, route := splitDurationKey("bad"); method != "bad" || route != "" {
		t.Fatalf("unexpected split duration fallback %q %q", method, route)
	}
}

func TestMetricsGaugeMutators(t *testing.T) {
	t.Parallel()

	metrics := NewMetrics()
	metrics.AddActiveAttachments(2)
	metrics.AddActiveSSE(5)

	output := metrics.RenderPrometheus()
	for _, snippet := range []string{
		"moltnet_active_attachments 2",
		"moltnet_active_sse_subscribers 5",
	} {
		if !strings.Contains(output, snippet) {
			t.Fatalf("expected metrics output to contain %q, got %q", snippet, output)
		}
	}
}
