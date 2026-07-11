package rooms

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// causalRunIDEnv is the environment variable every Noopolis authority reads
// the shared run_id from (specs/causal-event.v1.schema.json). It is never
// sourced from request payloads or model output.
const causalRunIDEnv = "NOOPOLIS_RUN_ID"

// causalPrincipalAnonymous is the principal_id stamped for the
// unauthenticated dev-mode case (no authn.Claims on the context at all).
// It is a system: principal per the grammar in specs/CAUSAL.md §3, never
// the bare string "anonymous": conformance rejects bare ids.
const causalPrincipalAnonymous = "system:moltnet.anonymous"

// causalPrincipal derives the causal-event principal_id for ctx per the
// principal grammar (specs/CAUSAL.md §3): agent:<agentId> for agent
// tokens, operator:<credentialKey> for static/operator credentials, and
// system:moltnet.anonymous only when there are no authn.Claims on ctx at
// all (unauthenticated dev-mode).
//
// This is derived exclusively from authn.Claims threaded onto ctx by the
// auth layer, or their absence. It never reads request.From, message
// parts, or any other caller-supplied field: those are claimed display
// identities, not authenticated ones, and are not trusted for causal
// attribution.
func causalPrincipal(ctx context.Context) string {
	claims, hasClaims := authn.ClaimsFromContext(ctx)
	if !hasClaims {
		return causalPrincipalAnonymous
	}
	if claims.AgentToken() {
		if agentIDs := claims.AgentIDs(); len(agentIDs) > 0 {
			return "agent:" + agentIDs[0]
		}
	}
	if credentialKey := strings.TrimSpace(claims.CredentialKey); credentialKey != "" {
		return "operator:" + credentialKey
	}
	return causalPrincipalAnonymous
}

// stampMessageAccepted records a message.accepted causal event for a message
// that has just durably landed. It must only be called after a successful,
// non-duplicate append: the caller (SendMessageContext) skips this entirely
// on the ErrDuplicateMessage early-return path, so a retried duplicate send
// never produces a second causal record.
//
// Stamping is best-effort with respect to the caller's happy path: a
// misconfigured or failing causal writer logs and returns without stamping,
// it never fails the message send itself. This keeps causal observability
// additive rather than a new way for core messaging to break.
func (s *Service) stampMessageAccepted(ctx context.Context, message protocol.Message, request protocol.SendMessageRequest) {
	if s.causalWriter == nil {
		return
	}

	logger := observability.Logger(ctx, "rooms.causal", "message_id", message.ID)

	runID := strings.TrimSpace(os.Getenv(causalRunIDEnv))
	if runID == "" {
		logger.Warn("skipping message.accepted causal stamp: " + causalRunIDEnv + " is not set")
		return
	}

	payload, err := messageAcceptedPayload(message)
	if err != nil {
		logger.Error("failed to build message.accepted causal payload", "error", err.Error())
		return
	}

	event := protocol.CausalEvent{
		Version: protocol.CausalEventVersion,
		RunID:   runID,
		EventID: protocol.MessageEventID(message.ID),
		Emitter: protocol.CausalEmitter{
			System:   protocol.CausalSystemMoltnet,
			StreamID: observability.NetworkStreamID(s.networkID),
		},
		Type: protocol.EventTypeMessageAccepted,
		// principal_id is the authenticated caller identity derived by
		// causalPrincipal from authn.Claims threaded through context by the
		// auth layer. It is never request.From: that field is a claimed
		// display identity and is not trusted for causal attribution.
		PrincipalID:   causalPrincipal(ctx),
		RecordedAt:    s.now().UTC(),
		CauseEventIDs: causeEventIDs(request.CauseEventIDs),
		Payload:       payload,
	}

	if _, err := s.causalWriter.Append(event); err != nil {
		logger.Error("failed to append message.accepted causal event", "error", err.Error())
	}
}

// stampMessageDenied records a message.denied causal event for a message
// that was rejected by room write policy (canWriteRoom returning false in
// SendMessageContext). The caller passes messageID and reason explicitly
// because a denial never reaches the point where a protocol.Message is
// constructed: this keeps deny paths ledger-visible instead of a silent
// drop, matching stampMessageAccepted's best-effort semantics (a
// misconfigured or failing causal writer logs and returns without
// stamping; it never turns a denial into a different kind of failure).
func (s *Service) stampMessageDenied(
	ctx context.Context,
	messageID string,
	request protocol.SendMessageRequest,
	reason string,
) {
	if s.causalWriter == nil {
		return
	}

	logger := observability.Logger(ctx, "rooms.causal", "message_id", messageID)

	runID := strings.TrimSpace(os.Getenv(causalRunIDEnv))
	if runID == "" {
		logger.Warn("skipping message.denied causal stamp: " + causalRunIDEnv + " is not set")
		return
	}

	payload, err := messageDeniedPayload(request, reason)
	if err != nil {
		logger.Error("failed to build message.denied causal payload", "error", err.Error())
		return
	}

	event := protocol.CausalEvent{
		Version: protocol.CausalEventVersion,
		RunID:   runID,
		EventID: protocol.MessageEventID(messageID) + ".denied",
		Emitter: protocol.CausalEmitter{
			System:   protocol.CausalSystemMoltnet,
			StreamID: observability.NetworkStreamID(s.networkID),
		},
		Type: protocol.EventTypeMessageDenied,
		// principal_id is the authenticated caller identity, exactly as for
		// stampMessageAccepted above: never request.From.
		PrincipalID:   causalPrincipal(ctx),
		RecordedAt:    s.now().UTC(),
		CauseEventIDs: causeEventIDs(request.CauseEventIDs),
		Payload:       payload,
	}

	if _, err := s.causalWriter.Append(event); err != nil {
		logger.Error("failed to append message.denied causal event", "error", err.Error())
	}
}

func messageDeniedPayload(request protocol.SendMessageRequest, reason string) (json.RawMessage, error) {
	// content_sha256 is computed the same way as messageAcceptedPayload:
	// over the canonically-serialized request Parts (encoding/json sorts
	// object keys when marshaling map[string]any), so a denied send and an
	// accepted send of identical content hash identically.
	partsJSON, err := json.Marshal(request.Parts)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(partsJSON)

	payload := protocol.MessageDeniedPayload{
		Target:         request.Target,
		Reason:         strings.TrimSpace(reason),
		ContentSHA256:  hex.EncodeToString(sum[:]),
		PolicyDecision: protocol.PolicyDecisionDenied,
	}
	return json.Marshal(payload)
}

func messageAcceptedPayload(message protocol.Message) (json.RawMessage, error) {
	// content_sha256 is computed over the canonically-serialized message
	// Parts. encoding/json sorts object keys when marshaling map[string]any
	// (protocol.Part.Data), so this JSON encoding is stable for a given Parts
	// value.
	partsJSON, err := json.Marshal(message.Parts)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(partsJSON)

	payload := protocol.MessageAcceptedPayload{
		MessageID:      message.ID,
		Target:         message.Target,
		ContentSHA256:  hex.EncodeToString(sum[:]),
		PolicyDecision: protocol.PolicyDecisionAccepted,
	}
	return json.Marshal(payload)
}

// causeEventIDs returns a non-nil copy of ids: protocol.CausalEvent.Validate
// requires cause_event_ids to be present (possibly empty), never nil.
func causeEventIDs(ids []string) []string {
	if len(ids) == 0 {
		return []string{}
	}
	return append([]string(nil), ids...)
}
