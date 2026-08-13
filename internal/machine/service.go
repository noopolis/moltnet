package machine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"reflect"
	"unicode/utf8"

	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

var errProviderDenied = errors.New("provider authority denied")
var errProviderInvalid = errors.New("provider value invalid")

// ProviderExecutor is the concrete packet-2 Executor. It has no HTTP or
// credential fields: Provider owns transport and DeliveryService owns the
// shared packet-1 reservation lifecycle.
type ProviderExecutor struct {
	resolver targetResolver
	delivery *DeliveryService
}

func NewProviderExecutor(attachment clientconfig.AttachmentConfig, provider Provider, registry DeliveryRegistry) *ProviderExecutor {
	return &ProviderExecutor{resolver: targetResolver{authority: projectAuthority(attachment), provider: provider}, delivery: NewDeliveryService(registry)}
}

var _ Executor = (*ProviderExecutor)(nil)

func (executor *ProviderExecutor) Execute(ctx context.Context, request protocol.MachineRequest) (protocol.MachineResponse, error) {
	if executor == nil || executor.resolver.provider == nil {
		return machineError(request, protocol.MachineErrorTransport), nil
	}
	switch request.Operation {
	case protocol.MachineOpSendNudge:
		return executor.send(ctx, request)
	case protocol.MachineOpRead:
		return executor.read(ctx, request)
	default:
		return machineError(request, protocol.MachineErrorUnsupported), nil
	}
}

func (executor *ProviderExecutor) send(ctx context.Context, request protocol.MachineRequest) (protocol.MachineResponse, error) {
	if request.SendNudge == nil || request.SendNudge.Validate() != nil {
		executor.releaseIfNotCanceled(ctx, request)
		return machineError(request, protocol.MachineErrorNotFound), nil
	}
	resolved, err := executor.resolver.resolve(ctx, request.SendNudge.Target, true)
	if err != nil {
		executor.releaseIfNotCanceled(ctx, request)
		return machineError(request, sendErrorCode(err)), nil
	}
	providerRequest := protocol.SendMessageRequest{
		ID: request.SendNudge.DeliveryID,
		Origin: protocol.MessageOrigin{NetworkID: executor.resolver.authority.networkID,
			MessageID: request.SendNudge.OriginMessageID},
		Target: resolved.target,
		From: protocol.Actor{Type: "agent", ID: executor.resolver.authority.memberID,
			Name: executor.resolver.authority.agentName, NetworkID: executor.resolver.authority.networkID},
		Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: request.SendNudge.Body,
			Data: map[string]any{"control_marker": protocol.MachineExportMarker}}},
		CauseEventIDs: append([]string(nil), request.SendNudge.CauseEventIDs...),
	}
	if request.SendNudge.OriginMessageID == "" {
		providerRequest.Origin = protocol.MessageOrigin{}
	}
	if protocol.ValidateSendMessageRequest(providerRequest) != nil {
		executor.releaseIfNotCanceled(ctx, request)
		return machineError(request, protocol.MachineErrorNotFound), nil
	}
	accepted, err := executor.resolver.provider.SendMessage(ctx, providerRequest)
	if err != nil {
		return machineError(request, protocol.MachineErrorTransport), nil
	}
	if !accepted.Accepted {
		if accepted.MessageID == "" && accepted.EventID == "" && !accepted.ThreadCreated && !accepted.DMCreated {
			executor.delivery.release(request)
			return machineError(request, protocol.MachineErrorNotFound), nil
		}
		return machineError(request, protocol.MachineErrorTransport), nil
	}
	result, ok := acceptedResult(accepted)
	if !ok {
		return machineError(request, protocol.MachineErrorTransport), nil
	}
	return protocol.MachineResponse{Version: protocol.MachineProtocolV1, CorrelationID: request.CorrelationID,
		Operation: request.Operation, SendNudge: &result}, nil
}

func (executor *ProviderExecutor) read(ctx context.Context, request protocol.MachineRequest) (protocol.MachineResponse, error) {
	if request.Read == nil || request.Read.Validate() != nil {
		return machineError(request, protocol.MachineErrorNotFound), nil
	}
	resolved, err := executor.resolver.resolve(ctx, request.Read.Target, false)
	if err != nil {
		return machineError(request, sendErrorCode(err)), nil
	}
	pageRequest := protocol.PageRequest{Before: request.Read.Before, After: request.Read.After, Limit: request.Read.Limit}
	var page protocol.MessagePage
	if resolved.target.Kind == protocol.TargetKindRoom {
		page, err = executor.resolver.provider.ListRoomMessages(ctx, resolved.target.RoomID, pageRequest)
	} else {
		page, err = executor.resolver.provider.ListDMMessages(ctx, resolved.target.DMID, pageRequest)
	}
	if err != nil {
		return machineError(request, protocol.MachineErrorTransport), nil
	}
	result, ok := projectRead(resolved, executor.resolver.authority.networkID, page, request.Read.Limit)
	if !ok {
		return machineError(request, protocol.MachineErrorTransport), nil
	}
	return protocol.MachineResponse{Version: protocol.MachineProtocolV1, CorrelationID: request.CorrelationID,
		Operation: request.Operation, Read: &result}, nil
}

