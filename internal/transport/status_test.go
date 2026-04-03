package transport

import (
	"errors"
	"net/http"
	"testing"
)

type testStatusError struct {
	status int
}

func (e testStatusError) Error() string {
	return "test"
}

func (e testStatusError) StatusCode() int {
	return e.status
}

func TestStatusForError(t *testing.T) {
	t.Parallel()

	if got := statusForError(nil); got != http.StatusOK {
		t.Fatalf("unexpected nil status %d", got)
	}
	if got := statusForError(testStatusError{status: http.StatusTeapot}); got != http.StatusTeapot {
		t.Fatalf("unexpected status error mapping %d", got)
	}
	if got := statusForError(errors.New("plain")); got != http.StatusInternalServerError {
		t.Fatalf("unexpected fallback status %d", got)
	}
}
