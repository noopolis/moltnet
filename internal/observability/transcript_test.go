package observability

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestTranscriptWriterExportsContractAndAttestation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.json")
	writer, err := NewTranscriptFileWriter(path, "office")
	if err != nil {
		t.Fatal(err)
	}
	message := protocol.Message{
		ID:        "msg_1",
		NetworkID: "office",
		Target:    protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		From:      protocol.Actor{Type: "agent", ID: "claimed"},
		Parts:     []protocol.Part{{Kind: "text", Text: "hello"}},
	}
	if err := writer.Append(message, "system:moltnet.anonymous"); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var artifact struct {
		Attestation struct {
			Attribution struct {
				Status string `json:"status"`
			} `json:"attribution"`
			MessageBodies struct {
				Status string `json:"status"`
			} `json:"message_bodies"`
			TextAgainstCausal struct {
				Status string `json:"status"`
			} `json:"text_against_causal"`
		} `json:"attestation"`
		Conversations []struct {
			Messages []struct {
				ID    string          `json:"id"`
				From  protocol.Actor  `json:"from"`
				Parts []protocol.Part `json:"parts"`
			} `json:"messages"`
		} `json:"conversations"`
		Source  string `json:"source"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.Version != transcriptVersion || artifact.Source != "moltnet-exported" {
		t.Fatalf("contract identity = %q/%q", artifact.Source, artifact.Version)
	}
	if artifact.Attestation.MessageBodies.Status != "attested" || artifact.Attestation.TextAgainstCausal.Status != "attested" || artifact.Attestation.Attribution.Status != "unattested" {
		t.Fatalf("attestation = %#v", artifact.Attestation)
	}
	if got := artifact.Conversations[0].Messages[0]; got.ID != message.ID || got.From.ID != message.From.ID || got.Parts[0].Text != message.Parts[0].Text {
		t.Fatalf("message = %#v", got)
	}
	parts, _ := json.Marshal(message.Parts)
	sum := sha256.Sum256(parts)
	if hex.EncodeToString(sum[:]) == "" {
		t.Fatal("checksum was empty")
	}
}

func TestTranscriptWriterAttestsAuthenticatedPrincipal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.json")
	writer, err := NewTranscriptFileWriter(path, "office")
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Append(protocol.Message{
		ID: "msg_1", NetworkID: "office",
		Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm_1"},
		From:   protocol.Actor{Type: "agent", ID: "claimed"},
		Parts:  []protocol.Part{{Kind: "text", Text: "hello"}},
	}, "operator:token:red-agent"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !stringContains(string(raw), `"status": "attested"`) {
		t.Fatalf("authenticated attestation missing: %s", raw)
	}
}

func stringContains(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
