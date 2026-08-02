package relay

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
)

// InboundHandler serves relay requests through an injected HTTP handler.
type InboundHandler struct {
	handler http.Handler
	token   string
}

// NewInboundHandler creates an inbound relay request handler. pairingToken is
// attached to each synthetic request as a bearer Authorization header.
func NewInboundHandler(handler http.Handler, pairingToken string) *InboundHandler {
	return &InboundHandler{handler: handler, token: pairingToken}
}

func (c *Client) inboundResponse(ctx context.Context, header frameHeader, payload []byte) (frameHeader, []byte) {
	if c.inbound == nil {
		return unavailableInboundResponse(header)
	}
	return c.inbound.serve(ctx, header, payload)
}

func (h *InboundHandler) serve(ctx context.Context, header frameHeader, payload []byte) (response frameHeader, responseBody []byte) {
	recorder := httptest.NewRecorder()
	if h == nil || h.handler == nil {
		http.Error(recorder, "relay inbound handler unavailable", http.StatusNotImplemented)
		return responseFrame(header, recorder.Code), recorder.Body.Bytes()
	}

	request, err := http.NewRequestWithContext(ctx, header.Method, header.Path, bytes.NewReader(payload))
	if err != nil {
		http.Error(recorder, "invalid relay request", http.StatusBadRequest)
		return responseFrame(header, recorder.Code), recorder.Body.Bytes()
	}
	if token := strings.TrimSpace(h.token); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	defer func() {
		if recover() != nil {
			recorder = httptest.NewRecorder()
			http.Error(recorder, "internal error", http.StatusInternalServerError)
			response = responseFrame(header, recorder.Code)
			responseBody = recorder.Body.Bytes()
		}
	}()
	h.handler.ServeHTTP(recorder, request)
	return responseFrame(header, recorder.Code), recorder.Body.Bytes()
}

func unavailableInboundResponse(header frameHeader) (frameHeader, []byte) {
	recorder := httptest.NewRecorder()
	http.Error(recorder, "relay inbound handler unavailable", http.StatusNotImplemented)
	return responseFrame(header, recorder.Code), recorder.Body.Bytes()
}

func responseFrame(request frameHeader, status int) frameHeader {
	return frameHeader{Type: "res", ID: request.ID, Status: status}
}
