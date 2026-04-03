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
	status = strings.TrimSpace(status)
	updatedAt := s.now().UTC()

	s.pairingsMu.Lock()
	previous := s.pairingStatuses[pairingID]
	if previous.value == status {
		previous.updatedAt = updatedAt
		s.pairingStatuses[pairingID] = previous
		s.pairingsMu.Unlock()
		return
	}
	s.pairingStatuses[pairingID] = pairingStatus{
		value:     status,
		updatedAt: updatedAt,
	}

	var eventPairing *protocol.Pairing
	for _, pairing := range s.pairings {
		if pairing.ID == pairingID {
			pairing.Token = ""
			pairing.Status = status
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

	observability.Logger(s.lifecycleCtx, "rooms.pairing", "pairing_id", pairingID, "status", status).
		Info("pairing status updated")
	s.publishEvent(protocol.Event{
		ID:        s.nextID("evt"),
		Type:      protocol.EventTypePairingUpdated,
		NetworkID: s.networkID,
		Pairing:   eventPairing,
		CreatedAt: time.Now().UTC(),
	})
}
