package rooms

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// newCausalTestService wires a Service to an in-memory CausalWriter so tests
// can inspect exactly which JSONL lines message.accepted stamping produced.
func newCausalTestService(t *testing.T) (*Service, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	memory := store.NewMemoryStore()
	service := NewService(ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local",
		NetworkName:       "Local",
		Version:           "test",
		Store:             memory,
		Messages:          memory,
		Broker:            events.NewBroker(),
		CausalWriter:      observability.NewCausalWriter(buf),
	})
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID:      "general",
		Members: []string{"writer"},
	}); err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}
	return service, buf
}

func causalLines(t *testing.T, buf *bytes.Buffer) []protocol.CausalEvent {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
	var events []protocol.CausalEvent
	for scanner.Scan() {
		var event protocol.CausalEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("unmarshal causal line %q: %v", scanner.Text(), err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan causal lines: %v", err)
	}
	return events
}

func TestStampMessageAcceptedRecordsAValidatingRecord(t *testing.T) {
	t.Setenv(causalRunIDEnv, "run_test_1")
	service, buf := newCausalTestService(t)

	request := protocol.SendMessageRequest{
		ID:     "msg_1",
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "general"},
		From:   protocol.Actor{Type: "agent", ID: "writer"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "hello"}},
	}
	ctx := authn.WithClaims(context.Background(), staticClaims("writer", authn.ScopeWrite))

	if _, err := service.SendMessageContext(ctx, request); err != nil {
		t.Fatalf("SendMessageContext() error = %v", err)
	}

	recorded := causalLines(t, buf)
	if len(recorded) != 1 {
		t.Fatalf("expected exactly 1 causal record, got %d: %#v", len(recorded), recorded)
	}
	event := recorded[0]
	if err := event.Validate(); err != nil {
		t.Fatalf("recorded event failed Validate(): %v", err)
	}

	if event.RunID != "run_test_1" {
		t.Fatalf("RunID = %q, want %q", event.RunID, "run_test_1")
	}
	if event.EventID != "moltnet:msg_1" {
		t.Fatalf("EventID = %q, want %q", event.EventID, "moltnet:msg_1")
	}
	if event.Emitter.System != protocol.CausalSystemMoltnet {
		t.Fatalf("Emitter.System = %q, want %q", event.Emitter.System, protocol.CausalSystemMoltnet)
	}
	if event.Emitter.StreamID != "network:local" {
		t.Fatalf("Emitter.StreamID = %q, want %q", event.Emitter.StreamID, "network:local")
	}
	if event.Emitter.Seq != 1 {
		t.Fatalf("Emitter.Seq = %d, want 1", event.Emitter.Seq)
	}
	if event.Type != protocol.EventTypeMessageAccepted {
		t.Fatalf("Type = %q, want %q", event.Type, protocol.EventTypeMessageAccepted)
	}
	if event.CauseEventIDs == nil || len(event.CauseEventIDs) != 0 {
		t.Fatalf("CauseEventIDs = %#v, want a non-nil empty slice", event.CauseEventIDs)
	}

	var payload protocol.MessageAcceptedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.MessageID != "msg_1" {
		t.Fatalf("payload.MessageID = %q, want %q", payload.MessageID, "msg_1")
	}
	if payload.PolicyDecision != protocol.PolicyDecisionAccepted {
		t.Fatalf("payload.PolicyDecision = %q, want %q", payload.PolicyDecision, protocol.PolicyDecisionAccepted)
	}
	wantParts, err := json.Marshal(request.Parts)
	if err != nil {
		t.Fatalf("marshal expected parts: %v", err)
	}
	wantSum := sha256.Sum256(wantParts)
	if payload.ContentSHA256 != hex.EncodeToString(wantSum[:]) {
		t.Fatalf("payload.ContentSHA256 = %q, want %q", payload.ContentSHA256, hex.EncodeToString(wantSum[:]))
	}
}

func TestStampMessageAcceptedPrincipalIDComesFromClaimsNotForgedFrom(t *testing.T) {
	t.Setenv(causalRunIDEnv, "run_test_2")
	service, buf := newCausalTestService(t)

	request := protocol.SendMessageRequest{
		ID:     "msg_forged",
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "general"},
		// A forged display identity: this must never leak into principal_id.
		From:  protocol.Actor{Type: "agent", ID: "someone-else-entirely", Name: "Not The Real Sender"},
		Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "hi"}},
	}
	// Admin+write scope is granted to bypass room membership entirely, so
	// this test isolates principal_id derivation from write-policy checks:
	// the forged From identity is never a room member and must still be
	// irrelevant to what gets stamped as principal_id.
	ctx := authn.WithClaims(context.Background(), staticClaims("real-writer", authn.ScopeWrite, authn.ScopeAdmin))

	if _, err := service.SendMessageContext(ctx, request); err != nil {
		t.Fatalf("SendMessageContext() error = %v", err)
	}

	recorded := causalLines(t, buf)
	if len(recorded) != 1 {
		t.Fatalf("expected exactly 1 causal record, got %d", len(recorded))
	}
	if got, want := recorded[0].PrincipalID, "operator:token:real-writer"; got != want {
		t.Fatalf("PrincipalID = %q, want %q (authenticated claims, not request.From)", got, want)
	}
	if recorded[0].PrincipalID == request.From.ID {
		t.Fatalf("PrincipalID must never equal the claimed (forged) From identity")
	}
}

