package daimon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/noopolis/moltnet/internal/bridge/loop"
)

const workBlockedVersion = "noopolis.daimon.work-blocked.v1"

// Only the versioned backpressure response is a deferral. Legacy 409 errors
// (unknown agent, conflicting delivery, malformed request) remain failures.
func decodeBlockedResponse(response *http.Response) error {
	failure := fmt.Errorf("control url returned %s", response.Status)
	fields, err := decodeExactObject(response.Body)
	if err != nil || requireExactFields(fields, "version", "state", "code", "blocked") != nil {
		return failure
	}
	version, err := requiredString(fields, "version")
	if err != nil || version != wakeAcceptanceVersion {
		return failure
	}
	state, err := requiredString(fields, "state")
	if err != nil || state != "stopped" {
		return failure
	}
	code, err := requiredString(fields, "code")
	if err != nil || (code != "host_stopping" && code != "host_stopped") {
		return failure
	}
	blocked, err := decodeExactObject(bytes.NewReader(fields["blocked"]))
	if err != nil || requireExactFields(blocked, "version", "reason", "retry_after_ms") != nil {
		return failure
	}
	version, err = requiredString(blocked, "version")
	if err != nil || version != workBlockedVersion {
		return failure
	}
	reason, err := requiredString(blocked, "reason")
	if err != nil || !validBlockedReason(reason) {
		return failure
	}
	var retryMS int64
	if err := json.Unmarshal(blocked["retry_after_ms"], &retryMS); err != nil || retryMS <= 0 {
		return failure
	}
	// Clamp before converting to Duration, which could otherwise overflow.
	if retryMS > int64((5*time.Minute)/time.Millisecond) {
		retryMS = int64((5 * time.Minute) / time.Millisecond)
	}
	return &loop.ControlDeferredError{Reason: reason, RetryAfter: time.Duration(retryMS) * time.Millisecond}
}

func validBlockedReason(reason string) bool {
	switch reason {
	case "operator_stop", "ledger_unavailable", "host_stopping", "host_stopped", "queue_full":
		return true
	default:
		return false
	}
}
