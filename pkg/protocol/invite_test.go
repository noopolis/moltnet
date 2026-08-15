package protocol

import (
	"strings"
	"testing"
	"time"
)

func validInvite() Invite {
	return Invite{
		V:            InviteVersion,
		RelayURL:     "wss://moltnet-relay.acme.workers.dev",
		Room:         "room-token",
		RelayToken:   "relay-token",
		PairingToken: "pairing-token",
		PairingID:    "friend-net",
		NetworkID:    "alice-net",
		NetworkName:  "Alice's Moltnet",
		SharedRooms:  []string{"chat"},
		Exp:          time.Now().Add(DefaultInviteTTL),
	}
}

func TestEncodeDecodeInviteRoundTrip(t *testing.T) {
	t.Parallel()

	invite := validInvite()
	code, err := EncodeInvite(invite)
	if err != nil {
		t.Fatalf("EncodeInvite() error = %v", err)
	}
	if !strings.HasPrefix(code, InvitePrefix) {
		t.Fatalf("invite code %q missing prefix %q", code, InvitePrefix)
	}

	decoded, err := DecodeInvite(code)
	if err != nil {
		t.Fatalf("DecodeInvite() error = %v", err)
	}
	if decoded.RelayURL != invite.RelayURL ||
		decoded.Room != invite.Room ||
		decoded.RelayToken != invite.RelayToken ||
		decoded.PairingToken != invite.PairingToken ||
		decoded.PairingID != invite.PairingID ||
		decoded.NetworkID != invite.NetworkID ||
		decoded.NetworkName != invite.NetworkName ||
		len(decoded.SharedRooms) != 1 || decoded.SharedRooms[0] != "chat" {
		t.Fatalf("decoded invite = %#v, want %#v", decoded, invite)
	}
	if decoded.V != InviteVersion {
		t.Fatalf("decoded invite version = %d, want %d", decoded.V, InviteVersion)
	}
}

func TestDecodeInviteRejectsMissingPrefix(t *testing.T) {
	t.Parallel()

	if _, err := DecodeInvite("not-an-invite"); err == nil {
		t.Fatal("expected error for missing prefix")
	}
}

func TestDecodeInviteRejectsMalformedBase64(t *testing.T) {
	t.Parallel()

	if _, err := DecodeInvite(InvitePrefix + "not-base64!!!"); err == nil {
		t.Fatal("expected error for malformed base64")
	}
}

func TestDecodeInviteRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	code := InvitePrefix + "bm90LWpzb24" // base64url("not-json") w/o padding
	if _, err := DecodeInvite(code); err == nil {
		t.Fatal("expected error for malformed JSON payload")
	}
}

func TestInviteValidateRejectsWrongVersion(t *testing.T) {
	t.Parallel()

	invite := validInvite()
	invite.V = 2
	if err := invite.Validate(); err == nil {
		t.Fatal("expected error for unsupported version")
	}
}

func TestInviteValidateRejectsNonRelayScheme(t *testing.T) {
	t.Parallel()

	for _, url := range []string{
		"https://moltnet-relay.acme.workers.dev",
		"not a url\x7f",
		"wss://",
	} {
		invite := validInvite()
		invite.RelayURL = url
		if err := invite.Validate(); err == nil {
			t.Fatalf("expected error for relay_url %q", url)
		}
	}
}

func TestInviteValidateRejectsEmptyRequiredFields(t *testing.T) {
	t.Parallel()

	base := validInvite()
	tests := []func(*Invite){
		func(i *Invite) { i.Room = "" },
		func(i *Invite) { i.RelayToken = "" },
		func(i *Invite) { i.PairingToken = "" },
		func(i *Invite) { i.PairingID = "" },
		func(i *Invite) { i.NetworkID = "" },
		func(i *Invite) { i.Exp = time.Time{} },
	}
	for _, mutate := range tests {
		invite := base
		mutate(&invite)
		if err := invite.Validate(); err == nil {
			t.Fatalf("expected validation error for mutated invite %#v", invite)
		}
	}
}

func TestInviteValidateRejectsExpiredInvite(t *testing.T) {
	t.Parallel()

	invite := validInvite()
	invite.Exp = time.Now().Add(-time.Minute)
	if err := invite.Validate(); err == nil {
		t.Fatal("expected error for expired invite")
	}
}

func TestInviteValidateRejectsInvalidSharedRoomID(t *testing.T) {
	t.Parallel()

	invite := validInvite()
	invite.SharedRooms = []string{"bad room id!"}
	err := invite.Validate()
	if err == nil {
		t.Fatal("expected error for invalid shared_rooms id")
	}
	if !strings.Contains(err.Error(), "bad room id!") {
		t.Fatalf("expected error to name the bad id, got %q", err.Error())
	}
}

func TestDecodeInviteRejectsInvalidSharedRoomID(t *testing.T) {
	t.Parallel()

	invite := validInvite()
	invite.SharedRooms = []string{"bad room id!"}
	code, err := EncodeInvite(invite)
	if err != nil {
		t.Fatalf("EncodeInvite() error = %v", err)
	}
	if _, err := DecodeInvite(code); err == nil {
		t.Fatal("expected error decoding an invite with an invalid shared_rooms id")
	}
}

func TestDecodeInviteRejectsExpiredInvite(t *testing.T) {
	t.Parallel()

	invite := validInvite()
	invite.Exp = time.Now().Add(-time.Hour)
	code, err := EncodeInvite(invite)
	if err != nil {
		t.Fatalf("EncodeInvite() error = %v", err)
	}
	if _, err := DecodeInvite(code); err == nil {
		t.Fatal("expected error for expired invite")
	}
}
