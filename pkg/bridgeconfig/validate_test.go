package bridgeconfig

import "testing"

func TestValidateReadReplyConfigHelpers(t *testing.T) {
	t.Parallel()

	valid := Config{
		Read:  ReadAll,
		Reply: ReplyAuto,
		Rooms: []RoomBinding{
			{ID: "research", Read: ReadMentions, Reply: ReplyManual},
		},
		DMs: &DMConfig{
			Enabled: true,
			Read:    ReadMentions,
			Reply:   ReplyNever,
		},
	}
	if err := validateReadReplyConfig(valid); err != nil {
		t.Fatalf("validateReadReplyConfig() error = %v", err)
	}

	if err := validateReadReplyConfig(Config{Read: ReadConfig("weird")}); err == nil {
		t.Fatal("expected invalid top-level read config error")
	}
	if err := validateReadReplyConfig(Config{Reply: ReplyConfig("weird")}); err == nil {
		t.Fatal("expected invalid top-level reply config error")
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
