package clientconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAttachmentValidateRejectsInvalidOutboundDMPeers(t *testing.T) {
	for _, peers := range [][]string{
		{""},
		{"bad peer"},
		{" agent"},
		{"agent "},
		{"net:agent"},
		{"molt://net/agents/world"},
		{"peer", "peer"},
		{"agent"},
	} {
		attachment := AttachmentConfig{
			Auth:            AuthConfig{Mode: "none"},
			BaseURL:         "http://127.0.0.1:8787",
			MemberID:        "agent",
			NetworkID:       "local",
			OutboundDMPEers: peers,
		}
		if err := attachment.Validate(); err == nil {
			t.Fatalf("expected invalid outbound dm peer validation for %#v", peers)
		}
	}
}

func TestValidateOutboundDMConfigPeersBounds(t *testing.T) {
	maxSized := make([]string, MaxOutboundDMPeers)
	for index := range maxSized {
		maxSized[index] = fmt.Sprintf("peer-%03d", index)
	}
	oversized := make([]string, MaxOutboundDMPeers+1)
	for index := range oversized {
		oversized[index] = fmt.Sprintf("peer-%03d", index)
	}

	attachment := AttachmentConfig{
		Auth:            AuthConfig{Mode: "none"},
		BaseURL:         "http://127.0.0.1:8787",
		MemberID:        "agent",
		NetworkID:       "local",
		OutboundDMPEers: maxSized,
	}
	if err := attachment.Validate(); err != nil {
		t.Fatalf("expected max outbound peer list to validate, got %v", err)
	}

	attachment = AttachmentConfig{
		Auth:            AuthConfig{Mode: "none"},
		BaseURL:         "http://127.0.0.1:8787",
		MemberID:        "agent",
		NetworkID:       "local",
		OutboundDMPEers: oversized,
	}
	if err := attachment.Validate(); err == nil {
		t.Fatalf("expected oversized outbound dm peer list validation error")
	}
}

func TestValidateOutboundDMConfigPeersDuplicateErrorOmitsUntrustedValue(t *testing.T) {
	peer := "magt_v1_secret_sentinel"
	attachment := AttachmentConfig{
		Auth:            AuthConfig{Mode: "none"},
		BaseURL:         "http://127.0.0.1:8787",
		MemberID:        "actor",
		NetworkID:       "local",
		OutboundDMPEers: []string{peer, peer},
	}
	err := attachment.Validate()
	if err == nil {
		t.Fatal("expected duplicate peer validation error")
	}
	if !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("expected duplicate-peer error, got %v", err)
	}
	if strings.Contains(err.Error(), peer) {
		t.Fatalf("duplicate peer validation error must not echo untrusted peer value: %v", err)
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "sentinel") {
		t.Fatalf("duplicate peer validation error must not leak credential fragment: %v", err)
	}
	if !strings.Contains(err.Error(), outboundDMPSentinelDup) {
		t.Fatalf("expected duplicate peer validation error to include a token sentinel: %v", err)
	}
}

func TestOutboundDMPeersAbsentAndExplicitEmpty(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		expected int
	}{
		{
			name: "absent",
			payload: `{
				"version":"moltnet.client.v1",
				"attachments":[{"auth":{"mode":"none"},"base_url":"http://127.0.0.1:8787","member_id":"agent","network_id":"local"}]
			}`,
			expected: 0,
		},
		{
			name: "explicit_empty",
			payload: `{
				"version":"moltnet.client.v1",
				"attachments":[{"auth":{"mode":"none"},"base_url":"http://127.0.0.1:8787","member_id":"agent","network_id":"local","outbound_dm_peers":[]}]
			}`,
			expected: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(test.payload), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}

			config, err := LoadFile(path)
			if err != nil {
				t.Fatalf("LoadFile() error = %v", err)
			}
			if len(config.Attachments) != 1 {
				t.Fatalf("unexpected number of attachments = %d", len(config.Attachments))
			}
			if len(config.Attachments[0].OutboundDMPEers) != test.expected {
				t.Fatalf("unexpected outbound_dm_peers length = %d, want %d", len(config.Attachments[0].OutboundDMPEers), test.expected)
			}
		})
	}
}

