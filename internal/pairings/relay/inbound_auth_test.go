package relay

import (
	"context"
	"net/http"
	"testing"
)

func TestClientRejectsInboundMessageWithoutFrameCredential(t *testing.T) {
	handler, service := newInboundHandlerFixture(t)
	defer service.Close()

	client := NewClient("", "relay-connect-token", "correct-pair-token", "local", WithInboundHandler(handler))
	request := messageFrame(t)
	// Before B20, SetHandler stamped correct-pair-token from the receiver's
	// client configuration, so this relay-forged frame was persisted.
	response, _ := client.inboundResponse(context.Background(), request.header, request.body)
	if response.Status != http.StatusUnauthorized {
		t.Fatalf("response status = %d, want %d", response.Status, http.StatusUnauthorized)
	}
	if got := inboundMessageCount(t, service); got != 0 {
		t.Fatalf("stored message count = %d, want 0", got)
	}
}
