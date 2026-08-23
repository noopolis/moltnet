package main

import (
	"bytes"
	"testing"

	"github.com/noopolis/moltnet/pkg/nodeconfig"
	"go.yaml.in/yaml/v3"
)

// TestDefaultMoltnetNodeConfigQuotesBaseURL pins finding 4: base_url must be
// rendered through %q, not a bare %s, so a baseURL value containing a YAML
// comment marker or an embedded newline can never corrupt the document or
// silently truncate the parsed value.
//
// consoleBaseURL (console.go) only ever derives baseURL from a --listen
// value that has already passed app.ValidateListenAddr's host:port syntax
// check -- but that check deliberately never validates the host's own
// character content beyond bracket-balancing an IPv6 literal (see its own
// doc comment: a stricter grammar risks rejecting a real, already-working
// listen_addr it has never seen before). A value like "127.0.0.1 #x:9911"
// or one containing a literal newline reaches consoleBaseURL, and from there
// this function, verbatim -- and net.SplitHostPort accepts both.
//
// Decoding here uses the exact strict path nodeconfig.LoadFile
// (pkg/nodeconfig/config.go) uses to load a real MoltnetNode file --
// yaml.NewDecoder(...).KnownFields(true) -- rather than a lenient
// yaml.Unmarshal, so this test also catches the newline case's pre-fix
// failure mode: an unescaped embedded newline splits base_url's scalar
// across lines, turning its remainder into an unrelated sibling top-level
// key, which KnownFields(true) rejects outright as a decode error.
func TestDefaultMoltnetNodeConfigQuotesBaseURL(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
	}{
		{"hash comment marker", "http://127.0.0.1 #x:9911"},
		{"embedded newline", "http://a\nb:9911"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			contents := defaultMoltnetNodeConfig("local", testCase.baseURL)

			var config nodeconfig.Config
			decoder := yaml.NewDecoder(bytes.NewReader([]byte(contents)))
			decoder.KnownFields(true)
			if err := decoder.Decode(&config); err != nil {
				t.Fatalf("strict decode of rendered MoltnetNode config failed: %v; contents:\n%s", err, contents)
			}

			if config.Moltnet.BaseURL != testCase.baseURL {
				t.Fatalf("base_url round-tripped to %q, want %q; contents:\n%s", config.Moltnet.BaseURL, testCase.baseURL, contents)
			}
			if config.Moltnet.NetworkID != "local" {
				t.Fatalf("network_id corrupted to %q by an unescaped base_url; contents:\n%s", config.Moltnet.NetworkID, contents)
			}
		})
	}
}

// TestDefaultMoltnetNodeConfigOrdinaryBaseURLUnchanged is the negative half:
// the overwhelmingly common case -- an ordinary "http://host:port" baseURL,
// exactly what consoleBaseURL produces for every real --listen value seen in
// practice -- must still round-trip byte-for-byte the same whether base_url
// is rendered with %s or %q, so this fix changes nothing observable for any
// existing config.
func TestDefaultMoltnetNodeConfigOrdinaryBaseURLUnchanged(t *testing.T) {
	contents := defaultMoltnetNodeConfig("local", "http://127.0.0.1:8787")

	var config nodeconfig.Config
	decoder := yaml.NewDecoder(bytes.NewReader([]byte(contents)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		t.Fatalf("strict decode failed: %v; contents:\n%s", err, contents)
	}
	if config.Moltnet.BaseURL != "http://127.0.0.1:8787" {
		t.Fatalf("base_url = %q, want unchanged http://127.0.0.1:8787", config.Moltnet.BaseURL)
	}
	if config.Moltnet.NetworkID != "local" {
		t.Fatalf("network_id = %q, want \"local\"", config.Moltnet.NetworkID)
	}
}