func TestOutboundDMPeersJSONBytes(t *testing.T) {
	withoutPeers := Config{
		Version: VersionV1,
		Attachments: []AttachmentConfig{{
			Auth:      AuthConfig{Mode: "none"},
			BaseURL:   "http://127.0.0.1:8787",
			MemberID:  "agent",
			NetworkID: "local",
		}},
	}
	withPeers := Config{
		Version: VersionV1,
		Attachments: []AttachmentConfig{{
			Auth:            AuthConfig{Mode: "none"},
			BaseURL:         "http://127.0.0.1:8787",
			MemberID:        "agent",
			NetworkID:       "local",
			OutboundDMPEers: []string{"zeta", "alpha", "beta"},
		}},
	}
	gotWithoutPeers, err := json.Marshal(withoutPeers)
	if err != nil {
		t.Fatalf("marshal config without peers: %v", err)
	}
	expectedWithoutPeers := `{"version":"moltnet.client.v1","agent":{},"attachments":[{"auth":{"mode":"none"},"base_url":"http://127.0.0.1:8787","member_id":"agent","network_id":"local"}]}`
	if string(gotWithoutPeers) != expectedWithoutPeers {
		t.Fatalf("unexpected emitted json without peers\ngot:  %q\nwant: %q", gotWithoutPeers, expectedWithoutPeers)
	}

	gotWithPeers, err := json.Marshal(withPeers)
	if err != nil {
		t.Fatalf("marshal config with peers: %v", err)
	}
	expectedWithPeers := `{"version":"moltnet.client.v1","agent":{},"attachments":[{"auth":{"mode":"none"},"base_url":"http://127.0.0.1:8787","outbound_dm_peers":["zeta","alpha","beta"],"member_id":"agent","network_id":"local"}]}`
	if string(gotWithPeers) != expectedWithPeers {
		t.Fatalf("unexpected emitted json with peers\ngot:  %q\nwant: %q", gotWithPeers, expectedWithPeers)
	}

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
		"version":"moltnet.client.v1",
		"attachments":[{"auth":{"mode":"none"},"base_url":"http://127.0.0.1:8787","member_id":"agent","network_id":"local","outbound_dm_peers":[]}]
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	emitted, err := json.Marshal(loaded)
	if err != nil {
		t.Fatalf("marshal loaded config: %v", err)
	}
	if strings.Contains(string(emitted), "\"outbound_dm_peers\"") {
		t.Fatalf("expected omitted outbound_dm_peers for explicit empty slice, got %s", emitted)
	}
}

func TestOutboundDMPeersJSONRoundTripAndResolutionCopies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
		"version":"moltnet.client.v1",
		"attachments":[{"auth":{"mode":"none"},"base_url":"http://127.0.0.1:8787","member_id":"agent","network_id":"local","outbound_dm_peers":["zeta","alpha","beta"]}
		]}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	config, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}

	if got := config.Attachments[0].OutboundDMPeers(); !reflect.DeepEqual(got, []string{"zeta", "alpha", "beta"}) {
		t.Fatalf("expected outbound_dm_peers order = %#v, got %#v", []string{"zeta", "alpha", "beta"}, got)
	}

	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	var roundTrip Config
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if !reflect.DeepEqual(roundTrip.Attachments[0].OutboundDMPEers, config.Attachments[0].OutboundDMPEers) {
		t.Fatalf("round-trip peers mismatch: %#v vs %#v", roundTrip.Attachments[0].OutboundDMPEers, config.Attachments[0].OutboundDMPEers)
	}

	resolvedOne, err := config.ResolveAttachment("local")
	if err != nil {
		t.Fatalf("ResolveAttachment() error = %v", err)
	}
	resolvedOne.OutboundDMPEers[0] = "tampered"
	if config.Attachments[0].OutboundDMPEers[0] != "zeta" {
		t.Fatalf("source-config mutation after resolve observed in source")
	}

	view := resolvedOne.OutboundDMPeers()
	view[0] = "tampered-view"
	if resolvedOne.OutboundDMPEers[0] != "tampered" {
		t.Fatalf("accessor-result mutation affected resolved attachment: %#v", resolvedOne)
	}

	resolvedTwo, err := config.ResolveAttachment("local")
	if err != nil {
		t.Fatalf("ResolveAttachment() error = %v", err)
	}
	if len(resolvedTwo.OutboundDMPEers) != 3 || resolvedTwo.OutboundDMPEers[0] != "zeta" {
		t.Fatalf("resolved mutation observed across resolves: %#v", resolvedTwo.OutboundDMPEers)
	}

	config.Attachments[0].OutboundDMPEers[0] = "tampered-source"
	if resolvedOne.OutboundDMPEers[0] != "tampered" {
		t.Fatalf("earlier resolved attachment should retain own pre-mutation value: %#v", resolvedOne.OutboundDMPEers)
	}
	if resolvedTwo.OutboundDMPEers[0] != "zeta" {
		t.Fatalf("earlier resolved view should retain pre-mutation value: %#v", resolvedTwo.OutboundDMPEers)
	}

	resolvedThree, err := config.ResolveAttachment("local")
	if err != nil {
		t.Fatalf("ResolveAttachment() error = %v", err)
	}
	if resolvedThree.OutboundDMPEers[0] != "tampered-source" {
		t.Fatalf("source mutation should not affect prior resolved attachment: %#v", resolvedThree.OutboundDMPEers)
	}
}
