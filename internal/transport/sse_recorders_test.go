package transport

import (
	"net/http"
	"strings"
	"sync"
)

type signalRecorder struct {
	header http.Header
	mu     sync.RWMutex
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
	r.mu.Lock()
	written, err := r.body.Write(bytes)
	matched := strings.Contains(r.body.String(), r.needle)
	r.mu.Unlock()
	if matched {
		r.once.Do(func() { close(r.signal) })
	}
	return written, err
}

func (r *signalRecorder) String() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.body.String()
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
