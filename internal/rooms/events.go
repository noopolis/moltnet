package rooms

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func (s *Service) publishEvent(event protocol.Event) {
	s.broker.Publish(event)
}

func eventIDForMessage(messageID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(messageID)))
	return "evt_msg_" + hex.EncodeToString(sum[:])
}

func (s *Service) conversationLifecycle(
	ctx context.Context,
	message protocol.Message,
) (store.AppendLifecycle, error) {
	switch message.Target.Kind {
	case protocol.TargetKindThread:
		thread, ok, err := s.getThread(ctx, message.Target.ThreadID)
		if err != nil {
			return store.AppendLifecycle{}, err
		}
		if ok && thread.MessageCount == 1 {
			return store.AppendLifecycle{Thread: &thread}, nil
		}
	case protocol.TargetKindDM:
		conversation, ok, err := s.getDirectConversation(ctx, message.Target.DMID)
		if err != nil {
			return store.AppendLifecycle{}, err
		}
		if ok && conversation.MessageCount == 1 {
			return store.AppendLifecycle{DM: &conversation}, nil
		}
	}

	return store.AppendLifecycle{}, nil
}

func (s *Service) setPairingStatus(pairingID string, status string) {
	s.setPairingRuntime(pairingID, pairingStatus{value: status}, false)
}

func (s *Service) setPairingError(pairingID string, message string) {
	s.setPairingRuntime(pairingID, pairingStatus{
		value: protocol.PairingStatusError,
		diagnostics: &protocol.PairingDiagnostics{
			CheckedAt: s.now().UTC(),
			Reason:    pairingDiagnosticRemoteRequestFailure,
			Message:   strings.TrimSpace(message),
		},
		checked: true,
	}, true)
}

func (s *Service) setPairingRuntime(pairingID string, next pairingStatus, replaceDiagnostics bool) {
	next.value = normalizePairingStatus(next.value)
	updatedAt := s.now().UTC()

	s.pairingsMu.Lock()
	previous := s.pairingStatuses[pairingID]
	if !replaceDiagnostics {
		next.diagnostics = clonePairingDiagnostics(previous.diagnostics)
		next.checked = previous.checked
		next.directMessages = previous.directMessages
		next.cursorPagination = previous.cursorPagination
	} else {
		next.diagnostics = clonePairingDiagnostics(next.diagnostics)
	}
	if previous.value == next.value &&
		previous.checked == next.checked &&
		previous.directMessages == next.directMessages &&
		previous.cursorPagination == next.cursorPagination &&
		pairingDiagnosticsEqual(previous.diagnostics, next.diagnostics) {
		previous.updatedAt = updatedAt
		s.pairingStatuses[pairingID] = previous
		s.pairingsMu.Unlock()
		return
	}
	next.updatedAt = updatedAt
	s.pairingStatuses[pairingID] = next

	var eventPairing *protocol.Pairing
	for _, pairing := range s.pairings {
		if pairing.ID == pairingID {
			pairing.Token = ""
			pairing.Status = next.value
			pairing.Diagnostics = clonePairingDiagnostics(next.diagnostics)
			copyPairing := pairing
			eventPairing = &copyPairing
			break
		}
	}
	s.pairingPublishMu.Lock()
	s.pairingsMu.Unlock()
	defer s.pairingPublishMu.Unlock()

	if eventPairing == nil {
		return
	}

	observability.Logger(s.lifecycleCtx, "rooms.pairing", "pairing_id", pairingID, "status", next.value).
		Info("pairing status updated")
	s.publishEvent(protocol.Event{
		ID:        s.nextID("evt"),
		Type:      protocol.EventTypePairingUpdated,
		NetworkID: s.networkID,
		Pairing:   eventPairing,
		CreatedAt: time.Now().UTC(),
	})
}
