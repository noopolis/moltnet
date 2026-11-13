package rooms

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/noopolis/moltnet/internal/automation"
	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func (s *Service) SendMessage(request protocol.SendMessageRequest) (protocol.MessageAccepted, error) {
	return s.SendMessageContext(context.Background(), request)
}

func (s *Service) SendMessageContext(ctx context.Context, request protocol.SendMessageRequest) (protocol.MessageAccepted, error) {
	if strings.TrimSpace(request.Target.Kind) == protocol.TargetKindDM && s.disableDirectMessages {
		return protocol.MessageAccepted{}, directMessagesDisabledError()
	}
	if request.From.Type == "human" && !s.allowHumanIngress {
		return protocol.MessageAccepted{}, humanIngressDisabledError()
	}
	request.Origin = canonicalOriginIDs(request.Origin)
	if pairing, relayed := s.pairingForPairScopedContext(ctx); relayed {
		request = stripPeerTestimony(request)
		if isHubRequest(request) {
			return s.receiveHubMessage(ctx, request)
		}
		if pairing.IsRoomLink() {
			return protocol.MessageAccepted{}, hubForbiddenError("a room link carries only signed hub room messages")
		}
	} else {
		// Envelopes and receipts are peer statements; local callers never
		// supply them.
		request.Envelope, request.Receipt = nil, nil
	}
	if err := s.validateSenderIdentity(ctx, request.From, request.Origin); err != nil {
		return protocol.MessageAccepted{}, err
	}
	inboundEnvelope, err := s.verifyInboundEnvelope(ctx, request)
	if err != nil {
		return protocol.MessageAccepted{}, err
	}

	now, err := s.now()
	if err != nil {
		return protocol.MessageAccepted{}, err
	}

	messageID := strings.TrimSpace(request.ID)
	if messageID == "" {
		messageID = newPrefixedID("msg")
	}

	if err := validateSendMessageRequest(request); err != nil {
		return protocol.MessageAccepted{}, err
	}

	from := protocol.NormalizeActor(s.networkID, request.From)
	// CredentialBound is server authority, never caller testimony. The request
	// uses the same Actor wire type as stored messages, so overwrite it after
	// normalization even when a caller supplied true.
	from.CredentialBound = s.senderCredentialBound(ctx, from)
	if err := s.validateDMSenderTopology(request.Target, from); err != nil {
		return protocol.MessageAccepted{}, err
	}
	if request.Target.Kind == protocol.TargetKindRoom || request.Target.Kind == protocol.TargetKindThread {
		if err := s.enforceTargetWritePolicy(ctx, request.Target, from); err != nil {
			// A canWriteRoom denial (non-member / write-policy rejection)
			// must always be ledger-visible, never a silent drop: stamp
			// message.denied before returning the existing error. Other
			// enforceTargetWritePolicy failures (e.g. unknown room) are not
			// write-policy denials and are left unstamped.
			if errors.Is(err, ErrWriteForbidden) {
				s.stampMessageDenied(ctx, messageID, request, err.Error(), now)
			}
			return protocol.MessageAccepted{}, err
		}
	}

	mentions, err := s.resolveMentions(ctx, request.Target, protocol.NormalizeMentions(request.Parts, request.Mentions))
	if err != nil {
		return protocol.MessageAccepted{}, err
	}

	// Authorized: only now may first contact pin the origin's key.
	if err := s.commitFirstContactPin(ctx, inboundEnvelope); err != nil {
		return protocol.MessageAccepted{}, err
	}
	if inboundEnvelope != nil {
		ctx = store.WithMessageEnvelope(ctx, inboundEnvelope.record)
	}

	target := s.normalizeTarget(request.Target, from)
	origin := s.normalizeOrigin(ctx, request.Origin, messageID)
	message := protocol.Message{
		ReplyTo:      request.ReplyTo,
		DiscussionID: request.DiscussionID,
		ID:           messageID,
		NetworkID:    s.networkID,
		Origin:       origin,
		Target:       target,
		From:         from,
		Parts:        append([]protocol.Part(nil), request.Parts...),
		Mentions:     mentions,
		CreatedAt:    now,
	}

	event := protocol.Event{
		ID:        eventIDForMessage(message.ID),
		Type:      protocol.EventTypeMessageCreated,
		NetworkID: s.networkID,
		Message:   &message,
		CreatedAt: now,
	}

	var lifecycle store.AppendLifecycle
	appendMessage := func() (bool, error) {
		event.Message = &message
		lifecycle, err = s.appendAndPublishMessage(ctx, request, message, event)
		return err == nil, err
	}
	if s.automation != nil && message.Target.Kind == protocol.TargetKindRoom {
		room, roomErr := s.GetRoomContext(ctx, message.Target.RoomID)
		if roomErr != nil {
			return protocol.MessageAccepted{}, roomErr
		}
		err = s.automation.Apply(&message, room.Members, appendMessage)
	} else {
		_, err = appendMessage()
	}
	if err != nil {
		if errors.Is(err, store.ErrDuplicateMessage) {
			var duplicate *store.DuplicateMessageError
			if errors.As(err, &duplicate) {
				message.ID = duplicate.MessageID
				event.ID = eventIDForMessage(message.ID)
			}
			return protocol.MessageAccepted{
				MessageID: message.ID,
				EventID:   event.ID,
				Accepted:  true,
			}, nil
		}
		if errors.Is(err, automation.ErrDiscussionConflict) {
			return protocol.MessageAccepted{}, &Error{status: http.StatusConflict, msg: err.Error(), cause: err}
		}
		if errors.Is(err, store.ErrEnvelopeConflict) {
			observability.Logger(ctx, "rooms.signing", "origin_network_id", message.Origin.NetworkID, "origin_message_id", message.Origin.MessageID).
				Warn("rejecting origin message whose envelope conflicts with the stored one", "error", err)
			return protocol.MessageAccepted{}, envelopeConflictError(err)
		}
		if errors.Is(err, store.ErrMessageIDConflict) {
			return protocol.MessageAccepted{}, &Error{status: http.StatusConflict, msg: err.Error(), cause: err}
		}
		if errors.Is(err, store.ErrDMTopologyConflict) {
			return protocol.MessageAccepted{}, dmTopologyConflictError()
		}
		return protocol.MessageAccepted{}, err
	}
	s.relayMessage(message)
	if s.automation != nil {
		s.automation.Dispatch(s.lifecycleCtx, message)
	}

	return protocol.MessageAccepted{
		MessageID:     message.ID,
		EventID:       event.ID,
		Accepted:      true,
		ThreadCreated: lifecycle.Thread != nil,
		DMCreated:     lifecycle.DM != nil,
	}, nil
}

