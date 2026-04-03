package signals

import (
	"testing"
	"time"
)

func TestDefaultCancelsContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := Default()
	cancel()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("expected signal context to be canceled")
	}
}
