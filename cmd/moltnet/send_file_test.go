package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestSendTextFilePreservesBodyAndDiscussionMetadata(t *testing.T) {
	body := "Grok 完整回复：\"hello world\"；$HOME 与 `literal`\n第二行"
	var received protocol.SendMessageRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(protocol.MessageAccepted{Accepted: true})
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("HOME", t.TempDir())
	writeOperatorFallbackServerConfig(t, filepath.Join(directory, "Moltnet"), parsed.Host, []operatorFallbackToken{
		{id: "operator", value: "test-secret", scopes: []string{"observe", "write", "admin", "pair"}},
	})
	path := filepath.Join(directory, "message.txt")
	if err := os.WriteFile(path, []byte("\ufeff"+body), 0600); err != nil {
		t.Fatal(err)
	}
	err := run(context.Background(), []string{"send", "--target", "room:general", "--reply-to", "msg_parent", "--discussion", "discussion_test", "--text-file", path}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(received.Parts) != 1 || received.Parts[0].Text != body || received.ReplyTo != "msg_parent" || received.DiscussionID != "discussion_test" {
		t.Fatalf("body or routing metadata changed: %+v", received)
	}
}

func TestSendTextFileRejectsInvalidAndConflictingInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "message.txt")
	for _, tc := range []struct{ name, data, want string }{
		{"oversized", strings.Repeat("a", (1<<20)+1), "1 MiB"},
		{"encoding", string([]byte{0xff}), "UTF-8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := readSendTextFile(path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s rejection, got %v", tc.want, err)
			}
		})
	}
	for _, extra := range [][]string{{"--text", "conflict"}, {"conflict"}} {
		args := append([]string{"--target", "room:general", "--text-file", path}, extra...)
		if err := runSend(args); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
			t.Fatalf("expected conflict before file or network access: %v", err)
		}
	}
	if _, err := readSendTextFile(path + ".missing"); err == nil {
		t.Fatal("missing file accepted")
	}
}