func (s *Service) appendAndPublishMessage(
	ctx context.Context,
	request protocol.SendMessageRequest,
	message protocol.Message,
	event protocol.Event,
) (store.AppendLifecycle, error) {
	if hub, ok, err := s.hubRoomForTarget(ctx, message.Target); err != nil {
		return store.AppendLifecycle{}, err
	} else if ok {
		return s.appendLocalHubMessage(ctx, request, message, event, hub)
	}
	s.deliveryMu.Lock()
	defer s.deliveryMu.Unlock()

	var (
		lifecycle store.AppendLifecycle
		err       error
	)
	pairingIDs := s.outboundPairingIDs(message)
	switch {
	case len(pairingIDs) > 0 && s.pairingDeliveries != nil:
		var durableEvent *protocol.Event
		if s.deliveryStore != nil {
			durableEvent = &event
		}
		limits := make(map[string]store.PairingDeliveryLimits)
		for _, pairing := range s.snapshotRelayPairings() {
			age, size := deliveryBounds(pairing)
			limits[pairing.ID] = store.PairingDeliveryLimits{MaxAge: age, MaxPending: size, RemoteNetworkID: pairing.RemoteNetworkID}
		}
		signedCtx, signErr := s.withOutboundEnvelope(ctx, message)
		if signErr != nil {
			return store.AppendLifecycle{}, signErr
		}
		appendCtx := store.WithPairingDeliveryLimits(signedCtx, limits)
		lifecycle, err = s.pairingDeliveries.AppendMessageWithPairingDeliveriesContext(appendCtx, message, durableEvent, pairingIDs)
	case len(pairingIDs) > 0:
		err = errors.New("message store does not support durable pairing delivery")
	case s.deliveryStore != nil:
		lifecycle, err = s.deliveryStore.AppendMessageEventWithLifecycleContext(ctx, message, event)
	case s.lifecycleMessages != nil:
		lifecycle, err = s.lifecycleMessages.AppendMessageWithLifecycleContext(ctx, message)
	default:
		err = s.appendMessage(ctx, message)
		if err == nil {
			lifecycle, err = s.conversationLifecycle(ctx, message)
		}
	}
	if err != nil {
		return store.AppendLifecycle{}, err
	}
	s.publishAppended(ctx, request, message, event, lifecycle)
	return lifecycle, nil
}

