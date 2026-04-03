package protocol

import "testing"

func TestValidateTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target Target
		ok     bool
	}{
		{
			name:   "room ok",
			target: Target{Kind: TargetKindRoom, RoomID: "research"},
			ok:     true,
		},
		{
			name:   "thread ok",
			target: Target{Kind: TargetKindThread, ThreadID: "thr_1", RoomID: "research", ParentMessageID: "msg_1"},
			ok:     true,
		},
		{
			name:   "dm ok",
			target: Target{Kind: TargetKindDM, DMID: "dm_1", ParticipantIDs: []string{"orchestrator", "molt://remote/agents/researcher"}},
			ok:     true,
		},
		{
			name:   "room missing id",
			target: Target{Kind: TargetKindRoom},
		},
		{
			name:   "thread missing id",
			target: Target{Kind: TargetKindThread},
		},
		{
			name:   "thread missing room",
			target: Target{Kind: TargetKindThread, ThreadID: "thr_1"},
		},
		{
			name:   "thread invalid id",
			target: Target{Kind: TargetKindThread, ThreadID: "bad thread", RoomID: "research"},
		},
		{
			name:   "dm missing id",
			target: Target{Kind: TargetKindDM, ParticipantIDs: []string{"orchestrator", "researcher"}},
		},
		{
			name:   "dm missing participants",
			target: Target{Kind: TargetKindDM, DMID: "dm_1"},
		},
		{
			name:   "dm too few participants",
			target: Target{Kind: TargetKindDM, DMID: "dm_1", ParticipantIDs: []string{"orchestrator"}},
		},
		{
			name:   "dm invalid participant",
			target: Target{Kind: TargetKindDM, DMID: "dm_1", ParticipantIDs: []string{"orchestrator", "bad\nparticipant"}},
		},
		{
			name:   "unsupported kind",
			target: Target{Kind: "weird"},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateTarget(test.target)
			if test.ok && err != nil {
				t.Fatalf("expected success, got %v", err)
			}

			if !test.ok && err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestUniqueParticipantIDs(t *testing.T) {
	t.Parallel()

	participants := UniqueTrimmedStrings([]string{" writer ", "researcher", "writer", "", "researcher"})
	if len(participants) != 2 || participants[0] != "writer" || participants[1] != "researcher" {
		t.Fatalf("unexpected participants %#v", participants)
	}
}
