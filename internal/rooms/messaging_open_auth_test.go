package rooms

import (
	"context"
	"errors"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestOpenModeWriteTokenRejectsUnpairedRemoteOriginActor(t *testing.T) {
	t.Parallel()

	service := newTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); err != nil {
		t.Fatal(err)
	}
	request := protocol.SendMessageRequest{
		ID:     "msg_remote_spoof",
		Origin: protocol.MessageOrigin{NetworkID: "remote", MessageID: "msg_remote_1"},
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:   protocol.Actor{Type: "agent", ID: "remote-agent", NetworkID: "remote"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "spoof"}},
	}

	writeOnly := openContextWithClaims(staticClaims("writer", authn.ScopeWrite))
	if _, err := service.SendMessageContext(writeOnly, request); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("expected write-only remote-origin rejection, got %v", err)
	}

	pairWriter := openContextWithClaims(staticClaims("pair-writer", authn.ScopeWrite, authn.ScopePair))
	if _, err := service.SendMessageContext(pairWriter, request); err != nil {
		t.Fatalf("expected pair-authorized remote-origin send, got %v", err)
	}

	page, err := service.ListRoomMessages("research", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages() error = %v", err)
	}
	if len(page.Messages) != 1 ||
		page.Messages[0].From.NetworkID != "remote" ||
		page.Messages[0].Origin.NetworkID != "remote" {
		t.Fatalf("unexpected accepted remote-origin message %#v", page.Messages)
	}
}

func TestPairRemoteOriginRequiresExplicitConsistentActorNetwork(t *testing.T) {
	t.Parallel()

	service := newTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); err != nil {
		t.Fatal(err)
	}

	request := protocol.SendMessageRequest{
		ID:     "msg_remote_inconsistent",
		Origin: protocol.MessageOrigin{NetworkID: "remote", MessageID: "msg_remote_1"},
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From: protocol.Actor{
			Type: "agent",
			ID:   "luna",
			FQID: protocol.AgentFQID("remote", "luna"),
		},
		Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "spoof"}},
	}

	pairContext := authn.WithClaims(context.Background(), staticClaims("pair", authn.ScopePair))
	if _, err := service.SendMessageContext(pairContext, request); !errors.Is(err, ErrAgentConflict) {
		t.Fatalf("expected inconsistent remote actor rejection, got %v", err)
	}

	request.From.NetworkID = "remote"
	if _, err := service.SendMessageContext(pairContext, request); err != nil {
		t.Fatalf("expected explicit consistent remote actor, got %v", err)
	}

	page, err := service.ListRoomMessages("research", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages() error = %v", err)
	}
	if len(page.Messages) != 1 || page.Messages[0].From.NetworkID != "remote" {
		t.Fatalf("unexpected stored remote actor %#v", page.Messages)
	}
}

func TestPairRemoteOriginRequiresExplicitConsistentHumanNetwork(t *testing.T) {
	t.Parallel()

	service := newTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); err != nil {
		t.Fatal(err)
	}

	request := protocol.SendMessageRequest{
		ID:     "msg_remote_human",
		Origin: protocol.MessageOrigin{NetworkID: "remote", MessageID: "msg_remote_human"},
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From: protocol.Actor{
			Type: "human",
			ID:   "operator",
		},
		Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "remote human"}},
	}

	pairContext := authn.WithClaims(context.Background(), staticClaims("pair", authn.ScopePair))
	if _, err := service.SendMessageContext(pairContext, request); !errors.Is(err, ErrAgentConflict) {
		t.Fatalf("expected inconsistent remote human rejection, got %v", err)
	}

	request.From.NetworkID = "remote"
	if _, err := service.SendMessageContext(pairContext, request); err != nil {
		t.Fatalf("expected explicit consistent remote human, got %v", err)
	}

	page, err := service.ListRoomMessages("research", "", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages() error = %v", err)
	}
	if len(page.Messages) != 1 || page.Messages[0].From.NetworkID != "remote" {
		t.Fatalf("unexpected stored remote human %#v", page.Messages)
	}
}

func TestRemoteOriginHumanRequiresPairScope(t *testing.T) {
	t.Parallel()

	service := newTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{ID: "research"}); err != nil {
		t.Fatal(err)
	}

	request := protocol.SendMessageRequest{
		ID:     "msg_remote_human_write",
		Origin: protocol.MessageOrigin{NetworkID: "remote", MessageID: "msg_remote_human_write"},
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From: protocol.Actor{
			Type:      "human",
			ID:        "operator",
			NetworkID: "remote",
		},
		Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "remote human"}},
	}

	writeContext := authn.WithClaims(context.Background(), staticClaims("writer", authn.ScopeWrite))
	if _, err := service.SendMessageContext(writeContext, request); !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("expected remote human write-only rejection, got %v", err)
	}

	pairContext := authn.WithClaims(context.Background(), staticClaims("pair", authn.ScopePair))
	if _, err := service.SendMessageContext(pairContext, request); err != nil {
		t.Fatalf("expected remote human pair acceptance, got %v", err)
	}
}
