package machine

import (
	"context"
	"io"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func runProviderSend(t *testing.T, executor Executor, registry DeliveryRegistry, request protocol.MachineRequest) protocol.MachineResponse {
	t.Helper()
	input, writer := io.Pipe()
	out := newResponseCaptureWriter()
	session := NewSession(context.Background(), input, out, WithExecutor(executor), WithDeliveryRegistry(registry))
	done := make(chan error, 1)
	go func() { done <- session.Run() }()
	if _, err := writer.Write([]byte(harmonizeRequest(request) + "\n")); err != nil {
		t.Fatalf("write request: %v", err)
	}
	response := waitResponseByCorrelation(out, func(response protocol.MachineResponse) bool { return response.CorrelationID == request.CorrelationID })
	if err := writer.Close(); err != nil {
		t.Fatalf("close request input: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("session: %v", err)
	}
	return response
}

func TestProviderExecutorDeliveryReleaseCacheConflictAndCapacity(t *testing.T) {
	registry := NewDeliveryRegistry(2)
	fake := &fakeProvider{room: providerRoom(), accepted: accepted()}
	executor := NewProviderExecutor(machineAttachment(), fake, registry)
	first := sendRequest("room", "room")
	first.CorrelationID = "first"
	if response := runProviderSend(t, executor, registry, first); response.Error != nil || fake.sendCalls != 1 {
		t.Fatalf("first response %#v calls=%d", response, fake.sendCalls)
	}
	identical := first
	identical.CorrelationID = "identical"
	if response := runProviderSend(t, executor, registry, identical); response.Error != nil || fake.sendCalls != 1 {
		t.Fatalf("cached response %#v calls=%d", response, fake.sendCalls)
	}
	changed := first
	changed.CorrelationID = "changed"
	changed.SendNudge = &protocol.MachineSendNudgeRequest{DeliveryID: "delivery", Target: first.SendNudge.Target, Body: "changed"}
	if response := runProviderSend(t, executor, registry, changed); response.Error == nil || response.Error.Code != protocol.MachineErrorConflict || fake.sendCalls != 1 {
		t.Fatalf("changed response %#v calls=%d", response, fake.sendCalls)
	}
	second := sendRequest("room", "room")
	second.CorrelationID, second.SendNudge.DeliveryID = "capacity", "delivery2"
	if response := runProviderSend(t, executor, registry, second); response.Error != nil || fake.sendCalls != 2 {
		t.Fatalf("second response %#v calls=%d", response, fake.sendCalls)
	}
	third := sendRequest("room", "room")
	third.CorrelationID, third.SendNudge.DeliveryID = "over", "delivery3"
	if response := runProviderSend(t, executor, registry, third); response.Error == nil || response.Error.Code != protocol.MachineErrorCapacity || fake.sendCalls != 2 {
		t.Fatalf("capacity response %#v calls=%d", response, fake.sendCalls)
	}
}

func TestProviderExecutorDeliveryReleasesDefiniteFailureAndRetainsAmbiguity(t *testing.T) {
	t.Run("resolverFailure", func(t *testing.T) {
		registry := NewDeliveryRegistry(2)
		fake := &fakeProvider{room: providerRoom(), accepted: accepted()}
		executor := NewProviderExecutor(machineAttachment(), fake, registry)
		request := sendRequest("room", "missing")
		request.CorrelationID = "denied"
		if response := runProviderSend(t, executor, registry, request); response.Error == nil || fake.sendCalls != 0 {
			t.Fatalf("expected pre-send failure %#v calls=%d", response, fake.sendCalls)
		}
		request.CorrelationID = "retry"
		if response := runProviderSend(t, executor, registry, request); response.Error == nil || response.Error.Code == protocol.MachineErrorConflict || fake.sendCalls != 0 {
			t.Fatalf("resolver retry was not released %#v", response)
		}
	})
	t.Run("resolverTransport", func(t *testing.T) {
		registry := NewDeliveryRegistry(2)
		fake := &fakeProvider{roomErr: io.ErrClosedPipe}
		executor := NewProviderExecutor(machineAttachment(), fake, registry)
		request := sendRequest("room", "room")
		if response := runProviderSend(t, executor, registry, request); response.Error == nil || response.Error.Code != protocol.MachineErrorTransport || fake.sendCalls != 0 {
			t.Fatalf("expected pre-send transport %#v calls=%d", response, fake.sendCalls)
		}
		request.CorrelationID = "retry"
		if response := runProviderSend(t, executor, registry, request); response.Error == nil || response.Error.Code != protocol.MachineErrorTransport || fake.sendCalls != 0 {
			t.Fatalf("expected released pre-send transport %#v", response)
		}
	})
	t.Run("providerRejection", func(t *testing.T) {
		registry := NewDeliveryRegistry(2)
		fake := &fakeProvider{room: providerRoom(), accepted: protocol.MessageAccepted{Accepted: false}}
		executor := NewProviderExecutor(machineAttachment(), fake, registry)
		request := sendRequest("room", "room")
		if response := runProviderSend(t, executor, registry, request); response.Error == nil || fake.sendCalls != 1 {
			t.Fatalf("expected rejection %#v calls=%d", response, fake.sendCalls)
		}
		request.CorrelationID = "retry"
		if response := runProviderSend(t, executor, registry, request); response.Error == nil || fake.sendCalls != 2 {
			t.Fatalf("expected rejection retry %#v calls=%d", response, fake.sendCalls)
		}
	})
	t.Run("postSendError", func(t *testing.T) {
		registry := NewDeliveryRegistry(2)
		fake := &fakeProvider{room: providerRoom(), sendErr: errProviderInvalid}
		executor := NewProviderExecutor(machineAttachment(), fake, registry)
		request := sendRequest("room", "room")
		if response := runProviderSend(t, executor, registry, request); response.Error == nil || response.Error.Code != protocol.MachineErrorTransport || fake.sendCalls != 1 {
			t.Fatalf("expected ambiguous transport %#v calls=%d", response, fake.sendCalls)
		}
		request.CorrelationID = "retry"
		if response := runProviderSend(t, executor, registry, request); response.Error == nil || response.Error.Code != protocol.MachineErrorTransport || fake.sendCalls != 1 {
			t.Fatalf("expected cached ambiguity %#v calls=%d", response, fake.sendCalls)
		}
	})
}