func (executor *ProviderExecutor) releaseIfNotCanceled(ctx context.Context, request protocol.MachineRequest) {
	if ctx.Err() == nil {
		executor.delivery.release(request)
	}
}

func machineError(request protocol.MachineRequest, code string) protocol.MachineResponse {
	return protocol.MachineResponse{Version: protocol.MachineProtocolV1, CorrelationID: request.CorrelationID,
		Operation: request.Operation, Error: &protocol.MachineError{Code: code}}
}

func sendErrorCode(err error) string {
	if errors.Is(err, errProviderDenied) || errors.Is(err, errProviderInvalid) {
		return protocol.MachineErrorNotFound
	}
	return protocol.MachineErrorTransport
}

func acceptedResult(accepted protocol.MessageAccepted) (protocol.MachineSendNudgeResult, bool) {
	if accepted.ThreadCreated || accepted.DMCreated || protocol.ValidateMessageID(accepted.MessageID) != nil || protocol.ValidateMessageID(accepted.EventID) != nil {
		return protocol.MachineSendNudgeResult{}, false
	}
	no := false
	result := protocol.MachineSendNudgeResult{MessageID: accepted.MessageID, EventID: accepted.EventID,
		Accepted: boolPtr(true), ThreadCreated: &no, DMCreated: &no}
	return result, result.Validate() == nil
}

func boolPtr(value bool) *bool { return &value }

func projectRead(resolved resolvedTarget, network string, page protocol.MessagePage, limit int) (protocol.MachineReadResult, bool) {
	if len(page.Messages) > limit || len(page.Messages) > protocol.MachineMaxReadLimit {
		return protocol.MachineReadResult{}, false
	}
	messages := make([]protocol.MachineReadMessage, len(page.Messages))
	for index, message := range page.Messages {
		if !validReadMessage(message, resolved, network) {
			return protocol.MachineReadResult{}, false
		}
		copy, ok := cloneReadMessage(message)
		if !ok {
			return protocol.MachineReadResult{}, false
		}
		messages[index] = copy
	}
	hasMore := page.Page.HasMore
	result := protocol.MachineReadResult{Target: resolved.machine, Page: protocol.MachineReadPage{
		Messages: messages, Page: protocol.MachineReadPageInfo{HasMore: &hasMore,
			NextBefore: page.Page.NextBefore, NextAfter: page.Page.NextAfter}}}
	if result.Validate() != nil {
		return protocol.MachineReadResult{}, false
	}
	response := protocol.MachineResponse{Version: protocol.MachineProtocolV1, CorrelationID: "read", Operation: protocol.MachineOpRead, Read: &result}
	if _, err := protocol.EncodeMachineResponseLine(response); err != nil {
		return protocol.MachineReadResult{}, false
	}
	return result, true
}

func validReadMessage(message protocol.Message, resolved resolvedTarget, network string) bool {
	if message.NetworkID == "" || message.NetworkID != network || len(message.Parts) > protocol.MachineMaxReadMessageParts || len(message.Mentions) > protocol.MachineMaxReadMentions {
		return false
	}
	if resolved.target.Kind == protocol.TargetKindRoom {
		return message.Target.Kind == protocol.TargetKindRoom && message.Target.RoomID == resolved.target.RoomID && len(message.Target.ParticipantIDs) == 0
	}
	return resolved.dm != nil && message.Target.Kind == protocol.TargetKindDM && message.Target.DMID == resolved.target.DMID &&
		sameParticipantMultiset(message.Target.ParticipantIDs, resolved.dm.ParticipantIDs)
}

func sameParticipantMultiset(left, right []string) bool {
	if len(left) != 2 || len(right) != 2 {
		return false
	}
	return (left[0] == right[0] && left[1] == right[1]) || (left[0] == right[1] && left[1] == right[0])
}

func cloneReadMessage(message protocol.Message) (protocol.MachineReadMessage, bool) {
	copy := protocol.Message{ID: message.ID, NetworkID: message.NetworkID,
		Origin: protocol.MessageOrigin{NetworkID: message.Origin.NetworkID, MessageID: message.Origin.MessageID},
		Target: protocol.Target{Kind: message.Target.Kind, RoomID: message.Target.RoomID, ThreadID: message.Target.ThreadID,
			ParentMessageID: message.Target.ParentMessageID, DMID: message.Target.DMID},
		From:  protocol.Actor{Type: message.From.Type, ID: message.From.ID, Name: message.From.Name, NetworkID: message.From.NetworkID},
		Parts: make([]protocol.Part, len(message.Parts)), Mentions: append([]string(nil), message.Mentions...), CreatedAt: message.CreatedAt}
	for index, part := range message.Parts {
		copyPart, ok := clonePart(part)
		if !ok {
			return protocol.MachineReadMessage{}, false
		}
		copy.Parts[index] = copyPart
	}
	// The frozen machine read wire excludes DM participants. They were checked
	// against the resolved raw authority above, then intentionally removed.
	return protocol.MachineReadMessage(copy), true
}

