package relay

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"
)

var errRelayResponseTooLarge = errors.New("relay response too large")

var inboundAllowedRequests = map[string]map[string]struct{}{
	http.MethodGet: {
		"/v1/network": {},
		"/v1/rooms":   {},
		"/v1/agents":  {},
	},
	http.MethodPost: {
		"/v1/messages": {},
	},
}

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
	c.inboundMu.RLock()
	inbound := c.inbound
	c.inboundMu.RUnlock()
	if inbound == nil {
		return unavailableInboundResponse(header)
	}
	return inbound.serve(ctx, header, payload)
}

func (h *InboundHandler) serve(ctx context.Context, header frameHeader, payload []byte) (response frameHeader, responseBody []byte) {
	writer := newBoundedResponseWriter(maxInboundResponseBodyBytes)
	if h == nil || h.handler == nil {
		http.Error(writer, "relay inbound handler unavailable", http.StatusNotImplemented)
		return responseFrame(header, writer.statusCode()), writer.body
	}
	if !isAllowedInboundRequest(header.Method, header.Path) {
		return inboundErrorResponse(header, "relay inbound request forbidden", http.StatusForbidden)
	}

	request, err := http.NewRequestWithContext(ctx, header.Method, header.Path, bytes.NewReader(payload))
	if err != nil {
		http.Error(writer, "invalid relay request", http.StatusBadRequest)
		return responseFrame(header, writer.statusCode()), writer.body
	}
	if token := strings.TrimSpace(h.token); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	defer func() {
		if recover() != nil {
			response, responseBody = inboundErrorResponse(header, "internal error", http.StatusInternalServerError)
		}
	}()
	h.handler.ServeHTTP(writer, request)
	if writer.tooLarge {
		return inboundErrorResponse(header, errRelayResponseTooLarge.Error(), http.StatusInternalServerError)
	}
	return responseFrame(header, writer.statusCode()), writer.body
}

func isAllowedInboundRequest(method, requestTarget string) bool {
	requestURL, err := url.ParseRequestURI(requestTarget)
	if err != nil || requestURL.Scheme != "" || requestURL.Host != "" || requestURL.RawPath != "" {
		return false
	}

	// Reject escaped paths rather than decoding them: this prevents encoded dot
	// segments or slashes from changing the path considered by the allowlist.
	cleanPath := path.Clean(requestURL.Path)
	if requestURL.Path != cleanPath {
		return false
	}
	_, allowed := inboundAllowedRequests[method][cleanPath]
	return allowed
}

func unavailableInboundResponse(header frameHeader) (frameHeader, []byte) {
	return inboundErrorResponse(header, "relay inbound handler unavailable", http.StatusNotImplemented)
}

func responseFrame(request frameHeader, status int) frameHeader {
	return frameHeader{Type: "res", ID: request.ID, Status: status}
}

func inboundErrorResponse(header frameHeader, message string, status int) (frameHeader, []byte) {
	writer := newBoundedResponseWriter(maxInboundResponseBodyBytes)
	http.Error(writer, message, status)
	return responseFrame(header, writer.statusCode()), writer.body
}

type boundedResponseWriter struct {
	header    http.Header
	body      []byte
	status    int
	wroteHead bool
	tooLarge  bool
	limit     int
}

func newBoundedResponseWriter(limit int) *boundedResponseWriter {
	return &boundedResponseWriter{header: make(http.Header), limit: limit}
}

func (w *boundedResponseWriter) Header() http.Header {
	return w.header
}

func (w *boundedResponseWriter) WriteHeader(status int) {
	if w.wroteHead {
		return
	}
	w.status = status
	w.wroteHead = true
}

func (w *boundedResponseWriter) Write(body []byte) (int, error) {
	if !w.wroteHead {
		w.WriteHeader(http.StatusOK)
	}
	if w.tooLarge || len(body) > w.limit-len(w.body) {
		w.tooLarge = true
		return 0, errRelayResponseTooLarge
	}
	w.body = append(w.body, body...)
	return len(body), nil
}

func (w *boundedResponseWriter) statusCode() int {
	if !w.wroteHead {
		return http.StatusOK
	}
	return w.status
}
