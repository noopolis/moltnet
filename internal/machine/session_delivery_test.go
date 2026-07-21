package machine

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestSessionDeliveryIdenticalResolvedAndConflict(t *testing.T) {
	delivery := newMemoryDeliveryRegistry(16)
	var executeCounter atomic.Int64
	exec := &simpleExecutor{
		handler: func(request protocol.MachineRequest) (protocol.MachineResponse, error) {
			executeCounter.Add(1)
			return successResponse(request)
		},
	}
	s1Line := harmonizeRequest(buildSendNudgeRequest(1, "hello", "delivery-1"))
	s1Input, s1Writer := io.Pipe()
	s1Out := newResponseCaptureWriter()
	session1 := NewSession(context.Background(), s1Input, s1Out, WithExecutor(exec), WithDeliveryRegistry(delivery))
	session1Done := make(chan error, 1)
	go func() {
		session1Done <- session1.Run()
	}()
	if _, err := s1Writer.Write([]byte(s1Line + "\n")); err != nil {
		t.Fatalf("expected first delivery write: %v", err)
	}
	s1Response := waitResponseByCorrelation(s1Out, func(response protocol.MachineResponse) bool {
		return response.CorrelationID == "send_1" &&
			response.Error == nil &&
			response.SendNudge != nil &&
			response.SendNudge.Accepted != nil &&
			*response.SendNudge.Accepted
	})
	if err := s1Writer.Close(); err != nil {
		t.Fatalf("expected first delivery close: %v", err)
	}
	if err := readErr(t, session1Done); err != nil {
		t.Fatalf("first send failed: %v", err)
	}
	if got := executeCounter.Load(); got != 1 {
		t.Fatalf("expected one semantic execution after first send, got %d", got)
	}
	if s1Response.CorrelationID != "send_1" {
		t.Fatalf("expected correlation send_1, got %s", s1Response.CorrelationID)
	}
	if s1Response.Error != nil {
		t.Fatalf("expected no error on first delivery, got %v", s1Response.Error)
	}
	if s1Response.SendNudge == nil {
		t.Fatalf("expected send_nudge result on first delivery")
	}
	if s1Response.SendNudge.Accepted == nil || !*s1Response.SendNudge.Accepted {
		t.Fatalf("expected accepted first delivery")
	}

	out2 := &bytes.Buffer{}
	s2Input := io.NopCloser(strings.NewReader(strings.Join([]string{
		harmonizeRequest(buildSendNudgeRequest(2, "hello", "delivery-1")),
		harmonizeRequest(buildSendNudgeRequest(3, "changed", "delivery-1")),
	}, "\n")))
	session2 := NewSession(context.Background(), s2Input, out2, WithExecutor(exec), WithDeliveryRegistry(delivery))
	if err := session2.Run(); err != nil {
		t.Fatalf("second session failed: %v", err)
	}
	if got := executeCounter.Load(); got != 1 {
		t.Fatalf("expected one semantic execution after duplicate/conflict handling, got %d", got)
	}

	responses := decodeResponses(t, out2.Bytes())
	if len(responses) != 2 {
		t.Fatalf("expected two responses, got %d", len(responses))
	}
	if responses[0].Error != nil || responses[0].CorrelationID != "send_2" || responses[0].SendNudge == nil || responses[0].SendNudge.Accepted == nil || !*responses[0].SendNudge.Accepted {
		t.Fatalf("expected cached terminal for send_2, got %#v", responses[0])
	}
	if responses[1].Error == nil || responses[1].Error.Code != protocol.MachineErrorConflict || responses[1].CorrelationID != "send_3" {
		t.Fatalf("expected conflict for changed fingerprint, got %#v", responses[1].Error)
	}
}

func TestSessionDeliveryRegistryTracksFingerprintAndAvoidsSecondExecution(t *testing.T) {
	delivery := &trackedDeliveryRegistry{delegate: newMemoryDeliveryRegistry(16)}
	var executeCounter atomic.Int64
	exec := &simpleExecutor{
		handler: func(request protocol.MachineRequest) (protocol.MachineResponse, error) {
			executeCounter.Add(1)
			resp, err := successResponse(request)
			return resp, err
		},
	}

	s1Request := buildSendNudgeRequest(1, "hello", "same-delivery")
	s1Line := harmonizeRequest(s1Request)
	s1Input, s1Writer := io.Pipe()
	s1Out := newResponseCaptureWriter()
	s1 := NewSession(context.Background(), s1Input, s1Out, WithExecutor(exec), WithDeliveryRegistry(delivery))
	s1Done := make(chan error, 1)
	go func() {
		s1Done <- s1.Run()
	}()
	if _, err := s1Writer.Write([]byte(s1Line + "\n")); err != nil {
		t.Fatalf("expected first delivery write: %v", err)
	}
	s1Response := waitResponseByCorrelation(s1Out, func(response protocol.MachineResponse) bool {
		return response.CorrelationID == s1Request.CorrelationID &&
			response.Error == nil &&
			response.SendNudge != nil &&
			response.SendNudge.Accepted != nil &&
			*response.SendNudge.Accepted
	})
	if err := s1Writer.Close(); err != nil {
		t.Fatalf("expected first delivery close: %v", err)
	}
	if err := readErr(t, s1Done); err != nil {
		t.Fatalf("first delivery failed: %v", err)
	}
	if got := executeCounter.Load(); got != 1 {
		t.Fatalf("expected one semantic execution after first send, got %d", got)
	}
	if s1Response.CorrelationID != s1Request.CorrelationID {
		t.Fatalf("expected correlation %q, got %q", s1Request.CorrelationID, s1Response.CorrelationID)
	}

	s2Request := buildSendNudgeRequest(2, "hello", "same-delivery")
	out2 := &bytes.Buffer{}
	s2Input := io.NopCloser(strings.NewReader(harmonizeRequest(s2Request)))
	s2 := NewSession(context.Background(), s2Input, out2, WithExecutor(exec), WithDeliveryRegistry(delivery))
	if err := s2.Run(); err != nil {
		t.Fatalf("cached duplicate should not fail: %v", err)
	}
	responses := decodeResponses(t, out2.Bytes())
	if len(responses) != 1 {
		t.Fatalf("expected one cached replay response, got %d", len(responses))
	}
	if responses[0].Error != nil || responses[0].CorrelationID != s2Request.CorrelationID || responses[0].SendNudge == nil || responses[0].SendNudge.Accepted == nil || !*responses[0].SendNudge.Accepted {
		t.Fatalf("expected cached terminal for send_2, got %#v", responses[0])
	}

	if len(delivery.claims) != 2 {
		t.Fatalf("expected two wrapper claims, got %d", len(delivery.claims))
	}
	if delivery.claims[0] != DeliveryClaimStateNew {
		t.Fatalf("expected first claim to be new, got %v", delivery.claims[0])
	}
	if delivery.claims[1] != DeliveryClaimStateIdenticalResolved {
		t.Fatalf("expected second claim to be identical resolved, got %v", delivery.claims[1])
	}
	if delivery.resolveCalls != 1 {
		t.Fatalf("expected one resolve call from first execution, got %d", delivery.resolveCalls)
	}
	if got := executeCounter.Load(); got != 1 {
		t.Fatalf("expected one semantic execution after duplicate handling, got %d", got)
	}
}
