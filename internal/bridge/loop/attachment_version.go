package loop

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

func validateReadyFrame(frame protocol.AttachmentFrame, networkID string) error {
	if err := requireAttachmentFrameVersion(frame); err != nil {
		return err
	}
	if strings.TrimSpace(frame.NetworkID) != strings.TrimSpace(networkID) {
		return fmt.Errorf("attachment READY network_id %q does not match %q", frame.NetworkID, networkID)
	}
	return nil
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
