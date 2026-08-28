package daimon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestCodecEncodesExactWakeAcceptanceRequest(t *testing.T) {
	const token = "daimon-bearer-canary"
	codec := NewCodec(protocol.NewSecretString(token))
	delivery := daimonDelivery()
	request, err := codec.EncodeRequest(context.Background(), daimonConfig("http://control.invalid/base/"), delivery)
	if err != nil {
		t.Fatalf("EncodeRequest() error = %v", err)
	}
	defer request.Body.Close()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read request: %v", err)
	}
	want := `{"agent_id":"agent:researcher","delivery_id":"moltnet:msg_1","event":{"version":"noopolis.daimon.wake.v2","kind":"message","text":"[room research] Writer\nhello","occurred_at":"2026-08-24T08:11:12.345Z"}}`
	if string(body) != want {
		t.Fatalf("Daimon request body:\n got %s\nwant %s", body, want)
	}
	digest, err := wakeDigest(daimonConfig("http://control.invalid/base/"), delivery)
	if err != nil || digest != "7fc695a1c2d5203fc079731b4e45fd79b8ffdee730cc823cd9bb6afe571732a6" {
		t.Fatalf("Daimon request digest = %q, %v", digest, err)
	}
	if request.Method != http.MethodPost || request.URL.String() != "http://control.invalid/base/v2/wakes" {
		t.Fatalf("unexpected request %s %s", request.Method, request.URL)
	}
	if request.Header.Get("Content-Type") != "application/json" || request.Header.Get("Authorization") != "Bearer "+token {
		t.Fatalf("unexpected request headers %#v", request.Header)
	}
	if !codec.RequiresDurableAcceptance() {
		t.Fatal("Daimon codec must retain failed acceptance for replay")
	}

	bootstrap, err := codec.EncodeRequest(context.Background(), daimonConfig("http://control.invalid"), loop.ControlDelivery{})
	if err != nil || bootstrap != nil {
		t.Fatalf("bootstrap EncodeRequest() = %#v, %v; want intentional skip", bootstrap, err)
	}
}

func TestCodecAcceptsMatchingDurableReceiptWithoutPublishing(t *testing.T) {
	codec := NewCodec("")
	config := daimonConfig("http://control.invalid")
	delivery := daimonDelivery()
	body := acceptanceBody(t, config, delivery)

	for attempt := 0; attempt < 2; attempt++ {
		result, err := codec.DecodeResponse(config, delivery, daimonResponse(http.StatusAccepted, body))
		if err != nil || result.Publish || result.Text != "" || result.Acceptance == nil || result.Acceptance.ID != "11111111-1111-4111-8111-111111111111" {
			t.Fatalf("DecodeResponse() = %#v, %v; want durable acceptance without publication", result, err)
		}
	}
}

func TestWakeDigestMatchesDaimonJSONStringEscaping(t *testing.T) {
	delivery := daimonDelivery()
	delivery.EventID = "moltnet:special"
	delivery.Message = "<&>\u2028\u000b"
	digest, err := wakeDigest(daimonConfig("http://control.invalid"), delivery)
	if err != nil || digest != "af9aa520d07d2cb774fe71a30d8d9d3803591405f052b3533908d91c1d9f0421" {
		t.Fatalf("JavaScript-compatible digest = %q, %v", digest, err)
	}
}

func TestCodecFallsBackToMoltnetMemberForLegacyConfig(t *testing.T) {
	config := daimonConfig("http://control.invalid")
	config.Runtime.AgentID = ""
	delivery := daimonDelivery()
	codec := NewCodec("")
	request, err := codec.EncodeRequest(context.Background(), config, delivery)
	if err != nil {
		t.Fatalf("EncodeRequest() error = %v", err)
	}
	body, err := io.ReadAll(request.Body)
	request.Body.Close()
	if err != nil || !strings.Contains(string(body), `"agent_id":"researcher"`) {
		t.Fatalf("legacy request body = %s, %v", body, err)
	}
	result, err := codec.DecodeResponse(config, delivery, daimonResponse(http.StatusAccepted, acceptanceBody(t, config, delivery)))
	if err != nil || result.Publish || result.Acceptance == nil {
		t.Fatalf("legacy DecodeResponse() = %#v, %v", result, err)
	}
}

