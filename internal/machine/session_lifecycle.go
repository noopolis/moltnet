package machine

import (
	"context"
	"errors"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func (session *Session) handleRequest(request protocol.MachineRequest) {
	if session.isShuttingDownOrStopped() {
		return
	}

	state, err := session.lifecycle.register(request.CorrelationID, request.Operation)
	switch {
	case err == nil && state == registerStateAccepted:
		// Continue.
	case state == registerStateDuplicateActive:
		session.emitDuplicate(request)
		return
	case state == registerStateCapacity:
		session.shutdown(nil)
		session.emitTerminalErrorNoClaim(request, protocol.MachineErrorCapacity)
		return
	case state == registerStateClosed:
		return
	case state == registerStateDuplicateTerminal:
		return
	case errors.Is(err, errLifetimeCapacity):
		session.emitTerminalError(request, protocol.MachineErrorCapacity)
		return
	default:
		session.emitTerminalError(request, protocol.MachineErrorTransport)
		return
	}

	switch request.Operation {
	case protocol.MachineOpSubscribe, protocol.MachineOpExport:
		session.emitTerminalError(request, protocol.MachineErrorUnsupported)
	case protocol.MachineOpCancel:
		session.handleCancel(request)
	default:
		session.dispatchExecutableRequest(request)
	}
}

func (session *Session) emitDuplicate(request protocol.MachineRequest) {
	emit, target := session.lifecycle.claimDuplicate(request.CorrelationID)
	if target.cancel != nil {
		target.cancel()
	}
	if !emit {
		return
	}
	session.writeResponseNoClaim(terminalErrorResponse(target.correlation, target.operation, protocol.MachineErrorDuplicateRequest))
}

func (session *Session) dispatchExecutableRequest(request protocol.MachineRequest) {
	trackDelivery := false
	var deliveryIdentity DeliveryIdentity
	releaseDelivery := func() {}

	if request.Operation == protocol.MachineOpSendNudge {
		identity, ok := session.deliveryResolver.Identity(request)
		if ok {
			deliveryIdentity = identity
			claim, claimErr := session.deliveryRegistry.Claim(identity)
			if claimErr != nil && errors.Is(claimErr, errDeliveryCapacity) {
				session.emitTerminalError(request, protocol.MachineErrorCapacity)
				return
			}
			switch claim.State {
			case DeliveryClaimStateIdenticalResolved:
				cached := claim.ExistingResponse
				cached.CorrelationID = request.CorrelationID
				cached.Operation = request.Operation
				session.emitResponse(cached)
				return
			case DeliveryClaimStateIdenticalPending, DeliveryClaimStateChangedConflict:
				session.emitTerminalError(request, protocol.MachineErrorConflict)
				return
			case DeliveryClaimStateNew:
				trackDelivery = true
				releaseDelivery = func() {
					session.deliveryRegistry.Release(deliveryIdentity)
				}
			}
		}
	}

	ctx, cancel := context.WithCancel(session.ctx)
	if err := session.lifecycle.activate(request.CorrelationID, cancel); err != nil {
		releaseDelivery()
		cancel()
		session.emitTerminalError(request, sessionToProtocolError(err))
		return
	}

	if !session.startWorker(ctx, request, trackDelivery, deliveryIdentity) {
		releaseDelivery()
		cancel()
		session.emitTerminalError(request, protocol.MachineErrorTransport)
		return
	}
}

func (session *Session) startWorker(
	ctx context.Context,
	request protocol.MachineRequest,
	trackDelivery bool,
	deliveryIdentity DeliveryIdentity,
) bool {
	session.joinMu.Lock()
	if session.stopWorkers {
		session.joinMu.Unlock()
		return false
	}
	session.joinGroup.Add(1)
	session.joinMu.Unlock()

	go session.dispatchWorker(ctx, request, trackDelivery, deliveryIdentity)
	return true
}

func (session *Session) dispatchWorker(
	ctx context.Context,
	request protocol.MachineRequest,
	trackDelivery bool,
	deliveryIdentity DeliveryIdentity,
) {
	defer session.joinGroup.Done()

	result, err := session.executor.Execute(ctx, request)
	if err != nil {
		result = terminalTransportResponse(request)
	}
	result = normalizeResponse(request, result)

	terminalEmitted := session.emitResponse(result)
	if trackDelivery {
		if terminalEmitted {
			_ = session.deliveryRegistry.Resolve(
				deliveryIdentity,
				result,
				result.Error != nil && result.Error.Code == protocol.MachineErrorTransport,
			)
			return
		}
		_ = session.deliveryRegistry.Resolve(
			deliveryIdentity,
			terminalTransportResponse(request),
			true,
		)
	}
}

func terminalErrorResponse(correlationID, operation, code string) protocol.MachineResponse {
	return protocol.MachineResponse{
		Version:       protocol.MachineProtocolV1,
		CorrelationID: correlationID,
		Operation:     operation,
		Error: &protocol.MachineError{
			Code: code,
		},
	}
}

func (session *Session) emitResponse(response protocol.MachineResponse) bool {
	return session.emitTerminal(response)
}

func (session *Session) handleCancel(request protocol.MachineRequest) {
	targetCorrelationID := request.Cancel.TargetCorrelationID
	if targetCorrelationID == request.CorrelationID {
		session.emitTerminal(protocol.MachineResponse{
			Version:       protocol.MachineProtocolV1,
			CorrelationID: request.CorrelationID,
			Operation:     protocol.MachineOpCancel,
			Cancel: &protocol.MachineCancelResult{
				TargetCorrelationID: targetCorrelationID,
				State:               protocol.MachineCancelStateCanceled,
			},
		})
		return
	}

	state, target := session.lifecycle.claimCancel(targetCorrelationID)
	if state == protocol.MachineCancelStateCanceled {
		if target.cancel != nil {
			target.cancel()
		}
		session.writeResponseNoClaim(terminalCanceledResponse(target.correlation, target.operation))
	}

	response := protocol.MachineResponse{
		Version:       protocol.MachineProtocolV1,
		CorrelationID: request.CorrelationID,
		Operation:     protocol.MachineOpCancel,
		Cancel: &protocol.MachineCancelResult{
			TargetCorrelationID: targetCorrelationID,
			State:               state,
		},
	}
	session.emitTerminal(response)
}

func (session *Session) emitTerminalError(request protocol.MachineRequest, code string) {
	session.emitTerminal(protocol.MachineResponse{
		Version:       protocol.MachineProtocolV1,
		CorrelationID: request.CorrelationID,
		Operation:     request.Operation,
		Error: &protocol.MachineError{
			Code: code,
		},
	})
}

func (session *Session) emitTerminalErrorNoClaim(request protocol.MachineRequest, code string) {
	if err := session.writer.write(protocol.MachineResponse{
		Version:       protocol.MachineProtocolV1,
		CorrelationID: request.CorrelationID,
		Operation:     request.Operation,
		Error: &protocol.MachineError{
			Code: code,
		},
	}); err != nil {
		session.onWriterFailure(err)
	}
}

func (session *Session) emitTerminal(response protocol.MachineResponse) bool {
	emit, _ := session.lifecycle.claimTerminal(response.CorrelationID)
	if !emit {
		return false
	}
	session.writeResponse(response)
	return true
}

func sessionToProtocolError(err error) string {
	switch {
	case errors.Is(err, errActiveCapacity):
		return protocol.MachineErrorCapacity
	default:
		return protocol.MachineErrorTransport
	}
}

func normalizeResponse(request protocol.MachineRequest, response protocol.MachineResponse) protocol.MachineResponse {
	response.Version = protocol.MachineProtocolV1
	response.CorrelationID = request.CorrelationID
	response.Operation = request.Operation
	if response.Error != nil && response.Error.Code == protocol.MachineErrorTransport {
		return response
	}
	if err := response.Validate(); err != nil {
		return terminalTransportResponse(request)
	}
	if _, err := protocol.EncodeMachineResponseLine(response); err != nil {
		return terminalTransportResponse(request)
	}
	return response
}

func terminalTransportResponse(request protocol.MachineRequest) protocol.MachineResponse {
	return protocol.MachineResponse{
		Version:       protocol.MachineProtocolV1,
		CorrelationID: request.CorrelationID,
		Operation:     request.Operation,
		Error: &protocol.MachineError{
			Code: protocol.MachineErrorTransport,
		},
	}
}

func terminalCanceledResponse(correlationID, operation string) protocol.MachineResponse {
	return protocol.MachineResponse{
		Version:       protocol.MachineProtocolV1,
		CorrelationID: correlationID,
		Operation:     operation,
		Error: &protocol.MachineError{
			Code: protocol.MachineErrorCanceled,
		},
	}
}
