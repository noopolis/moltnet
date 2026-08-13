package machine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type Executor interface {
	Execute(ctx context.Context, request protocol.MachineRequest) (protocol.MachineResponse, error)
}

type noopExecutor struct{}

func (noopExecutor) Execute(ctx context.Context, request protocol.MachineRequest) (protocol.MachineResponse, error) {
	return protocol.MachineResponse{Version: protocol.MachineProtocolV1, CorrelationID: request.CorrelationID, Operation: request.Operation}, nil
}

type DeliveryIdentity struct {
	Identity    string
	Fingerprint string
}

type DeliveryClaimState int

const (
	DeliveryClaimStateNew DeliveryClaimState = iota
	DeliveryClaimStateIdenticalPending
	DeliveryClaimStateIdenticalResolved
	DeliveryClaimStateChangedConflict
	DeliveryClaimStateCapacity
)

type DeliveryClaim struct {
	State               DeliveryClaimState
	ExistingResponse    protocol.MachineResponse
	ExistingResolved    bool
	ExistingAmbiguous   bool
	ExistingFingerprint string
}

type DeliveryIdentityResolver interface {
	Identity(request protocol.MachineRequest) (DeliveryIdentity, bool)
}

type DeliveryRegistry interface {
	Claim(identity DeliveryIdentity) (DeliveryClaim, error)
	Resolve(identity DeliveryIdentity, response protocol.MachineResponse, ambiguous bool) bool
	Release(identity DeliveryIdentity) bool
	Lookup(identity DeliveryIdentity) (protocol.MachineResponse, bool)
	Size() int
}

type DeliveryIdentityFromSendNudge struct{}

func (DeliveryIdentityFromSendNudge) Identity(request protocol.MachineRequest) (DeliveryIdentity, bool) {
	if request.Operation != protocol.MachineOpSendNudge || request.SendNudge == nil {
		return DeliveryIdentity{}, false
	}
	return DeliveryIdentity{
		Identity:    request.SendNudge.DeliveryID,
		Fingerprint: canonicalSendNudgeFingerprint(request.Operation, request.SendNudge),
	}, true
}

func canonicalSendNudgeFingerprint(operation string, req *protocol.MachineSendNudgeRequest) string {
	if req == nil {
		return ""
	}
	causes := append([]string{}, req.CauseEventIDs...)
	encoded, err := json.Marshal(struct {
		Operation string `json:"operation"`
		Target    struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		} `json:"target"`
		Body   string   `json:"body"`
		Origin string   `json:"origin_message_id"`
		Causes []string `json:"cause_event_ids"`
	}{
		Operation: operation,
		Target: struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		}{
			Kind: req.Target.Kind,
			ID:   req.Target.ID,
		},
		Body:   req.Body,
		Origin: req.OriginMessageID,
		Causes: causes,
	})
	if err != nil {
		return ""
	}

	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
