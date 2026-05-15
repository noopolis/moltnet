package protocol

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestNetworkCompatibilityMetadataJSON(t *testing.T) {
	t.Parallel()

	network := Network{
		ID:      "local",
		Name:    "Local",
		Version: "test",
		Protocols: NetworkProtocols{
			HTTP:   []string{HTTPProtocolV1},
			Attach: []string{AttachmentProtocolV1},
			Pair:   []string{PairProtocolV1},
		},
		Capabilities: NetworkCapabilities{
			EventStream:        "sse",
			AttachmentProtocol: "websocket",
			DebugEvents:        true,
			HumanIngress:       true,
			DirectMessages:     false,
			MessagePagination:  "cursor",
			Pairings:           true,
		},
		Warnings: []NetworkWarning{{
			Severity: "warning",
			Code:     "storage.sqlite.backup_recommended",
			Message:  "Back up the database before restart.",
			Action:   "Run sqlite3 .backup before restart.",
			DocsURL:  "https://moltnet.dev/docs/guides/operating-moltnet/",
		}},
	}

	payload, err := json.Marshal(network)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	text := string(payload)
	for _, want := range []string{
		`"http":["moltnet.http.v1"]`,
		`"attach":["moltnet.attach.v1"]`,
		`"pair":["moltnet.pair.v1"]`,
		`"docs_url":"https://moltnet.dev/docs/guides/operating-moltnet/"`,
		`"direct_messages":false`,
		`"debug_events":true`,
		`"attachment_protocol":"websocket"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("network JSON missing %q: %s", want, text)
		}
	}

	var decoded Network
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !slices.Equal(decoded.Protocols.HTTP, []string{HTTPProtocolV1}) ||
		!slices.Equal(decoded.Protocols.Attach, []string{AttachmentProtocolV1}) ||
		!slices.Equal(decoded.Protocols.Pair, []string{PairProtocolV1}) {
		t.Fatalf("unexpected protocols %#v", decoded.Protocols)
	}
	if len(decoded.Warnings) != 1 ||
		decoded.Warnings[0].Severity != "warning" ||
		decoded.Warnings[0].Code != "storage.sqlite.backup_recommended" ||
		decoded.Warnings[0].Message == "" ||
		decoded.Warnings[0].Action == "" ||
		decoded.Warnings[0].DocsURL == "" {
		t.Fatalf("unexpected warnings %#v", decoded.Warnings)
	}
}
