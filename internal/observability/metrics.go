package observability

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

var requestDurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type Metrics struct {
	mu sync.Mutex

	httpRequests  map[string]uint64
	httpDurations map[string]*durationMetric
	activeAttach  int64
	activeSSE     int64
	relayTotals   map[string]uint64
	eventsDropped uint64
	storeHealth   float64
}

type durationMetric struct {
	count   uint64
	sum     float64
	buckets []uint64
}

var DefaultMetrics = NewMetrics()

func NewMetrics() *Metrics {
	return &Metrics{
		httpRequests:  make(map[string]uint64),
		httpDurations: make(map[string]*durationMetric),
		relayTotals:   make(map[string]uint64),
	}
}

func (m *Metrics) RecordHTTPRequest(method string, route string, status int, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := routeKey(method, route, status)
	m.httpRequests[key]++

	durationKey := routeDurationKey(method, route)
	metric, ok := m.httpDurations[durationKey]
	if !ok {
		metric = &durationMetric{buckets: make([]uint64, len(requestDurationBuckets))}
		m.httpDurations[durationKey] = metric
	}
	seconds := duration.Seconds()
	metric.count++
	metric.sum += seconds
	for index, bucket := range requestDurationBuckets {
		if seconds <= bucket {
			metric.buckets[index]++
		}
	}
}

func (m *Metrics) AddActiveAttachments(delta int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeAttach += delta
}

func (m *Metrics) AddActiveSSE(delta int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeSSE += delta
}

func (m *Metrics) RecordRelay(result string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.relayTotals[strings.TrimSpace(result)]++
}

func (m *Metrics) RecordDroppedEvent() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventsDropped++
}

func (m *Metrics) RecordStoreHealth(ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ok {
		m.storeHealth = 1
		return
	}
	m.storeHealth = 0
}

func (m *Metrics) ServeHTTP(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = response.Write([]byte(m.RenderPrometheus()))
}

func (m *Metrics) RenderPrometheus() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var lines []string
	lines = append(lines,
		"# HELP moltnet_http_requests_total Total HTTP requests handled by Moltnet.",
		"# TYPE moltnet_http_requests_total counter",
	)
	for _, key := range sortedKeys(m.httpRequests) {
		method, route, status := splitRouteKey(key)
		lines = append(lines, fmt.Sprintf(
			`moltnet_http_requests_total{method=%q,route=%q,status=%q} %d`,
			method, route, status, m.httpRequests[key],
		))
	}

	lines = append(lines,
		"# HELP moltnet_http_request_duration_seconds HTTP request duration histogram.",
		"# TYPE moltnet_http_request_duration_seconds histogram",
	)
	for _, key := range sortedKeys(m.httpDurations) {
		method, route := splitDurationKey(key)
		metric := m.httpDurations[key]
		for index, bucket := range requestDurationBuckets {
			lines = append(lines, fmt.Sprintf(
				`moltnet_http_request_duration_seconds_bucket{method=%q,route=%q,le=%q} %d`,
				method, route, trimFloat(bucket), metric.buckets[index],
			))
		}
		lines = append(lines, fmt.Sprintf(
			`moltnet_http_request_duration_seconds_bucket{method=%q,route=%q,le="+Inf"} %d`,
			method, route, metric.count,
		))
		lines = append(lines, fmt.Sprintf(
			`moltnet_http_request_duration_seconds_sum{method=%q,route=%q} %s`,
			method, route, trimFloat(metric.sum),
		))
		lines = append(lines, fmt.Sprintf(
			`moltnet_http_request_duration_seconds_count{method=%q,route=%q} %d`,
			method, route, metric.count,
		))
	}

	lines = append(lines,
		"# HELP moltnet_active_attachments Active attachment websocket connections.",
		"# TYPE moltnet_active_attachments gauge",
		fmt.Sprintf("moltnet_active_attachments %d", m.activeAttach),
		"# HELP moltnet_active_sse_subscribers Active SSE observer connections.",
		"# TYPE moltnet_active_sse_subscribers gauge",
		fmt.Sprintf("moltnet_active_sse_subscribers %d", m.activeSSE),
		"# HELP moltnet_relay_total Relay attempt outcomes.",
		"# TYPE moltnet_relay_total counter",
	)
	for _, result := range sortedKeys(m.relayTotals) {
		lines = append(lines, fmt.Sprintf(`moltnet_relay_total{result=%q} %d`, result, m.relayTotals[result]))
	}
	lines = append(lines,
		"# HELP moltnet_events_dropped_total Dropped live events because subscriber buffers were full.",
		"# TYPE moltnet_events_dropped_total counter",
		fmt.Sprintf("moltnet_events_dropped_total %d", m.eventsDropped),
		"# HELP moltnet_store_health Store readiness gauge from the latest health check.",
		"# TYPE moltnet_store_health gauge",
		fmt.Sprintf("moltnet_store_health %s", trimFloat(m.storeHealth)),
	)

	return strings.Join(lines, "\n") + "\n"
}

func routeKey(method string, route string, status int) string {
	return strings.TrimSpace(method) + "\x00" + strings.TrimSpace(route) + "\x00" + strconv.Itoa(status)
}

func routeDurationKey(method string, route string) string {
	return strings.TrimSpace(method) + "\x00" + strings.TrimSpace(route)
}

func splitRouteKey(key string) (string, string, string) {
	parts := strings.Split(key, "\x00")
	if len(parts) != 3 {
		return key, "", ""
	}
	return parts[0], parts[1], parts[2]
}

func splitDurationKey(key string) (string, string) {
	parts := strings.Split(key, "\x00")
	if len(parts) != 2 {
		return key, ""
	}
	return parts[0], parts[1]
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func trimFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
