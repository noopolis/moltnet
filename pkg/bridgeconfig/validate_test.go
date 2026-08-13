package bridgeconfig

import (
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestValidateWakeConfigHelpers(t *testing.T) {
	t.Parallel()

	valid := Config{
		Rooms: []RoomBinding{
			{ID: "research", Wake: WakeMentions},
		},
		DMs: &DMConfig{
			Enabled:            true,
			Wake:               WakeNever,
			AllowedWakeSenders: []string{"world"},
		},
	}
	if err := validateWakeConfig(valid); err != nil {
		t.Fatalf("validateWakeConfig() error = %v", err)
	}

	if err := validateWakeConfig(Config{Rooms: []RoomBinding{{ID: "research", Wake: WakeConfig("weird")}}}); err == nil {
		t.Fatal("expected invalid room wake config error")
	}

	invalidSenders := [][]string{
		{" world "},
		{"world", " world "},
		{"pitch:world"},
		{"molt://pitch/agents/world"},
		{"bad sender"},
		{""},
		make([]string, protocol.MaxMembersPerRequest+1),
	}
	for _, senders := range invalidSenders {
		if err := validateWakeConfig(Config{DMs: &DMConfig{Enabled: true, AllowedWakeSenders: senders}}); err == nil {
			t.Fatalf("expected invalid allowed wake senders error for %#v", senders)
		}
	}
}

func TestValidateURLAndPrivateMode(t *testing.T) {
	t.Parallel()

	if err := validateURL("moltnet", "http://127.0.0.1:8787"); err != nil {
		t.Fatalf("validateURL() error = %v", err)
	}
	if err := validateURL("moltnet", "https://example.com"); err != nil {
		t.Fatalf("validateURL() https error = %v", err)
	}
	if err := validateURL("moltnet", "ftp://example.com"); err == nil {
		t.Fatal("expected unsupported scheme error")
	}
	if err := validateURL("moltnet", "http:///no-host"); err == nil {
		t.Fatal("expected missing host error")
	}
}
