package daimon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const (
	wakeVersion           = "noopolis.daimon.wake.v2"
	wakeAcceptanceVersion = "noopolis.daimon.wake-acceptance.v2"
	maxResponseBytes      = 65_536
	maxStringBytes        = 16_384
	daimonTimestampLayout = "2006-01-02T15:04:05.000Z"
)

var (
	acceptanceIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	digestPattern       = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type Codec struct {
	token            protocol.SecretString
	receiptStorePath string
	receipts         *receiptTracker
}

type wakeRequest struct {
	AgentID    string    `json:"agent_id"`
	DeliveryID string    `json:"delivery_id"`
	Event      wakeEvent `json:"event"`
}

type wakeEvent struct {
	Version    string `json:"version"`
	Kind       string `json:"kind"`
	Text       string `json:"text"`
	OccurredAt string `json:"occurred_at"`
}

func NewCodec(token protocol.SecretString) *Codec { return &Codec{token: token} }

func newRuntimeCodec(token protocol.SecretString, receiptStorePath string) *Codec {
	return &Codec{token: token, receiptStorePath: receiptStorePath}
}

func runtimeAgentID(config bridgeconfig.Config) string {
	if agentID := strings.TrimSpace(config.Runtime.AgentID); agentID != "" {
		return agentID
	}
	return config.Agent.ID
}

func (*Codec) RequiresDurableAcceptance() bool { return true }

func (c *Codec) EncodeRequest(
	ctx context.Context,
	config bridgeconfig.Config,
	delivery loop.ControlDelivery,
) (*http.Request, error) {
	if delivery.EventID == "" {
		return nil, nil
	}
	payload, err := wakePayload(config, delivery)
	if err != nil {
		return nil, fmt.Errorf("encode control request: %w", err)
	}
	body := canonicalWakeJSON(payload)
	endpoint := strings.TrimRight(config.Runtime.ControlURL, "/") + "/v2/wakes"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build control request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(c.token.Reveal()); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return request, nil
}

func (*Codec) DecodeResponse(
	config bridgeconfig.Config,
	delivery loop.ControlDelivery,
	response *http.Response,
) (loop.ControlResult, error) {
	if response.StatusCode != http.StatusAccepted {
		return loop.ControlResult{}, fmt.Errorf("control url returned %s", response.Status)
	}
	fields, err := decodeExactObject(response.Body)
	if err != nil {
		return loop.ControlResult{}, fmt.Errorf("decode control response: %w", err)
	}
	if err := requireExactFields(fields,
		"version", "acceptance_id", "agent_id", "delivery_id", "request_digest", "state", "accepted_at",
	); err != nil {
		return loop.ControlResult{}, fmt.Errorf("decode control response: %w", err)
	}
	version, err := requiredString(fields, "version")
	if err != nil || version != wakeAcceptanceVersion {
		return loop.ControlResult{}, fmt.Errorf("decode control response: daimon wake acceptance version is unsupported")
	}
	state, err := requiredString(fields, "state")
	if err != nil || state != "accepted" {
		return loop.ControlResult{}, fmt.Errorf("decode control response: daimon wake was not durably accepted")
	}
	agentID, err := requiredBoundedString(fields, "agent_id")
	if err != nil || agentID != runtimeAgentID(config) {
		return loop.ControlResult{}, fmt.Errorf("decode control response: daimon wake acceptance agent does not match request")
	}
	deliveryID, err := requiredBoundedString(fields, "delivery_id")
	if err != nil || deliveryID != delivery.EventID {
		return loop.ControlResult{}, fmt.Errorf("decode control response: daimon wake acceptance delivery does not match request")
	}
	acceptanceID, err := requiredString(fields, "acceptance_id")
	if err != nil || !acceptanceIDPattern.MatchString(acceptanceID) {
		return loop.ControlResult{}, fmt.Errorf("decode control response: daimon wake acceptance id is invalid")
	}
	acceptedAt, err := requiredString(fields, "accepted_at")
	if err != nil {
		return loop.ControlResult{}, fmt.Errorf("decode control response: daimon wake acceptance timestamp is invalid")
	}
	acceptedTime, err := time.Parse(time.RFC3339Nano, acceptedAt)
	if err != nil {
		return loop.ControlResult{}, fmt.Errorf("decode control response: daimon wake acceptance timestamp is invalid")
	}
	wantDigest, err := wakeDigest(config, delivery)
	if err != nil {
		return loop.ControlResult{}, fmt.Errorf("decode control response: %w", err)
	}
	digest, err := requiredString(fields, "request_digest")
	if err != nil || digest != wantDigest {
		return loop.ControlResult{}, fmt.Errorf("decode control response: daimon wake acceptance digest does not match request")
	}

	// A durable acceptance receipt is the attachment ACK boundary. Cognitive
	// completion and any resulting Moltnet messages remain asynchronous.
	return loop.ControlResult{Acceptance: &loop.ControlAcceptance{
		ID:            acceptanceID,
		AgentID:       agentID,
		DeliveryID:    deliveryID,
		RequestDigest: digest,
		AcceptedAt:    acceptedTime,
	}}, nil
}

func wakePayload(config bridgeconfig.Config, delivery loop.ControlDelivery) (wakeRequest, error) {
	agentID := runtimeAgentID(config)
	if err := requireBoundedText("daimon agent id", agentID); err != nil {
		return wakeRequest{}, err
	}
	if err := requireBoundedText("daimon delivery id", delivery.EventID); err != nil {
		return wakeRequest{}, err
	}
	if err := requireWakeText(delivery.Message); err != nil {
		return wakeRequest{}, err
	}
	if delivery.OccurredAt.IsZero() {
		return wakeRequest{}, fmt.Errorf("daimon occurred_at is required")
	}
	return wakeRequest{
		AgentID:    agentID,
		DeliveryID: delivery.EventID,
		Event: wakeEvent{
			Version:    wakeVersion,
			Kind:       "message",
			Text:       delivery.Message,
			OccurredAt: delivery.OccurredAt.UTC().Truncate(time.Millisecond).Format(daimonTimestampLayout),
		},
	}, nil
}

func wakeDigest(config bridgeconfig.Config, delivery loop.ControlDelivery) (string, error) {
	payload, err := wakePayload(config, delivery)
	if err != nil {
		return "", err
	}
	body := canonicalWakeJSON(payload)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// canonicalWakeJSON matches JavaScript JSON.stringify for this fixed,
// string-only contract. encoding/json's HTML and U+2028/U+2029 escaping would
// otherwise produce a different digest from Daimon's parsed request.
func canonicalWakeJSON(payload wakeRequest) []byte {
	result := make([]byte, 0, len(payload.AgentID)+len(payload.DeliveryID)+len(payload.Event.Text)+160)
	result = append(result, `{"agent_id":`...)
	result = appendJSONString(result, payload.AgentID)
	result = append(result, `,"delivery_id":`...)
	result = appendJSONString(result, payload.DeliveryID)
	result = append(result, `,"event":{"version":`...)
	result = appendJSONString(result, payload.Event.Version)
	result = append(result, `,"kind":`...)
	result = appendJSONString(result, payload.Event.Kind)
	result = append(result, `,"text":`...)
	result = appendJSONString(result, payload.Event.Text)
	result = append(result, `,"occurred_at":`...)
	result = appendJSONString(result, payload.Event.OccurredAt)
	return append(result, '}', '}')
}

func appendJSONString(destination []byte, value string) []byte {
	const hexadecimal = "0123456789abcdef"
	destination = append(destination, '"')
	for _, character := range value {
		switch character {
		case '"', '\\':
			destination = append(destination, '\\', byte(character))
		case '\b':
			destination = append(destination, `\b`...)
		case '\t':
			destination = append(destination, `\t`...)
		case '\n':
			destination = append(destination, `\n`...)
		case '\f':
			destination = append(destination, `\f`...)
		case '\r':
			destination = append(destination, `\r`...)
		default:
			if character < 0x20 {
				destination = append(destination, '\\', 'u', '0', '0', hexadecimal[character>>4], hexadecimal[character&0x0f])
				continue
			}
			destination = utf8.AppendRune(destination, character)
		}
	}
	return append(destination, '"')
}

func decodeExactObject(reader io.Reader) (map[string]json.RawMessage, error) {
	payload, err := io.ReadAll(io.LimitReader(reader, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read daimon wake acceptance: %w", err)
	}
	if len(payload) > maxResponseBytes {
		return nil, fmt.Errorf("daimon wake acceptance exceeds maximum size")
	}
	if !utf8.Valid(payload) {
		return nil, fmt.Errorf("daimon wake acceptance is not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, fmt.Errorf("daimon wake acceptance must be an object")
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		keyToken, keyErr := decoder.Token()
		key, ok := keyToken.(string)
		if keyErr != nil || !ok {
			return nil, fmt.Errorf("daimon wake acceptance has an invalid field")
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, fmt.Errorf("daimon wake acceptance has duplicate fields")
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, fmt.Errorf("daimon wake acceptance field is invalid")
		}
		fields[key] = raw
	}
	if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') {
		return nil, fmt.Errorf("daimon wake acceptance object is incomplete")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("daimon wake acceptance has trailing data")
	}
	return fields, nil
}

func requireExactFields(fields map[string]json.RawMessage, expected ...string) error {
	if len(fields) != len(expected) {
		return fmt.Errorf("daimon wake acceptance fields do not match its state")
	}
	for _, field := range expected {
		if _, ok := fields[field]; !ok {
			return fmt.Errorf("daimon wake acceptance field %s is required", field)
		}
	}
	return nil
}

func requiredString(fields map[string]json.RawMessage, name string) (string, error) {
	raw, ok := fields[name]
	if !ok {
		return "", fmt.Errorf("daimon wake acceptance field %s is required", name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("daimon wake acceptance field %s must be a string", name)
	}
	return value, nil
}

func requiredBoundedString(fields map[string]json.RawMessage, name string) (string, error) {
	value, err := requiredString(fields, name)
	if err != nil {
		return "", err
	}
	if err := requireBoundedText("daimon wake acceptance field "+name, value); err != nil {
		return "", err
	}
	return value, nil
}

func requireWakeText(value string) error {
	if !utf8.ValidString(value) || len(value) > maxStringBytes {
		return fmt.Errorf("daimon wake text exceeds maximum size")
	}
	return nil
}

func requireBoundedText(name string, value string) error {
	if !utf8.ValidString(value) || len(value) > maxStringBytes {
		return fmt.Errorf("%s exceeds maximum size", name)
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	return nil
}
