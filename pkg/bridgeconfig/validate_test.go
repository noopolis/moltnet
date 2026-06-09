package bridgeconfig

import "testing"

func TestValidateWakeConfigHelpers(t *testing.T) {
	t.Parallel()

	valid := Config{
		Rooms: []RoomBinding{
			{ID: "research", Wake: WakeMentions},
		},
		DMs: &DMConfig{
			Enabled: true,
			Wake:    WakeNever,
		},
	}
	if err := validateWakeConfig(valid); err != nil {
		t.Fatalf("validateWakeConfig() error = %v", err)
	}

	if err := validateWakeConfig(Config{Rooms: []RoomBinding{{ID: "research", Wake: WakeConfig("weird")}}}); err == nil {
		t.Fatal("expected invalid room wake config error")
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
