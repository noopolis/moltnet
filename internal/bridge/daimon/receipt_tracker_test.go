package daimon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestReceiptTrackerResumesAfterRestartAndUsesIdempotentPublicationID(t *testing.T) {
	useFastReceiptPolling(t)
	job := trackerTestJob()
	var receiptState atomic.Value
	receiptState.Store("running")
	var receiptPolls atomic.Int32
	control := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer daimon-token" {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		receiptPolls.Add(1)
		_, _ = response.Write([]byte(trackerReceiptJSON(t, job, receiptState.Load().(string), "result text", "")))
	}))
	defer control.Close()

	var mu sync.Mutex
	var sendAttempts []protocol.SendMessageRequest
	visible := map[string]protocol.SendMessageRequest{}
	moltnet := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/messages" {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		var payload protocol.SendMessageRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode message: %v", err)
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		sendAttempts = append(sendAttempts, payload)
		visible[payload.ID] = payload
		attempt := len(sendAttempts)
		mu.Unlock()
		if attempt == 1 {
			response.WriteHeader(http.StatusInternalServerError) // persisted, response lost
			return
		}
		_, _ = response.Write([]byte(fmt.Sprintf(`{"message_id":%q,"event_id":"evt_reply","accepted":true}`, payload.ID)))
	}))
	defer moltnet.Close()

	storePath := filepath.Join(t.TempDir(), "private", "receipts.json")
	config := trackerConfig(control.URL, moltnet.URL, storePath)
	store, err := openReceiptStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	first := newReceiptTracker(store, "daimon-token", loop.NewMoltnetClient(config), config)
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := runTracker(firstCtx, first)
	if err := first.Accept(job); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return receiptPolls.Load() > 0 })
	cancelFirst()
	<-firstDone
	if pending := store.Pending(); len(pending) != 1 {
		t.Fatalf("pending jobs before restart = %#v", pending)
	}

	receiptState.Store("completed")
	restartedStore, err := openReceiptStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := restartedStore.ValidateAuthority("other", job.RuntimeAgentID); err == nil {
		t.Fatal("receipt store authority mismatch was accepted")
	}
	second := newReceiptTracker(restartedStore, "daimon-token", loop.NewMoltnetClient(config), config)
	secondCtx, cancelSecond := context.WithCancel(context.Background())
	secondDone := runTracker(secondCtx, second)
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(sendAttempts) >= 2 && len(restartedStore.Pending()) == 0
	})
	cancelSecond()
	<-secondDone

	mu.Lock()
	defer mu.Unlock()
	if len(sendAttempts) != 2 || sendAttempts[0].ID != sendAttempts[1].ID || len(visible) != 1 {
		t.Fatalf("publication attempts were not idempotent: attempts=%#v visible=%#v", sendAttempts, visible)
	}
	published := sendAttempts[1]
	if published.ID != bridgeutil.DeliveryReplyMessageID("researcher", job.DeliveryID, job.Event.Message.Target) || published.Parts[0].Text != "result text" || published.Target.RoomID != "research" {
		t.Fatalf("unexpected publication %#v", published)
	}
	if len(published.CauseEventIDs) != 1 || published.CauseEventIDs[0] != "daimon:moltnet:msg_1:turn.output.completed" {
		t.Fatalf("unexpected publication causes %#v", published.CauseEventIDs)
	}
}

func TestReceiptTrackerJoinsAnExplicitDeliveryReplyInsteadOfPublishingAgain(t *testing.T) {
	useFastReceiptPolling(t)
	job := trackerTestJob()
	control := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(trackerReceiptJSON(t, job, "completed", "terminal version", "")))
	}))
	defer control.Close()

	replyID := bridgeutil.DeliveryReplyMessageID(job.MoltnetAgent.ID, job.DeliveryID, job.Event.Message.Target)
	visible := map[string]protocol.SendMessageRequest{
		replyID: {
			ID: replyID, Target: job.Event.Message.Target, From: job.MoltnetAgent,
			Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "explicit version"}},
		},
	}
	var attempts int
	moltnet := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var payload protocol.SendMessageRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode message: %v", err)
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		attempts++
		if _, exists := visible[payload.ID]; !exists {
			visible[payload.ID] = payload
		}
		_, _ = response.Write([]byte(fmt.Sprintf(`{"message_id":%q,"event_id":"evt_reply","accepted":true}`, payload.ID)))
	}))
	defer moltnet.Close()

	store, err := openReceiptStore(filepath.Join(t.TempDir(), "private", "receipts.json"))
	if err != nil {
		t.Fatal(err)
	}
	config := trackerConfig(control.URL, moltnet.URL, store.path)
	tracker := newReceiptTracker(store, "", loop.NewMoltnetClient(config), config)
	ctx, cancel := context.WithCancel(context.Background())
	done := runTracker(ctx, tracker)
	if err := tracker.Accept(job); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(store.Pending()) == 0 })
	cancel()
	<-done

	if attempts != 1 || len(visible) != 1 || visible[replyID].Parts[0].Text != "explicit version" {
		t.Fatalf("explicit delivery reply was not joined idempotently: attempts=%d visible=%#v", attempts, visible)
	}
}