// publishAppended runs after a durable, non-duplicate append: drop logs,
// the causal stamp, and lifecycle and message events, exactly once.
func (s *Service) publishAppended(ctx context.Context, request protocol.SendMessageRequest, message protocol.Message, event protocol.Event, lifecycle store.AppendLifecycle) {
	for pairingID, dropped := range lifecycle.PairingDeliveriesDropped {
		s.logDroppedDeliveries(ctx, pairingID, dropped)
	}

	// The durable append above succeeded and was not a duplicate, so stamp and
	// fan out exactly once while attachment baseline creation remains excluded.
	s.stampMessageAccepted(ctx, message, request)
	if lifecycle.Thread != nil {
		s.publishEvent(protocol.Event{
			ID:        newPrefixedID("evt"),
			Type:      protocol.EventTypeThreadCreated,
			NetworkID: s.networkID,
			Thread:    lifecycle.Thread,
			CreatedAt: event.CreatedAt,
		})
	}
	if lifecycle.DM != nil {
		s.publishEvent(protocol.Event{
			ID:        newPrefixedID("evt"),
			Type:      protocol.EventTypeDMCreated,
			NetworkID: s.networkID,
			DM:        lifecycle.DM,
			CreatedAt: event.CreatedAt,
		})
	}
	s.publishEvent(event)
}

func (s *Service) validateDMSenderTopology(target protocol.Target, actor protocol.Actor) error {
	if target.Kind != protocol.TargetKindDM {
		return nil
	}

	participantIDs := protocol.UniqueTrimmedStrings(target.ParticipantIDs)
	canonicalIDs := make(map[string]struct{}, len(participantIDs))
	senderIncluded := false
	for _, participantID := range participantIDs {
		networkID, agentID := normalizedAgentIdentity(s.networkID, participantID)
		canonicalID := protocol.ScopedAgentID(networkID, agentID)
		if _, duplicate := canonicalIDs[canonicalID]; duplicate {
			return dmTopologyConflictError()
		}
		canonicalIDs[canonicalID] = struct{}{}
		if AgentIdentityMatches(actor.NetworkID, actor.ID, s.networkID, participantID) {
			senderIncluded = true
		}
	}
	if len(canonicalIDs) < 2 || !senderIncluded {
		return dmTopologyConflictError()
	}
	return nil
}

func (s *Service) Subscribe(ctx context.Context) <-chan protocol.Event {
	return s.filterEvents(ctx, s.broker.Subscribe(ctx))
}

// canonicalOriginIDs trims the origin ids once, before verification,
// dedupe and persistence all read them. Origin ids key receive receipts and
// envelope-conflict checks byte for byte, so " msg-1" and "msg-1" must be
// one spelling of one signed message everywhere, never two.
func canonicalOriginIDs(origin protocol.MessageOrigin) protocol.MessageOrigin {
	origin.NetworkID = strings.TrimSpace(origin.NetworkID)
	origin.MessageID = strings.TrimSpace(origin.MessageID)
	return origin
}

// stripPeerTestimony drops fields a peer may not supply on a pairing relay:
// the claimed_* origin fields are follow-import testimony only, and
// cause_event_ids is the local harness's causal context. Neither is covered
// by the origin signature, so keeping them would let a hop attach
// unverifiable claims to a verified message.
func stripPeerTestimony(request protocol.SendMessageRequest) protocol.SendMessageRequest {
	request.Origin.ClaimedAuthor = nil
	request.Origin.ClaimedNetworkID = ""
	request.Origin.ClaimedMessageID = ""
	request.Origin.ClaimedDiscoveryNetworkID = ""
	request.CauseEventIDs = nil
	return request
}
