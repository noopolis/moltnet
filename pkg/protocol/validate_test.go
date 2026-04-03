package protocol

import (
	"strings"
	"testing"
)

func TestValidateSendMessageRequest(t *testing.T) {
	t.Parallel()

	valid := SendMessageRequest{
		ID:     "msg_1",
		Target: Target{Kind: TargetKindRoom, RoomID: "research"},
		From:   Actor{Type: "agent", ID: "writer"},
		Parts:  []Part{{Kind: PartKindText, Text: "hello"}},
	}
	if err := ValidateSendMessageRequest(valid); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}

	tests := []SendMessageRequest{
		{Target: valid.Target, Parts: valid.Parts},
		{Target: valid.Target, From: valid.From},
		{ID: "bad id\n", Target: valid.Target, From: valid.From, Parts: valid.Parts},
		{Target: valid.Target, From: Actor{Type: "agent", ID: "bad id\n"}, Parts: valid.Parts},
		{Target: valid.Target, From: valid.From, Parts: []Part{{Kind: "mystery", Text: "hello"}}},
		{Target: valid.Target, From: valid.From, Parts: []Part{{Kind: PartKindURL, URL: "javascript:alert(1)"}}},
		{Target: Target{Kind: TargetKindRoom}, From: valid.From, Parts: valid.Parts},
	}

	for _, test := range tests {
		if err := ValidateSendMessageRequest(test); err == nil {
			t.Fatalf("expected invalid request %#v", test)
		}
	}
}

func TestValidateMessageID(t *testing.T) {
	t.Parallel()

	if err := ValidateMessageID("msg.valid-1:ok"); err != nil {
		t.Fatalf("expected valid message id, got %v", err)
	}
	if err := ValidateMessageID(""); err == nil {
		t.Fatal("expected empty message id error")
	}
	if err := ValidateMessageID("msg with spaces"); err == nil {
		t.Fatal("expected invalid character error")
	}
	if err := ValidateMessageID(strings.Repeat("m", MaxMessageIDLength+1)); err == nil {
		t.Fatal("expected overly long message id error")
	}
}

func TestValidateCreateRoomRequest(t *testing.T) {
	t.Parallel()

	if err := ValidateCreateRoomRequest(CreateRoomRequest{ID: "research", Members: []string{"alpha", "net_b:beta"}}); err != nil {
		t.Fatalf("expected valid room id, got %v", err)
	}
	if err := ValidateCreateRoomRequest(CreateRoomRequest{ID: "bad room"}); err == nil {
		t.Fatal("expected invalid room id error")
	}
	if err := ValidateCreateRoomRequest(CreateRoomRequest{ID: "research", Members: []string{"bad member\n"}}); err == nil {
		t.Fatal("expected invalid member id error")
	}
}

func TestValidateUpdateRoomMembersRequest(t *testing.T) {
	t.Parallel()

	if err := ValidateUpdateRoomMembersRequest(UpdateRoomMembersRequest{
		Add:    []string{"alpha", "molt://net_b/agents/beta"},
		Remove: []string{"gamma"},
	}); err != nil {
		t.Fatalf("expected valid update request, got %v", err)
	}
	if err := ValidateUpdateRoomMembersRequest(UpdateRoomMembersRequest{Add: []string{"bad\nmember"}}); err == nil {
		t.Fatal("expected invalid member update")
	}
}

func TestValidatePageRequest(t *testing.T) {
	t.Parallel()

	if err := ValidatePageRequest(PageRequest{Before: "msg_1"}); err != nil {
		t.Fatalf("expected valid page request, got %v", err)
	}
	if err := ValidatePageRequest(PageRequest{Before: "msg_1", After: "msg_2"}); err == nil {
		t.Fatal("expected before+after validation error")
	}
	if err := ValidatePageRequest(PageRequest{After: "bad cursor"}); err == nil {
		t.Fatal("expected invalid cursor format error")
	}
}

func TestValidateMemberID(t *testing.T) {
	t.Parallel()

	valid := []string{"alpha", "net_a:beta", "molt://net_a/agents/gamma"}
	for _, value := range valid {
		if err := ValidateMemberID(value); err != nil {
			t.Fatalf("expected valid member id %q, got %v", value, err)
		}
	}
	if err := ValidateMemberID("bad\nmember"); err == nil {
		t.Fatal("expected invalid member id error")
	}
}

func TestValidatePartURL(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"https://example.com/x", "http://example.com/x", "molt://local/artifacts/a"} {
		if err := ValidatePartURL(value); err != nil {
			t.Fatalf("expected valid part url %q, got %v", value, err)
		}
	}
	if err := ValidatePartURL("javascript:alert(1)"); err == nil {
		t.Fatal("expected invalid scheme error")
	}
}
