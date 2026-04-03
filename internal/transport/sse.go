package transport

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/observability"
)

const (
	defaultMaxSSESubscribers = 128
	defaultMaxSSEPerIdentity = 16
	sseHeartbeatInterval     = 15 * time.Second
)

type streamLimiter struct {
	slots            chan struct{}
	perIdentityLimit int
	mu               sync.Mutex
	counts           map[string]int
}

func newStreamLimiter(limit int) *streamLimiter {
	if limit <= 0 {
		limit = defaultMaxSSESubscribers
	}
	return &streamLimiter{
		slots:            make(chan struct{}, limit),
		perIdentityLimit: defaultMaxSSEPerIdentity,
		counts:           make(map[string]int),
	}
}

func (l *streamLimiter) acquire(identity string) bool {
	select {
	case l.slots <- struct{}{}:
	default:
		return false
	}

	if identity == "" {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.perIdentityLimit > 0 && l.counts[identity] >= l.perIdentityLimit {
		<-l.slots
		return false
	}
	l.counts[identity]++
	return true
}

func (l *streamLimiter) release(identity string) {
	if identity != "" {
		l.mu.Lock()
		if count := l.counts[identity]; count <= 1 {
			delete(l.counts, identity)
		} else {
			l.counts[identity] = count - 1
		}
		l.mu.Unlock()
	}

	select {
	case <-l.slots:
	default:
	}
}

func handleEventStream(service Service, limiter *streamLimiter) http.HandlerFunc {
	return newEventStreamHandler(service, limiter, sseHeartbeatInterval)
}

func newEventStreamHandler(service Service, limiter *streamLimiter, heartbeatInterval time.Duration) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		flusher, ok := responseFlusher(response)
		if !ok {
			writeError(response, http.StatusInternalServerError, errors.New("streaming unsupported"))
			return
		}
		identity := streamIdentity(request)
		if !limiter.acquire(identity) {
			writeError(response, http.StatusServiceUnavailable, errors.New("too many concurrent sse subscribers"))
			return
		}
		defer limiter.release(identity)

		if err := clearStreamingWriteDeadline(response); err != nil {
			writeError(response, http.StatusInternalServerError, fmt.Errorf("prepare event stream: %w", err))
			return
		}

		response.Header().Set("Content-Type", "text/event-stream")
		response.Header().Set("Cache-Control", "no-cache")
		response.Header().Set("Connection", "keep-alive")

		if _, err := fmt.Fprint(response, ": stream-open\n\n"); err != nil {
			return
		}
		flusher.Flush()
		observability.DefaultMetrics.AddActiveSSE(1)
		defer observability.DefaultMetrics.AddActiveSSE(-1)
		observability.Logger(request.Context(), "transport.sse").Info("stream opened")
		defer observability.Logger(request.Context(), "transport.sse").Info("stream closed")

		heartbeatTicker := time.NewTicker(heartbeatInterval)
		defer heartbeatTicker.Stop()

		stream := service.SubscribeFrom(request.Context(), readLastEventID(request))
		for {
			select {
			case <-request.Context().Done():
				return
			case <-heartbeatTicker.C:
				if _, err := fmt.Fprint(response, ": keep-alive\n\n"); err != nil {
					return
				}
				flusher.Flush()
			case event, ok := <-stream:
				if !ok {
					return
				}

				payload, err := json.Marshal(event)
				if err != nil {
					observability.Logger(request.Context(), "transport.sse").Error("encode event", "error", err)
					continue
				}
				eventType := normalizeSSEEventType(event.Type)
				if eventType == "" {
					observability.Logger(request.Context(), "transport.sse", "event_id", event.ID).
						Error("skip unsafe event type", "event_type", event.Type)
					continue
				}
				eventID := normalizeSSEEventID(event.ID)
				if eventID == "" {
					observability.Logger(request.Context(), "transport.sse", "event_id", event.ID).
						Error("skip unsafe event id")
					continue
				}

				if _, err := fmt.Fprintf(response, "id: %s\n", eventID); err != nil {
					return
				}
				if _, err := fmt.Fprintf(response, "event: %s\n", eventType); err != nil {
					return
				}
				if _, err := fmt.Fprintf(response, "data: %s\n\n", payload); err != nil {
					return
				}

				flusher.Flush()
			}
		}
	}
}

func streamIdentity(request *http.Request) string {
	if claims, ok := authn.ClaimsFromContext(request.Context()); ok && strings.TrimSpace(claims.TokenID) != "" {
		return "token:" + strings.TrimSpace(claims.TokenID)
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(request.RemoteAddr))
	if err == nil && strings.TrimSpace(host) != "" {
		return "addr:" + strings.TrimSpace(host)
	}
	if trimmed := strings.TrimSpace(request.RemoteAddr); trimmed != "" {
		return "addr:" + trimmed
	}
	return "anonymous"
}
