package protocol

import "testing"

func TestCanonicalIDs(t *testing.T) {
	t.Parallel()

	if got := RoomFQID("local_lab", "research"); got != "molt://local_lab/rooms/research" {
		t.Fatalf("unexpected room fqid %q", got)
	}

	if got := DMFQID("local_lab", "dm-1"); got != "molt://local_lab/dms/dm-1" {
		t.Fatalf("unexpected dm fqid %q", got)
	}

	if got := AgentFQID("local_lab", "writer"); got != "molt://local_lab/agents/writer" {
		t.Fatalf("unexpected agent fqid %q", got)
	}
}