func TestReceiptTrackerReportsTerminalRuntimeFailureOnceAcrossRestart(t *testing.T) {
	useFastReceiptPolling(t)
	job := trackerTestJob()
	control := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(trackerReceiptJSON(t, job, "failed", "", "engine_failed")))
	}))
	defer control.Close()

	var mu sync.Mutex
	var reports []protocol.ReportWakeFailedRequest
	moltnet := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v1/messages" {
			t.Error("failed receipt published a message")
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		if request.URL.Path != "/v1/agents/wake-failed" {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		var payload protocol.ReportWakeFailedRequest
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode failure report: %v", err)
		}
		mu.Lock()
		reports = append(reports, payload)
		mu.Unlock()
		response.WriteHeader(http.StatusAccepted)
	}))
	defer moltnet.Close()

	storePath := filepath.Join(t.TempDir(), "private", "receipts.json")
	config := trackerConfig(control.URL, moltnet.URL, storePath)
	store, err := openReceiptStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	tracker := newReceiptTracker(store, "", loop.NewMoltnetClient(config), config)
	ctx, cancel := context.WithCancel(context.Background())
	done := runTracker(ctx, tracker)
	if err := tracker.Accept(job); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(store.Pending()) == 0 })
	cancel()
	<-done

	restarted, err := openReceiptStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	restartCtx, cancelRestart := context.WithCancel(context.Background())
	restartDone := runTracker(restartCtx, newReceiptTracker(restarted, "", loop.NewMoltnetClient(config), config))
	time.Sleep(25 * time.Millisecond)
	cancelRestart()
	<-restartDone

	mu.Lock()
	defer mu.Unlock()
	if len(reports) != 1 || reports[0].Classification != protocol.WakeFailureClassificationRuntimeFailed || reports[0].Event.Message == nil || reports[0].Event.Message.ID != "msg_1" {
		t.Fatalf("unexpected terminal failure reports %#v", reports)
	}
}

func TestDecodeWakeReceiptAllowsLegacyAndEmptyCompletionButRejectsStateLeaks(t *testing.T) {
	job := trackerTestJob()
	acceptance := loop.ControlAcceptance{ID: job.AcceptanceID, AgentID: job.RuntimeAgentID, DeliveryID: job.DeliveryID, RequestDigest: job.RequestDigest}
	tests := []struct {
		name    string
		body    string
		ok      bool
		hasText bool
	}{
		{name: "empty reply", body: trackerReceiptJSON(t, job, "completed", "", ""), ok: true, hasText: true},
		{name: "legacy completion", body: strings.Replace(trackerReceiptJSON(t, job, "completed", "", ""), `,"text":""`, "", 1), ok: true},
		{name: "failed text leak", body: strings.Replace(trackerReceiptJSON(t, job, "failed", "", "engine_failed"), `,"code":"engine_failed"`, `,"code":"engine_failed","text":"leak"`, 1)},
		{name: "unknown field", body: strings.Replace(trackerReceiptJSON(t, job, "running", "", ""), `"state":"running"`, `"state":"running","extra":true`, 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := daimonResponse(http.StatusOK, test.body)
			receipt, err := decodeWakeReceipt(response, acceptance)
			response.Body.Close()
			if (err == nil) != test.ok || receipt.HasText != test.hasText {
				t.Fatalf("decodeWakeReceipt() = %#v, %v", receipt, err)
			}
		})
	}
}

func trackerTestJob() receiptJob {
	acceptedAt := time.Date(2026, 8, 24, 8, 11, 12, 345000000, time.UTC)
	return receiptJob{
		AcceptanceID:   "11111111-1111-4111-8111-111111111111",
		RuntimeAgentID: "agent:researcher",
		DeliveryID:     "moltnet:msg_1",
		RequestDigest:  strings.Repeat("a", 64),
		MoltnetAgent:   protocol.Actor{Type: "agent", ID: "researcher", Name: "Researcher"},
		Event: protocol.Event{ID: "evt_1", Type: protocol.EventTypeMessageCreated, NetworkID: "local", Message: &protocol.Message{
			ID: "msg_1", NetworkID: "local", Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"}, From: protocol.Actor{Type: "agent", ID: "writer"}, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "question"}}, CreatedAt: acceptedAt,
		}, CreatedAt: acceptedAt},
		State: receiptJobPending, AcceptedAt: acceptedAt, UpdatedAt: acceptedAt,
	}
}

func trackerReceiptJSON(t *testing.T, job receiptJob, state, text, code string) string {
	t.Helper()
	payload := map[string]any{"version": wakeReceiptVersion, "acceptance_id": job.AcceptanceID, "agent_id": job.RuntimeAgentID, "delivery_id": job.DeliveryID, "request_digest": job.RequestDigest, "state": state, "accepted_at": job.AcceptedAt.Format(time.RFC3339Nano), "updated_at": job.AcceptedAt.Add(time.Minute).Format(time.RFC3339Nano)}
	if state == "completed" {
		payload["text"] = text
	}
	if code != "" {
		payload["code"] = code
	}
	contents, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func trackerConfig(controlURL, moltnetURL, storePath string) bridgeconfig.Config {
	return bridgeconfig.Config{Agent: bridgeconfig.AgentConfig{ID: "researcher", Name: "Researcher"}, Moltnet: bridgeconfig.MoltnetConfig{BaseURL: moltnetURL, NetworkID: "local"}, Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimeDaimon, AgentID: "agent:researcher", ControlURL: controlURL, TokenEnv: "DAIMON_TOKEN", ReceiptStorePath: storePath}}
}

func runTracker(ctx context.Context, tracker *receiptTracker) <-chan struct{} {
	done := make(chan struct{})
	go func() { tracker.Run(ctx); close(done) }()
	return done
}

func waitFor(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for receipt follower")
		}
		time.Sleep(time.Millisecond)
	}
}

func useFastReceiptPolling(t *testing.T) {
	t.Helper()
	priorBase, priorMax := receiptPollBaseDelay, receiptPollMaxDelay
	receiptPollBaseDelay, receiptPollMaxDelay = 2*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { receiptPollBaseDelay, receiptPollMaxDelay = priorBase, priorMax })
}
