// transcript.go writes transcript.json into the SAME directory as the causal
// event log, which the Spawnfile compiler mounts unconditionally so the social
// record survives container teardown even for a purely in-memory room store.
//
// THIS CHANGES THAT VOLUME'S CONTRACT, DELIBERATELY. It previously held only
// hashes: the causal log records content_sha256, never message text and never
// a sender. It now also holds message bodies. That is not an incidental
// widening — agent speech is the research data the memetics programme exists
// to study, and a run record that cannot show who said what to whom cannot
// support the claim that agents held a conversation.
//
// Two properties keep this an attestation rather than a second, drifting copy
// of the truth:
//
//   - Text is checkable. Each message's sha256(json.Marshal(Parts)) equals the
//     content_sha256 the causal log already recorded for that message_id, so a
//     reader verifies the transcript against the sealed log rather than
//     trusting it.
//   - Attribution is stated, never assumed. `from` is what the SENDER claimed
//     and a sender can claim anything; the authenticated identity is the causal
//     event's principal_id, which causalPrincipal() derives from auth claims
//     alone. The attestation block therefore reports three states and not two —
//     attested, violated, and unattested — because a network running
//     auth.mode: none stamps system:moltnet.anonymous and there is then nothing
//     to compare against. Unattested must never be read as passed.
package observability

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/noopolis/moltnet/pkg/protocol"
)

const transcriptVersion = "simfile.moltnet.transcript.v1"

type transcript struct {
	Attestation   transcriptAttestation    `json:"attestation"`
	Conversations []transcriptConversation `json:"conversations"`
	Source        string                   `json:"source"`
	Version       string                   `json:"version"`
}

type transcriptAttestation struct {
	States            []string         `json:"states"`
	Attribution       transcriptStatus `json:"attribution"`
	MessageBodies     transcriptStatus `json:"message_bodies"`
	TextAgainstCausal transcriptStatus `json:"text_against_causal"`
}

type transcriptStatus struct {
	Status       string `json:"status"`
	Reason       string `json:"reason,omitempty"`
	SinceVersion string `json:"since_version,omitempty"`
	RunID        string `json:"run_id,omitempty"`
	Algorithm    string `json:"algorithm,omitempty"`
	Source       string `json:"source,omitempty"`
}

type transcriptConversation struct {
	Messages []transcriptMessage `json:"messages"`
	Target   transcriptTarget    `json:"target"`
}

type transcriptMessage struct {
	From   protocol.Actor  `json:"from"`
	ID     string          `json:"id"`
	Parts  []protocol.Part `json:"parts"`
	Target protocol.Target `json:"target"`
}

type transcriptTarget struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Scope string `json:"scope,omitempty"`
}

// TranscriptWriter writes the social record beside causal.jsonl. Bodies are
// research data for the memetics programme, not an incidental widening of the
// causal volume: this artifact makes accepted message text available for
// replay and lets readers attest it against the adjacent content_sha256.
type TranscriptWriter struct {
	mu   sync.Mutex
	path string
	data transcript
}

func NewTranscriptFileWriter(path string, _ string) (*TranscriptWriter, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, fmt.Errorf("transcript writer: path is required")
	}
	data := transcript{
		Attestation: transcriptAttestation{
			States:            []string{"attested", "unattested", "violated"},
			Attribution:       transcriptStatus{Status: "unattested", Reason: "no accepted messages yet", Source: "causal.principal_id"},
			MessageBodies:     transcriptStatus{Status: "attested", SinceVersion: transcriptVersion, RunID: transcriptRunID()},
			TextAgainstCausal: transcriptStatus{Status: "attested", Reason: "sha256(json.Marshal(message.Parts)) equals causal content_sha256", Algorithm: "sha256(json.Marshal(message.Parts))", Source: "causal.jsonl.content_sha256"},
		},
		Conversations: []transcriptConversation{},
		Source:        "moltnet-exported",
		Version:       transcriptVersion,
	}
	if raw, err := os.ReadFile(trimmed); err == nil {
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, fmt.Errorf("transcript writer: decode existing %q: %w", trimmed, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("transcript writer: read %q: %w", trimmed, err)
	}
	if data.Conversations == nil {
		data.Conversations = []transcriptConversation{}
	}
	if len(data.Attestation.States) == 0 {
		data.Attestation.States = []string{"attested", "unattested", "violated"}
	}
	if data.Source == "" {
		data.Source = "moltnet-exported"
	}
	if data.Version == "" {
		data.Version = transcriptVersion
	}
	return &TranscriptWriter{path: trimmed, data: data}, nil
}

func (w *TranscriptWriter) Append(message protocol.Message, principalID string) error {
	if w == nil {
		return fmt.Errorf("transcript writer: writer is nil")
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	conversation := transcriptConversation{
		Messages: []transcriptMessage{transcriptMessageFrom(message)},
		Target:   transcriptTarget{Kind: message.Target.Kind, ID: messageTargetID(message.Target)},
	}
	if message.Target.Kind == protocol.TargetKindRoom || message.Target.Kind == protocol.TargetKindThread {
		conversation.Target.Kind = protocol.TargetKindRoom
		conversation.Target.Scope = "room:" + message.NetworkID + ":" + message.Target.RoomID
	}
	for index := range w.data.Conversations {
		if w.data.Conversations[index].Target == conversation.Target {
			w.data.Conversations[index].Messages = append(w.data.Conversations[index].Messages, transcriptMessageFrom(message))
			w.updateAttribution(principalID)
			return w.writeLocked()
		}
	}
	w.data.Conversations = append(w.data.Conversations, conversation)
	w.updateAttribution(principalID)
	return w.writeLocked()
}

func (w *TranscriptWriter) updateAttribution(principalID string) {
	if strings.HasPrefix(strings.TrimSpace(principalID), "system:moltnet.anonymous") {
		w.data.Attestation.Attribution = transcriptStatus{Status: "unattested", Reason: "accepted message principal is system:moltnet.anonymous", Source: "causal.principal_id"}
		return
	}
	w.data.Attestation.Attribution = transcriptStatus{Status: "attested", Reason: "principal_id is authenticated in adjacent causal event", Source: "causal.principal_id"}
}

func (w *TranscriptWriter) writeLocked() error {
	raw, err := json.MarshalIndent(w.data, "", "  ")
	if err != nil {
		return fmt.Errorf("transcript writer: encode: %w", err)
	}
	raw = append(raw, '\n')
	tmp := w.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("transcript writer: write: %w", err)
	}
	if err := os.Rename(tmp, w.path); err != nil {
		return fmt.Errorf("transcript writer: install: %w", err)
	}
	return nil
}

func (w *TranscriptWriter) Close() error { return nil }

func messageTargetID(target protocol.Target) string {
	if target.Kind == protocol.TargetKindDM {
		return target.DMID
	}
	return target.RoomID
}

func transcriptMessageFrom(message protocol.Message) transcriptMessage {
	return transcriptMessage{From: message.From, ID: message.ID, Parts: message.Parts, Target: message.Target}
}

func transcriptRunID() string {
	if runID := strings.TrimSpace(os.Getenv("NOOPOLIS_RUN_ID")); runID != "" {
		return runID
	}
	return "unset-run"
}