func TestStampMessageAcceptedPrincipalIDIsAnonymousWithoutClaims(t *testing.T) {
	t.Setenv(causalRunIDEnv, "run_test_3")
	service, buf := newCausalTestService(t)

	request := protocol.SendMessageRequest{
		ID:     "msg_no_claims",
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "general"},
		From:   protocol.Actor{Type: "agent", ID: "whoever-i-claim-to-be"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "hi"}},
	}

	if _, err := service.SendMessage(request); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	recorded := causalLines(t, buf)
	if len(recorded) != 1 {
		t.Fatalf("expected exactly 1 causal record, got %d", len(recorded))
	}
	if recorded[0].PrincipalID != "system:moltnet.anonymous" {
		t.Fatalf("PrincipalID = %q, want %q for an unauthenticated request", recorded[0].PrincipalID, "system:moltnet.anonymous")
	}
	if recorded[0].PrincipalID == request.From.ID {
		t.Fatalf("PrincipalID must never equal request.From even when unauthenticated")
	}
}

func TestStampMessageAcceptedSkipsDuplicateMessages(t *testing.T) {
	t.Setenv(causalRunIDEnv, "run_test_4")
	service, buf := newCausalTestService(t)

	request := protocol.SendMessageRequest{
		ID:     "msg_dup",
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "general"},
		From:   protocol.Actor{Type: "agent", ID: "writer"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "hi"}},
	}

	first, err := service.SendMessage(request)
	if err != nil {
		t.Fatalf("first SendMessage() error = %v", err)
	}
	second, err := service.SendMessage(request)
	if err != nil {
		t.Fatalf("second (duplicate) SendMessage() error = %v", err)
	}
	if !first.Accepted || !second.Accepted || first.EventID != second.EventID {
		t.Fatalf("expected both sends to report the same accepted event, got first=%#v second=%#v", first, second)
	}

	recorded := causalLines(t, buf)
	if len(recorded) != 1 {
		t.Fatalf("expected exactly 1 causal record despite 2 sends of the same message, got %d: %#v", len(recorded), recorded)
	}
}

func TestStampMessageAcceptedSeqIsContiguousAcrossMessages(t *testing.T) {
	t.Setenv(causalRunIDEnv, "run_test_5")
	service, buf := newCausalTestService(t)

	for index, messageID := range []string{"msg_a", "msg_b", "msg_c"} {
		request := protocol.SendMessageRequest{
			ID:     messageID,
			Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "general"},
			From:   protocol.Actor{Type: "agent", ID: "writer"},
			Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: messageID}},
		}
		if _, err := service.SendMessage(request); err != nil {
			t.Fatalf("SendMessage(%d) error = %v", index, err)
		}
	}

	recorded := causalLines(t, buf)
	if len(recorded) != 3 {
		t.Fatalf("expected 3 causal records, got %d: %#v", len(recorded), recorded)
	}
	for index, event := range recorded {
		wantSeq := index + 1
		if event.Emitter.Seq != wantSeq {
			t.Fatalf("record %d: Emitter.Seq = %d, want %d", index, event.Emitter.Seq, wantSeq)
		}
	}
}

func TestStampMessageAcceptedFallsBackToUnsetRunWithoutRunID(t *testing.T) {
	t.Setenv(causalRunIDEnv, "")
	service, buf := newCausalTestService(t)

	request := protocol.SendMessageRequest{
		ID:     "msg_no_run_id",
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "general"},
		From:   protocol.Actor{Type: "agent", ID: "writer"},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: "hi"}},
	}

	if _, err := service.SendMessage(request); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	// Aligns moltnet with mneme's resolveCausalRunId and daimon's
	// resolveRunId: an unset NOOPOLIS_RUN_ID falls back to emitting under
	// "unset-run" rather than moltnet silently writing nothing while the
	// other two authorities still emit.
	recorded := causalLines(t, buf)
	if len(recorded) != 1 {
		t.Fatalf("expected exactly 1 causal record even without %s set, got %d: %#v", causalRunIDEnv, len(recorded), recorded)
	}
	if recorded[0].RunID != causalUnsetRunID {
		t.Fatalf("RunID = %q, want %q", recorded[0].RunID, causalUnsetRunID)
	}
}

func TestStampMessageAcceptedCarriesCauseEventIDs(t *testing.T) {
	t.Setenv(causalRunIDEnv, "run_test_6")
	service, buf := newCausalTestService(t)

	request := protocol.SendMessageRequest{
		ID:            "msg_with_cause",
		Target:        protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "general"},
		From:          protocol.Actor{Type: "agent", ID: "writer"},
		Parts:         []protocol.Part{{Kind: protocol.PartKindText, Text: "hi"}},
		CauseEventIDs: []string{"daimon:turn_output_1"},
	}
	if _, err := service.SendMessage(request); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	recorded := causalLines(t, buf)
	if len(recorded) != 1 {
		t.Fatalf("expected exactly 1 causal record, got %d", len(recorded))
	}
	if len(recorded[0].CauseEventIDs) != 1 || recorded[0].CauseEventIDs[0] != "daimon:turn_output_1" {
		t.Fatalf("CauseEventIDs = %#v, want [daimon:turn_output_1]", recorded[0].CauseEventIDs)
	}
}
