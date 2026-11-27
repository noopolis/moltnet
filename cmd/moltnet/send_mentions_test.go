package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestRunSendWarnsOnStderrWhenAMentionMatchedNobody(t *testing.T) {
	tests := []struct {
		name       string
		unresolved []string
		wantWarn   bool
	}{
		{name: "unresolved mention", unresolved: []string{"scout"}, wantWarn: true},
		{name: "every mention resolved"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			writeClientConfigFixture(t, workspace, clientconfig.Config{
				Version: "moltnet.client.v1",
				Agent:   clientconfig.AgentConfig{Name: "Alpha", Runtime: "openclaw"},
				Attachments: []clientconfig.AttachmentConfig{{
					AgentName: "Alpha",
					Auth:      clientconfig.AuthConfig{Mode: "none"},
					BaseURL:   "http://127.0.0.1:8787",
					MemberID:  "alpha",
					NetworkID: "local_lab",
					Rooms:     []bridgeconfig.RoomBinding{{ID: "general"}},
				}},
			})

			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(response).Encode(protocol.MessageAccepted{
					Accepted:           true,
					EventID:            "evt_1",
					MessageID:          "msg_1",
					UnresolvedMentions: test.unresolved,
				})
			}))
			defer server.Close()
			rewriteClientConfigBaseURL(t, workspace, server.URL)

			cwd := mustGetwd(t)
			defer func() { _ = os.Chdir(cwd) }()
			if err := os.Chdir(workspace); err != nil {
				t.Fatalf("chdir: %v", err)
			}

			out, errOut := captureStdoutAndStderr(t, func() {
				if err := run(context.Background(), []string{
					"send", "--target", "room:general", "--text", "@scout the answer is above",
				}, "test"); err != nil {
					t.Fatalf("run() send error = %v", err)
				}
			})

			if !strings.Contains(out, `"accepted": true`) {
				t.Fatalf("the message is posted either way, got stdout %q", out)
			}
			warned := strings.Contains(errOut, "@scout matched no participant of room:general") &&
				strings.Contains(errOut, "moltnet participants --target room:general")
			if warned != test.wantWarn {
				t.Fatalf("warning on stderr = %v, want %v; stderr %q", warned, test.wantWarn, errOut)
			}
			if !test.wantWarn && errOut != "" {
				t.Fatalf("no mention failed, so stderr must stay empty, got %q", errOut)
			}
		})
	}
}
