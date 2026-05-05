package transport

import (
	"fmt"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func requireAttachmentFrameVersion(frame protocol.AttachmentFrame) error {
	version := strings.TrimSpace(frame.Version)
	if version == "" {
		return fmt.Errorf("attachment %s version is required", attachmentFrameOp(frame))
	}
	if version != protocol.AttachmentProtocolV1 {
		return attachmentFrameVersionMismatchError(frame)
	}
	return nil
}

func validateAttachmentFrameVersion(frame protocol.AttachmentFrame) error {
	if strings.TrimSpace(frame.Version) == "" && attachmentFrameMayOmitVersion(frame) {
		return nil
	}
	return requireAttachmentFrameVersion(frame)
}

func attachmentFrameMayOmitVersion(frame protocol.AttachmentFrame) bool {
	switch frame.Op {
	case protocol.AttachmentOpAck, protocol.AttachmentOpPong:
		return true
	default:
		return false
	}
}

func writeAttachmentError(writer *attachmentWriter, message string) error {
	return writer.write(protocol.AttachmentFrame{
		Op:      protocol.AttachmentOpError,
		Version: protocol.AttachmentProtocolV1,
		Error:   strings.TrimSpace(message),
	})
}

func writeAttachmentFrameVersionError(writer *attachmentWriter, err error) error {
	if writeErr := writeAttachmentError(writer, err.Error()); writeErr != nil {
		return writeErr
	}
	return err
}

func attachmentFrameVersionMismatchError(frame protocol.AttachmentFrame) error {
	return fmt.Errorf(
		"attachment %s version %q does not match %q",
		attachmentFrameOp(frame),
		frame.Version,
		protocol.AttachmentProtocolV1,
	)
}

func attachmentFrameOp(frame protocol.AttachmentFrame) string {
	op := strings.TrimSpace(frame.Op)
	if op == "" {
		return "frame"
	}
	return op
}
