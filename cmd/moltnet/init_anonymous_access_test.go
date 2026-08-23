package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// This file covers P1-2 (the idempotent-rerun false note) and P2-2 (the
// starter room's anonymous readability) against real `runInit` output --
// split out of init_operator_token_test.go to keep that file under the
// repo's 400-line limit.

// TestRunInitRerunOnHealthyNetworkStaysQuiet is P1-2's fix: `moltnet init`
// re-run against a network that already has its own admin-capable operator
// token (the shape phase 7.0's first run always produces) must not print
// the "already has auth.tokens; left it untouched ... edit auth: there by
// hand" note. That note used to fire on ANY existing auth.tokens[],
// including this exact, most-repeated case in the whole CLI -- telling an
// operator whose network is working fine to go hand-edit their config for
// no reason at all.
func TestRunInitRerunOnHealthyNetworkStaysQuiet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "acme"}); err != nil {
			t.Fatalf("runInit() first run error = %v", err)
		}
	})

	output := captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "acme"}); err != nil {
			t.Fatalf("runInit() rerun error = %v", err)
		}
	})
	if strings.Contains(output, "already has auth.tokens") {
		t.Fatalf("expected the idempotent rerun against a healthy network to stay quiet about auth.tokens, got %q", output)
	}
}

// TestRunInitStarterRoomIsAnonymouslyReadable is P2-2's mutation-test fix:
// deleting `visibility: public` from defaultMoltnetConfig (templates.go)
// used to break no test in this repo at all. Read access and write_policy
// are two fully independent axes (internal/rooms/access_policy.go's
// canReadRoom never consults write_policy), so write_policy:
// registered_agents alone -- which every other init test already pins --
// proves nothing about whether GET /install.md's own room listing or an
// anonymous visitor can ever see "general". This test drives a real
// internal/app.App built from runInit's actual output (not a hand-built
// equivalent config), so a regression that quietly dropped or narrowed
// visibility: public in templates.go would fail here.
//
// Both read paths are checked: REST history (GET
// /v1/rooms/general/messages) and the live SSE event stream (GET
// /v1/events/stream) -- a second, independent security review of this same
// unit pointed out that anonymous callers reach "general" through both
// (internal/transport/sse.go's public-open event filter forwards
// MessageCreated for any room the anonymous read path can already resolve),
// so a test that only checked REST would still miss a regression that widened
// or narrowed the live stream's filter independently of the REST path.
func TestRunInitStarterRoomIsAnonymouslyReadable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	captureStdout(t, func() {
		if err := runInit(context.Background(), []string{"--id", "acme"}); err != nil {
			t.Fatalf("runInit() error = %v", err)
		}
	})
	configPath := filepath.Join(home, ".moltnet", "acme", "Moltnet")
	rewriteConfigListenAddr(t, configPath, freeLoopbackAddr(t))
	server := startRealMoltnetServer(t, configPath)

	// Open the anonymous SSE connection before the message is sent, so the
	// live-event path -- not just replay -- is what actually gets proven.
	streamRequest, err := http.NewRequest(http.MethodGet, server.URL+"/v1/events/stream", nil)
	if err != nil {
		t.Fatalf("new stream request: %v", err)
	}
	streamResponse, err := http.DefaultClient.Do(streamRequest)
	if err != nil {
		t.Fatalf("open anonymous sse stream: %v", err)
	}
	defer streamResponse.Body.Close()
	if streamResponse.StatusCode != http.StatusOK {
		t.Fatalf("anonymous sse stream status = %d, want %d", streamResponse.StatusCode, http.StatusOK)
	}
	sseLines := make(chan string, 64)
	go func() {
		scanner := bufio.NewScanner(streamResponse.Body)
		for scanner.Scan() {
			sseLines <- scanner.Text()
		}
	}()

	// Self-register (open registration, no operator credential involved)
	// and post into "general" -- write_policy: registered_agents grants
	// this with no membership check, exactly as templates.go's own doc
	// comment describes.
	registerResponse, err := http.Post(server.URL+"/v1/agents/register", "application/json", strings.NewReader(`{"requested_agent_id":"scout"}`))
	if err != nil {
		t.Fatalf("POST /v1/agents/register error = %v", err)
	}
	defer registerResponse.Body.Close()
	if registerResponse.StatusCode != http.StatusCreated {
		t.Fatalf("register scout = %d, want %d", registerResponse.StatusCode, http.StatusCreated)
	}
	var registration protocol.AgentRegistration
	if err := json.NewDecoder(registerResponse.Body).Decode(&registration); err != nil {
		t.Fatalf("decode registration: %v", err)
	}

	const sentinel = "anonymous-read-sentinel"
	sendBody, err := json.Marshal(protocol.SendMessageRequest{
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "general"},
		From:   protocol.Actor{Type: "agent", ID: registration.AgentID},
		Parts:  []protocol.Part{{Kind: protocol.PartKindText, Text: sentinel}},
	})
	if err != nil {
		t.Fatalf("marshal send request: %v", err)
	}
	sendRequest, err := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", bytes.NewReader(sendBody))
	if err != nil {
		t.Fatalf("new send request: %v", err)
	}
	sendRequest.Header.Set("Content-Type", "application/json")
	sendRequest.Header.Set("Authorization", "Bearer "+registration.AgentToken)
	sendResponse, err := http.DefaultClient.Do(sendRequest)
	if err != nil {
		t.Fatalf("POST /v1/messages error = %v", err)
	}
	defer sendResponse.Body.Close()
	if sendResponse.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(sendResponse.Body)
		t.Fatalf("send to general = %d, want %d: %s", sendResponse.StatusCode, http.StatusAccepted, body)
	}

	// REST: anonymous read of the room's message history.
	readResponse, err := http.Get(server.URL + "/v1/rooms/general/messages")
	if err != nil {
		t.Fatalf("anonymous GET /v1/rooms/general/messages: %v", err)
	}
	defer readResponse.Body.Close()
	if readResponse.StatusCode != http.StatusOK {
		t.Fatalf("anonymous room read status = %d, want %d", readResponse.StatusCode, http.StatusOK)
	}
	var page protocol.MessagePage
	if err := json.NewDecoder(readResponse.Body).Decode(&page); err != nil {
		t.Fatalf("decode room messages: %v", err)
	}
	found := false
	for _, message := range page.Messages {
		for _, part := range message.Parts {
			if part.Text == sentinel {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected anonymous REST read to see %q, got %#v", sentinel, page)
	}

	// SSE: the same message must also reach an anonymous live subscriber,
	// not just REST history.
	timeout := time.After(2 * time.Second)
	for {
		select {
		case line := <-sseLines:
			if strings.Contains(line, sentinel) {
				return
			}
		case <-timeout:
			t.Fatal("timed out waiting for the sentinel message on the anonymous SSE stream")
		}
	}
}
