package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type signalingService struct {
	*fakeService
	subscribed chan struct{}
	once       sync.Once
}

func (s *signalingService) SubscribeFrom(ctx context.Context, lastEventID string) <-chan protocol.Event {
	s.once.Do(func() { close(s.subscribed) })
	return s.fakeService.SubscribeFrom(ctx, lastEventID)
}

type signalRecorder struct {
	header http.Header
	body   strings.Builder
	code   int
	needle string
	signal chan struct{}
	once   sync.Once
}

func newSignalRecorder(needle string) *signalRecorder {
	return &signalRecorder{
		header: make(http.Header),
		code:   http.StatusOK,
		needle: needle,
		signal: make(chan struct{}),
	}
}

func (r *signalRecorder) Header() http.Header { return r.header }

func (r *signalRecorder) Write(bytes []byte) (int, error) {
	written, err := r.body.Write(bytes)
	if strings.Contains(r.body.String(), r.needle) {
		r.once.Do(func() { close(r.signal) })
	}
	return written, err
}

func (r *signalRecorder) WriteHeader(status int) { r.code = status }
func (r *signalRecorder) Flush()                 {}

type plainRecorder struct {
	header http.Header
	body   strings.Builder
	code   int
}

func newPlainRecorder() *plainRecorder {
	return &plainRecorder{
		header: make(http.Header),
		code:   http.StatusOK,
	}
}

func (r *plainRecorder) Header() http.Header { return r.header }

func (r *plainRecorder) Write(bytes []byte) (int, error) {
	return r.body.Write(bytes)
}

func (r *plainRecorder) WriteHeader(status int) { r.code = status }

func TestEventStreamHeartbeats(t *testing.T) {
	t.Parallel()

	service := &signalingService{
		fakeService: &fakeService{stream: make(chan protocol.Event)},
		subscribed:  make(chan struct{}),
	}
	handler := newEventStreamHandler(service, newStreamLimiter(1), 5*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil).WithContext(ctx)
	recorder := newSignalRecorder(": keep-alive")
	done := make(chan struct{})

	go func() {
		handler.ServeHTTP(recorder, request)
		close(done)
	}()

	select {
	case <-service.subscribed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream subscription")
	}

	select {
	case <-recorder.signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for keep-alive comment, body=%q", recorder.body.String())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream shutdown")
	}
}

func TestEventStreamRejectsWhenCapacityIsFull(t *testing.T) {
	t.Parallel()

	limiter := newStreamLimiter(1)
	firstService := &signalingService{
		fakeService: &fakeService{stream: make(chan protocol.Event)},
		subscribed:  make(chan struct{}),
	}
	handler := newEventStreamHandler(firstService, limiter, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	firstRequest := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil).WithContext(ctx)
	firstRecorder := newSignalRecorder(": stream-open")
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(firstRecorder, firstRequest)
		close(done)
	}()

	select {
	case <-firstService.subscribed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first stream subscription")
	}

	secondResponse := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil)
	handler.ServeHTTP(secondResponse, secondRequest)

	if secondResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected second stream status %d", secondResponse.Code)
	}
	if !strings.Contains(secondResponse.Body.String(), `"code":"service_unavailable"`) {
		t.Fatalf("unexpected capacity error body %s", secondResponse.Body.String())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first stream shutdown")
	}
}

func TestEventStreamRejectsWhenStreamingUnsupported(t *testing.T) {
	t.Parallel()

	handler := newEventStreamHandler(&fakeService{}, newStreamLimiter(1), time.Hour)
	response := newPlainRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil)

	handler.ServeHTTP(response, request)

	if response.code != http.StatusInternalServerError {
		t.Fatalf("unexpected status %d", response.code)
	}
	if !strings.Contains(response.body.String(), `"code":"internal_error"`) {
		t.Fatalf("unexpected error body %q", response.body.String())
	}
}

