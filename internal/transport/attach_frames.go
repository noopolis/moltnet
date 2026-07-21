package transport

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func readAttachmentFrame(connection *websocket.Conn, readTimeout time.Duration) (protocol.AttachmentFrame, error) {
	if err := connection.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
		return protocol.AttachmentFrame{}, fmt.Errorf("set attachment read deadline: %w", err)
	}

	messageType, payload, err := connection.ReadMessage()
	if err != nil {
		return protocol.AttachmentFrame{}, err
	}
	if messageType != websocket.TextMessage {
		return protocol.AttachmentFrame{}, invalidAttachmentFrameError("attachment protocol only accepts text JSON frames", nil)
	}

	var frame protocol.AttachmentFrame
	if err := json.Unmarshal(payload, &frame); err != nil {
		return protocol.AttachmentFrame{}, invalidAttachmentFrameError("attachment frame must be valid JSON", err)
	}

	return frame, nil
}

func attachmentHeartbeatInterval() time.Duration {
	return time.Duration(attachmentHeartbeatIntervalMS) * time.Millisecond
}

func attachmentReadTimeout() time.Duration {
	return attachmentHeartbeatInterval() * 2
}

type attachmentFrameError struct {
	message string
	err     error
}

func (e *attachmentFrameError) Error() string {
	return e.message
}

func (e *attachmentFrameError) Unwrap() error {
	return e.err
}

func invalidAttachmentFrameError(message string, err error) error {
	return &attachmentFrameError{
		message: strings.TrimSpace(message),
		err:     err,
	}
}

type attachmentClientError string

func (e attachmentClientError) Error() string {
	message := strings.TrimSpace(string(e))
	if message == "" {
		return "attachment client error"
	}
	return "attachment client error: " + message
}

func attachmentFrameErrorMessage(err error) (string, bool) {
	var frameErr *attachmentFrameError
	if !errors.As(err, &frameErr) || strings.TrimSpace(frameErr.message) == "" {
		return "", false
	}
	return frameErr.message, true
}
