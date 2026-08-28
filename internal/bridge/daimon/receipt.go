package daimon

import (
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/noopolis/moltnet/internal/bridge/loop"
)

const wakeReceiptVersion = "noopolis.daimon.wake-receipt-status.v2"

type wakeReceipt struct {
	AcceptanceID  string
	AgentID       string
	DeliveryID    string
	RequestDigest string
	State         string
	AcceptedAt    time.Time
	UpdatedAt     time.Time
	Code          string
	Text          string
	HasText       bool
}

func decodeWakeReceipt(response *http.Response, acceptance loop.ControlAcceptance) (wakeReceipt, error) {
	if response.StatusCode != http.StatusOK {
		return wakeReceipt{}, fmt.Errorf("Daimon wake receipt returned %s", response.Status)
	}
	fields, err := decodeExactObject(response.Body)
	if err != nil {
		return wakeReceipt{}, fmt.Errorf("decode Daimon wake receipt: %w", err)
	}
	required := []string{"version", "acceptance_id", "agent_id", "delivery_id", "request_digest", "state", "accepted_at", "updated_at"}
	allowed := map[string]bool{"code": true, "text": true}
	for _, field := range required {
		allowed[field] = true
		if _, ok := fields[field]; !ok {
			return wakeReceipt{}, fmt.Errorf("decode Daimon wake receipt: required field is missing")
		}
	}
	for field := range fields {
		if !allowed[field] {
			return wakeReceipt{}, fmt.Errorf("decode Daimon wake receipt: fields do not match its state")
		}
	}
	version, err := requiredString(fields, "version")
	if err != nil || version != wakeReceiptVersion {
		return wakeReceipt{}, fmt.Errorf("decode Daimon wake receipt: version is unsupported")
	}
	result := wakeReceipt{}
	if result.AcceptanceID, err = requiredString(fields, "acceptance_id"); err != nil || result.AcceptanceID != acceptance.ID {
		return wakeReceipt{}, fmt.Errorf("decode Daimon wake receipt: acceptance id does not match")
	}
	if result.AgentID, err = requiredBoundedString(fields, "agent_id"); err != nil || result.AgentID != acceptance.AgentID {
		return wakeReceipt{}, fmt.Errorf("decode Daimon wake receipt: agent does not match")
	}
	if result.DeliveryID, err = requiredBoundedString(fields, "delivery_id"); err != nil || result.DeliveryID != acceptance.DeliveryID {
		return wakeReceipt{}, fmt.Errorf("decode Daimon wake receipt: delivery does not match")
	}
	if result.RequestDigest, err = requiredString(fields, "request_digest"); err != nil || result.RequestDigest != acceptance.RequestDigest {
		return wakeReceipt{}, fmt.Errorf("decode Daimon wake receipt: request digest does not match")
	}
	if result.State, err = requiredString(fields, "state"); err != nil || !validWakeReceiptState(result.State) {
		return wakeReceipt{}, fmt.Errorf("decode Daimon wake receipt: state is unsupported")
	}
	acceptedAt, err := requiredString(fields, "accepted_at")
	if err != nil {
		return wakeReceipt{}, fmt.Errorf("decode Daimon wake receipt: accepted timestamp is invalid")
	}
	if result.AcceptedAt, err = time.Parse(time.RFC3339Nano, acceptedAt); err != nil {
		return wakeReceipt{}, fmt.Errorf("decode Daimon wake receipt: accepted timestamp is invalid")
	}
	if !acceptance.AcceptedAt.IsZero() && !result.AcceptedAt.Equal(acceptance.AcceptedAt) {
		return wakeReceipt{}, fmt.Errorf("decode Daimon wake receipt: accepted timestamp does not match")
	}
	updatedAt, err := requiredString(fields, "updated_at")
	if err != nil {
		return wakeReceipt{}, fmt.Errorf("decode Daimon wake receipt: updated timestamp is invalid")
	}
	if result.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil || result.UpdatedAt.Before(result.AcceptedAt) {
		return wakeReceipt{}, fmt.Errorf("decode Daimon wake receipt: updated timestamp is invalid")
	}
	if _, ok := fields["code"]; ok {
		result.Code, err = requiredString(fields, "code")
		if err != nil || !validWakeReceiptCode(result.Code) {
			return wakeReceipt{}, fmt.Errorf("decode Daimon wake receipt: code is unsupported")
		}
	}
	if _, ok := fields["text"]; ok {
		result.Text, err = requiredString(fields, "text")
		if err != nil || !utf8.ValidString(result.Text) || len(result.Text) > maxStringBytes {
			return wakeReceipt{}, fmt.Errorf("decode Daimon wake receipt: text exceeds maximum size")
		}
		result.HasText = true
	}
	if err := validateWakeReceiptFields(result); err != nil {
		return wakeReceipt{}, err
	}
	return result, nil
}

func validateWakeReceiptFields(receipt wakeReceipt) error {
	switch receipt.State {
	case "accepted", "running":
		if receipt.Code != "" || receipt.HasText {
			return fmt.Errorf("decode Daimon wake receipt: nonterminal fields are invalid")
		}
	case "completed":
		if receipt.Code != "" {
			return fmt.Errorf("decode Daimon wake receipt: completed code is invalid")
		}
	case "failed", "stopped":
		if receipt.HasText {
			return fmt.Errorf("decode Daimon wake receipt: failed text is invalid")
		}
	}
	return nil
}

func validWakeReceiptState(value string) bool {
	return value == "accepted" || value == "running" || value == "completed" || value == "failed" || value == "stopped"
}

func validWakeReceiptCode(value string) bool {
	return value == "engine_failed" || value == "host_stopped" || value == "host_stopping" || value == "queue_full" || value == "unknown_agent"
}
