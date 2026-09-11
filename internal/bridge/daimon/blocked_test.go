package daimon

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/bridge/loop"
)

func blockedBody(reason string, retryMS int64) string {
	return fmt.Sprintf(`{"version":"noopolis.daimon.wake-acceptance.v2","state":"stopped","code":"host_stopping","blocked":{"version":"noopolis.daimon.work-blocked.v1","reason":%q,"retry_after_ms":%d}}`, reason, retryMS)
}

func TestCodecRecognizesOnlyVersionedDeferral(t *testing.T) {
	for _, reason := range []string{"operator_stop", "ledger_unavailable", "host_stopping", "host_stopped", "queue_full"} {
		t.Run(reason, func(t *testing.T) {
			result, err := NewCodec("").DecodeResponse(daimonConfig("http://control.invalid"), daimonDelivery(), daimonResponse(http.StatusConflict, blockedBody(reason, 30000)))
			var deferred *loop.ControlDeferredError
			if !errors.As(err, &deferred) || deferred.Reason != reason || deferred.RetryAfter != 30*time.Second {
				t.Fatalf("expected typed deferral, got %#v, %v", deferred, err)
			}
			if result.Acceptance != nil || result.Publish {
				t.Fatalf("deferred work must not ACK or publish: %#v", result)
			}
		})
	}
}

func TestCodecRejectsMalformedBlockedResponsesWithoutLeakingMaterial(t *testing.T) {
	valid := blockedBody("operator_stop", 30000)
	for name, body := range map[string]string{
		"legacy stop":            `{"version":"noopolis.daimon.wake-acceptance.v2","state":"stopped","code":"host_stopping"}`,
		"legacy rejection":       `{"version":"noopolis.daimon.wake-acceptance.v2","state":"rejected","code":"unknown_agent"}`,
		"wrong state":            strings.Replace(valid, `"state":"stopped"`, `"state":"accepted"`, 1),
		"wrong code":             strings.Replace(valid, `"code":"host_stopping"`, `"code":"unknown_agent"`, 1),
		"wrong outer version":    strings.Replace(valid, wakeAcceptanceVersion, "unknown", 1),
		"wrong nested version":   strings.Replace(valid, workBlockedVersion, "unknown", 1),
		"unknown reason":         blockedBody("response-canary", 30000),
		"negative delay":         blockedBody("operator_stop", -1),
		"zero delay":             blockedBody("operator_stop", 0),
		"fractional delay":       strings.Replace(valid, "30000", "0.1", 1),
		"null delay":             strings.Replace(valid, "30000", "null", 1),
		"string delay":           strings.Replace(valid, "30000", `"30000"`, 1),
		"missing delay":          strings.Replace(valid, `,"retry_after_ms":30000`, "", 1),
		"extra nested field":     strings.Replace(valid, `"reason":`, `"extra":true,"reason":`, 1),
		"duplicate nested field": strings.Replace(valid, `"reason":`, `"reason":"operator_stop","reason":`, 1),
		"extra outer field":      strings.Replace(valid, `"state":`, `"extra":true,"state":`, 1),
		"duplicate outer field":  strings.Replace(valid, `"state":`, `"state":"stopped","state":`, 1),
		"trailing body":          valid + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			result, err := NewCodec("").DecodeResponse(daimonConfig("http://control.invalid"), daimonDelivery(), daimonResponse(http.StatusConflict, body))
			var deferred *loop.ControlDeferredError
			if err == nil || errors.As(err, &deferred) || result.Acceptance != nil || result.Publish || strings.Contains(err.Error(), "response-canary") {
				t.Fatalf("malformed response accepted or leaked material: %#v, %v", result, err)
			}
		})
	}
}

func TestCodecClampsDeferredDelayBeforeDurationConversion(t *testing.T) {
	_, err := NewCodec("").DecodeResponse(daimonConfig("http://control.invalid"), daimonDelivery(), daimonResponse(http.StatusConflict, blockedBody("operator_stop", 9223372036854775807)))
	var deferred *loop.ControlDeferredError
	if !errors.As(err, &deferred) || deferred.RetryAfter != 5*time.Minute {
		t.Fatalf("expected bounded delay, got %#v, %v", deferred, err)
	}
}
