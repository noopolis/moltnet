package transport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

const maxSSEFieldLength = 128

var sseEventTypePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:+@/-]{0,127}$`)

func responseFlusher(response http.ResponseWriter) (http.Flusher, bool) {
	if recorder, ok := response.(*statusRecorder); ok {
		flusher, ok := recorder.ResponseWriter.(http.Flusher)
		return flusher, ok
	}
	flusher, ok := response.(http.Flusher)
	return flusher, ok
}

func clearStreamingWriteDeadline(response http.ResponseWriter) error {
	controller := http.NewResponseController(response)
	if err := controller.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

func readLimit(request *http.Request) int {
	value := strings.TrimSpace(request.URL.Query().Get("limit"))
	if value == "" {
		return 100
	}

	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 {
		return 100
	}

	if limit > 500 {
		return 500
	}

	return limit
}

func readLastEventID(request *http.Request) string {
	if value := normalizeEventCursor(request.Header.Get("Last-Event-ID")); value != "" {
		return value
	}

	return normalizeEventCursor(request.URL.Query().Get("last_event_id"))
}

func readPageRequest(request *http.Request) (protocol.PageRequest, error) {
	page := protocol.PageRequest{
		Before: strings.TrimSpace(request.URL.Query().Get("before")),
		After:  strings.TrimSpace(request.URL.Query().Get("after")),
		Limit:  readLimit(request),
	}
	if err := protocol.ValidatePageRequest(page); err != nil {
		return protocol.PageRequest{}, err
	}
	return page, nil
}

func decodeJSON(response http.ResponseWriter, request *http.Request, target any) error {
	defer request.Body.Close()

	request.Body = http.MaxBytesReader(response, request.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(request.Body)

	if err := decoder.Decode(target); err != nil {
		return err
	}

	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return errors.New("expected a single JSON object")
		}
		return err
	}

	return nil
}

func writeJSON(response http.ResponseWriter, status int, payload any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)

	if err := json.NewEncoder(response).Encode(payload); err != nil {
		transportLogger(responseContext(response), "transport.http").Error("write json response", "error", err)
	}
}

func writeError(response http.ResponseWriter, status int, err error) {
	requestID := strings.TrimSpace(response.Header().Get("X-Request-Id"))
	ctx := responseContext(response)
	if status >= http.StatusInternalServerError && err != nil {
		transportLogger(ctx, "transport.http", "status", status, "request_id", requestID, "error", err).
			Error("write error response")
	}

	payload := map[string]string{
		"error": publicErrorMessage(status, err),
		"code":  errorCodeForStatus(status),
	}
	if requestID != "" {
		payload["request_id"] = requestID
	}
	writeJSON(response, status, payload)
}

func normalizeEventCursor(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if err := protocol.ValidateMessageID(trimmed); err != nil {
		return ""
	}
	return trimmed
}

func normalizeSSEEventID(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if err := protocol.ValidateMessageID(trimmed); err != nil {
		return ""
	}
	return trimmed
}

func normalizeSSEEventType(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > maxSSEFieldLength {
		return ""
	}
	if !sseEventTypePattern.MatchString(trimmed) {
		return ""
	}
	return trimmed
}

func responseContext(response http.ResponseWriter) context.Context {
	if recorder, ok := response.(*statusRecorder); ok {
		return recorder.Context()
	}
	if provider, ok := response.(interface{ Context() context.Context }); ok {
		return provider.Context()
	}
	return context.Background()
}

func publicErrorMessage(status int, err error) string {
	if status >= http.StatusInternalServerError || err == nil {
		if text := strings.TrimSpace(http.StatusText(status)); text != "" {
			return strings.ToLower(text)
		}
		return "internal server error"
	}

	if text := strings.TrimSpace(err.Error()); text != "" {
		return text
	}
	return strings.ToLower(http.StatusText(status))
}

func errorCodeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusUnprocessableEntity:
		return "unprocessable_entity"
	case http.StatusBadGateway:
		return "bad_gateway"
	case http.StatusServiceUnavailable:
		return "service_unavailable"
	default:
		return "internal_error"
	}
}