func clonePart(part protocol.Part) (protocol.Part, bool) {
	copy := protocol.Part{Kind: part.Kind, Text: part.Text, MediaType: part.MediaType, URL: part.URL, Filename: part.Filename}
	if part.Data == nil {
		return copy, true
	}
	if !validJSONData(part.Data) {
		return protocol.Part{}, false
	}
	raw, err := json.Marshal(part.Data)
	if err != nil || len(raw) > protocol.MachineMaxReadPartDataBytes {
		return protocol.Part{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var data map[string]any
	if decoder.Decode(&data) != nil || data == nil {
		return protocol.Part{}, false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return protocol.Part{}, false
	}
	copy.Data = data
	return copy, true
}

const maxJSONDataDepth, maxJSONDataNodes, maxJSONDataString, maxJSONDataNumber = 32, 1024, 1024, 128

type jsonDataState struct {
	nodes int
	stack map[uintptr]bool
}

// validJSONData bounds the only untrusted in-memory tree before Marshal so a
// Provider cannot make this boundary allocate for an unbounded custom value.
func validJSONData(data map[string]any) bool {
	_, ok := measureJSONData(data, 0, &jsonDataState{stack: map[uintptr]bool{}})
	return ok
}
func measureJSONData(value any, depth int, state *jsonDataState) (int, bool) {
	if depth > maxJSONDataDepth || state.nodes == maxJSONDataNodes {
		return 0, false
	}
	state.nodes++
	switch value := value.(type) {
	case nil:
		return 4, true
	case bool:
		if value {
			return 4, true
		}
		return 5, true
	case string:
		return jsonStringSize(value)
	case json.Number:
		return jsonNumberSize(value.String())
	case float32:
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return 0, false
		}
		return 24, true
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, false
		}
		return 24, true
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return 20, true
	case []any:
		return measureJSONArray(value, depth, state)
	case map[string]any:
		return measureJSONObject(value, depth, state)
	default:
		return 0, false
	}
}
func measureJSONArray(values []any, depth int, state *jsonDataState) (int, bool) {
	pointer := reflect.ValueOf(values).Pointer()
	if state.stack[pointer] {
		return 0, false
	}
	state.stack[pointer] = true
	defer delete(state.stack, pointer)
	size := 2
	for _, value := range values {
		item, ok := measureJSONData(value, depth+1, state)
		if !ok || size+item+1 > protocol.MachineMaxReadPartDataBytes {
			return 0, false
		}
		size += item + 1
	}
	if len(values) != 0 {
		size--
	}
	return size, true
}
func measureJSONObject(values map[string]any, depth int, state *jsonDataState) (int, bool) {
	pointer := reflect.ValueOf(values).Pointer()
	if state.stack[pointer] {
		return 0, false
	}
	state.stack[pointer] = true
	defer delete(state.stack, pointer)
	size := 2
	for key, value := range values {
		keySize, keysOK := jsonStringSize(key)
		item, itemOK := measureJSONData(value, depth+1, state)
		if !keysOK || !itemOK || size+keySize+item+2 > protocol.MachineMaxReadPartDataBytes {
			return 0, false
		}
		size += keySize + item + 2
	}
	if len(values) != 0 {
		size--
	}
	return size, true
}
func jsonStringSize(value string) (int, bool) {
	if len(value) > maxJSONDataString {
		return 0, false
	}
	size := 2
	for len(value) > 0 {
		runeValue, width := utf8.DecodeRuneInString(value)
		if runeValue == utf8.RuneError && width == 1 {
			size += 6
		} else if runeValue < 0x20 || runeValue == '"' || runeValue == '\\' || runeValue == '<' || runeValue == '>' || runeValue == '&' || runeValue == 0x2028 || runeValue == 0x2029 {
			size += 6
		} else {
			size += width
		}
		if size > protocol.MachineMaxReadPartDataBytes {
			return 0, false
		}
		value = value[width:]
	}
	return size, true
}
func jsonNumberSize(value string) (int, bool) {
	if len(value) == 0 || len(value) > maxJSONDataNumber {
		return 0, false
	}
	i := 0
	if value[i] == '-' {
		i++
		if i == len(value) {
			return 0, false
		}
	}
	if value[i] == '0' {
		i++
	} else if value[i] >= '1' && value[i] <= '9' {
		for i < len(value) && value[i] >= '0' && value[i] <= '9' {
			i++
		}
	} else {
		return 0, false
	}
	if i < len(value) && value[i] == '.' {
		i++
		start := i
		for i < len(value) && value[i] >= '0' && value[i] <= '9' {
			i++
		}
		if i == start {
			return 0, false
		}
	}
	if i < len(value) && (value[i] == 'e' || value[i] == 'E') {
		i++
		if i < len(value) && (value[i] == '+' || value[i] == '-') {
			i++
		}
		start := i
		for i < len(value) && value[i] >= '0' && value[i] <= '9' {
			i++
		}
		if i == start {
			return 0, false
		}
	}
	return len(value), i == len(value)
}
