package protocol

import "testing"

func TestAttachmentFrameFields(t *testing.T) {
	t.Parallel()

	frame := AttachmentFrame{
		Op:        AttachmentOpIdentify,
		Version:   AttachmentProtocolV1,
		NetworkID: "local",
		Agent: &Actor{
			Type: "agent",
			ID:   "researcher",
		},
		Capabilities: AttachmentCapabilities{
			Rooms:   true,
			Threads: true,
			DMs:     true,
		},
	}

	if frame.Op != AttachmentOpIdentify || frame.Version != AttachmentProtocolV1 {
		t.Fatalf("unexpected frame %#v", frame)
	}
	if frame.Agent == nil || frame.Agent.ID != "researcher" {
		t.Fatalf("unexpected agent %#v", frame.Agent)
	}
	if !frame.Capabilities.Rooms || !frame.Capabilities.Threads || !frame.Capabilities.DMs {
		t.Fatalf("unexpected capabilities %#v", frame.Capabilities)
	}
}