func TestEventStreamRejectsWhenIdentityCapacityIsFull(t *testing.T) {
	t.Parallel()

	limiter := newStreamLimiter(2)
	limiter.perIdentityLimit = 1

	firstService := &signalingService{
		fakeService: &fakeService{stream: make(chan protocol.Event)},
		subscribed:  make(chan struct{}),
	}
	handler := newEventStreamHandler(firstService, limiter, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	firstRequest := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil).WithContext(authn.WithClaims(ctx, authn.Claims{TokenID: "observer"}))
	firstRecorder := newSignalRecorder(": stream-open")
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(firstRecorder, firstRequest)
		close(done)
	}()

	select {
	case <-firstService.subscribed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first stream subscription")
	}

	secondResponse := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil).WithContext(authn.WithClaims(context.Background(), authn.Claims{TokenID: "observer"}))
	handler.ServeHTTP(secondResponse, secondRequest)
	if secondResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected second stream status %d", secondResponse.Code)
	}

	otherService := &signalingService{
		fakeService: &fakeService{stream: make(chan protocol.Event)},
		subscribed:  make(chan struct{}),
	}
	otherHandler := newEventStreamHandler(otherService, limiter, time.Hour)
	otherCtx, otherCancel := context.WithCancel(context.Background())
	defer otherCancel()
	otherRequest := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil).WithContext(authn.WithClaims(otherCtx, authn.Claims{TokenID: "operator"}))
	otherRecorder := newSignalRecorder(": stream-open")
	otherDone := make(chan struct{})
	go func() {
		otherHandler.ServeHTTP(otherRecorder, otherRequest)
		close(otherDone)
	}()

	select {
	case <-otherService.subscribed:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second identity stream subscription")
	}

	cancel()
	otherCancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first stream shutdown")
	}
	select {
	case <-otherDone:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second stream shutdown")
	}
}

func TestStreamLimiterDefaultsAndReleaseIsSafe(t *testing.T) {
	t.Parallel()

	limiter := newStreamLimiter(0)
	limiter.perIdentityLimit = 0
	for index := 0; index < defaultMaxSSESubscribers; index++ {
		if !limiter.acquire("identity") {
			t.Fatalf("expected acquire to succeed at slot %d", index)
		}
	}
	if limiter.acquire("identity") {
		t.Fatal("expected limiter to reject acquire when full")
	}

	for index := 0; index < defaultMaxSSESubscribers; index++ {
		limiter.release("identity")
	}
	limiter.release("identity")
	if !limiter.acquire("identity") {
		t.Fatal("expected acquire after releasing all slots")
	}
}

func TestEventStreamSkipsUnsafeEventFields(t *testing.T) {
	t.Parallel()

	t.Run("unsafe type", func(t *testing.T) {
		t.Parallel()

		stream := make(chan protocol.Event, 1)
		stream <- protocol.Event{
			ID:        "evt_123",
			Type:      "bad\nevent",
			NetworkID: "local",
		}
		close(stream)

		handler := newEventStreamHandler(&fakeService{stream: stream}, newStreamLimiter(1), time.Hour)
		response := newSignalRecorder("")
		request := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil)
		handler.ServeHTTP(response, request)

		if strings.Contains(response.body.String(), "event: ") {
			t.Fatalf("expected unsafe event type to be skipped, got %q", response.body.String())
		}
	})

	t.Run("unsafe id", func(t *testing.T) {
		t.Parallel()

		stream := make(chan protocol.Event, 1)
		stream <- protocol.Event{
			ID:        "bad\nevent",
			Type:      protocol.EventTypeMessageCreated,
			NetworkID: "local",
		}
		close(stream)

		handler := newEventStreamHandler(&fakeService{stream: stream}, newStreamLimiter(1), time.Hour)
		response := newSignalRecorder("")
		request := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil)
		handler.ServeHTTP(response, request)

		if strings.Contains(response.body.String(), "id: ") {
			t.Fatalf("expected unsafe event id to be skipped, got %q", response.body.String())
		}
	})
}

func TestStreamIdentityPrefersTokenThenRemoteAddr(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil).
		WithContext(authn.WithClaims(context.Background(), authn.Claims{TokenID: "observer"}))
	request.RemoteAddr = "192.0.2.10:8080"
	if got := streamIdentity(request); got != "token:observer" {
		t.Fatalf("unexpected token identity %q", got)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil)
	request.RemoteAddr = "192.0.2.10:8080"
	if got := streamIdentity(request); got != "addr:192.0.2.10" {
		t.Fatalf("unexpected remote addr identity %q", got)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil)
	request.RemoteAddr = ""
	if got := streamIdentity(request); got != "anonymous" {
		t.Fatalf("unexpected anonymous identity %q", got)
	}
}