func TestCodecRejectsNonAcceptanceReceipts(t *testing.T) {
	config := daimonConfig("http://control.invalid")
	delivery := daimonDelivery()
	accepted := acceptanceBody(t, config, delivery)
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{"error":"unauthorized"}`},
		{name: "rejected", status: http.StatusConflict, body: `{"version":"noopolis.daimon.wake-acceptance.v2","state":"rejected","code":"unknown_agent"}`},
		{name: "wrong agent", status: http.StatusAccepted, body: strings.Replace(accepted, `"agent:researcher"`, `"other"`, 1)},
		{name: "wrong delivery", status: http.StatusAccepted, body: strings.Replace(accepted, `"moltnet:msg_1"`, `"moltnet:other"`, 1)},
		{name: "wrong digest", status: http.StatusAccepted, body: strings.Replace(accepted, `"request_digest":"`, `"request_digest":"0`, 1)},
		{name: "wrong status", status: http.StatusOK, body: accepted},
		{name: "malformed", status: http.StatusAccepted, body: `{"version":`},
		{name: "wrong version", status: http.StatusAccepted, body: strings.Replace(accepted, wakeAcceptanceVersion, "noopolis.daimon.wake-acceptance.v1", 1)},
		{name: "unknown state", status: http.StatusAccepted, body: strings.Replace(accepted, `"accepted"`, `"running"`, 1)},
		{name: "unknown field", status: http.StatusAccepted, body: strings.Replace(accepted, `,"accepted_at"`, `,"extra":true,"accepted_at"`, 1)},
		{name: "missing field", status: http.StatusAccepted, body: strings.Replace(accepted, `,"accepted_at":"2026-08-24T08:11:12.345Z"`, ``, 1)},
		{name: "duplicate field", status: http.StatusAccepted, body: strings.Replace(accepted, `"state":"accepted"`, `"state":"accepted","state":"accepted"`, 1)},
		{name: "trailing data", status: http.StatusAccepted, body: accepted + `{}`},
		{name: "invalid acceptance id", status: http.StatusAccepted, body: strings.Replace(accepted, "11111111-1111-4111-8111-111111111111", "not-a-uuid", 1)},
		{name: "invalid timestamp", status: http.StatusAccepted, body: strings.Replace(accepted, "2026-08-24T08:11:12.345Z", "tomorrow", 1)},
		{name: "oversized body", status: http.StatusAccepted, body: strings.Repeat(" ", maxResponseBytes+1)},
	}
	codec := NewCodec("")
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := codec.DecodeResponse(config, delivery, daimonResponse(test.status, test.body))
			if err == nil || result.Publish {
				t.Fatalf("DecodeResponse() = %#v, %v; want non-publishing failure", result, err)
			}
		})
	}
}

func TestCodecErrorsDoNotLeakWakeOrResponseMaterial(t *testing.T) {
	const wakeCanary = "wake-text-canary"
	_, err := NewCodec("").EncodeRequest(context.Background(), daimonConfig("http://control.invalid"), loop.ControlDelivery{
		EventID: wakeCanary, Message: wakeCanary + strings.Repeat("x", maxStringBytes), OccurredAt: time.Now(),
	})
	if err == nil || strings.Contains(err.Error(), wakeCanary) {
		t.Fatalf("oversized wake error leaked content: %v", err)
	}

	const responseCanary = "reflected-bearer-canary"
	_, err = NewCodec("").DecodeResponse(
		daimonConfig("http://control.invalid"), daimonDelivery(),
		daimonResponse(http.StatusUnauthorized, `{"error":"`+responseCanary+`"}`),
	)
	if err == nil || strings.Contains(err.Error(), responseCanary) {
		t.Fatalf("response error reflected bearer material: %v", err)
	}
}

func daimonDelivery() loop.ControlDelivery {
	return loop.ControlDelivery{
		Target:        protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		EventID:       "moltnet:msg_1",
		From:          "writer",
		Message:       "[room research] Writer\nhello",
		TransportText: "hello",
		OccurredAt:    time.Date(2026, 8, 24, 10, 11, 12, 345678000, time.FixedZone("test", 2*60*60)),
	}
}

func acceptanceBody(t *testing.T, config bridgeconfig.Config, delivery loop.ControlDelivery) string {
	t.Helper()
	digest, err := wakeDigest(config, delivery)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf(`{"version":"%s","acceptance_id":"11111111-1111-4111-8111-111111111111","agent_id":%q,"delivery_id":%q,"request_digest":%q,"state":"accepted","accepted_at":"2026-08-24T08:11:12.345Z"}`,
		wakeAcceptanceVersion, runtimeAgentID(config), delivery.EventID, digest)
}

func daimonConfig(controlURL string) bridgeconfig.Config {
	return bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "researcher", Name: "Researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{NetworkID: "local"},
		Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeDaimon, AgentID: "agent:researcher", ControlURL: controlURL, TokenEnv: "DAIMON_TOKEN"},
	}
}

func daimonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
