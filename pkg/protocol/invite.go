package protocol

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// InvitePrefix marks a string as a Moltnet relay pairing invite. The
// remainder of the string is base64url(JSON) of an Invite.
const InvitePrefix = "moltnet-invite:"

// InviteVersion is the only supported Invite schema version.
const InviteVersion = 1

// DefaultInviteTTL is the default lifetime of a generated invite. An invite
// is a bearer credential (relay + pairing tokens in plaintext), so this
// bounds how long a leaked or unused invite stays valid.
const DefaultInviteTTL = 7 * 24 * time.Hour

// Invite is the payload of a shareable `moltnet-invite:` code. Fields are
// intentionally plain strings, not SecretString: the invite's entire purpose
// is to carry plaintext secrets to the peer that consumes it, and
// SecretString's redacted marshaling would defeat that.
type Invite struct {
	V            int       `json:"v"`
	RelayURL     string    `json:"relay_url"`
	Room         string    `json:"room"`
	RelayToken   string    `json:"relay_token"`
	PairingToken string    `json:"pairing_token"`
	PairingID    string    `json:"pairing_id"`
	NetworkID    string    `json:"network_id"`
	NetworkName  string    `json:"network_name,omitempty"`
	SharedRooms  []string  `json:"shared_rooms,omitempty"`
	Exp          time.Time `json:"exp"`
}

// EncodeInvite renders invite as a `moltnet-invite:` code. The version field
// is always stamped to InviteVersion regardless of the caller-supplied value.
func EncodeInvite(invite Invite) (string, error) {
	invite.V = InviteVersion

	payload, err := json.Marshal(invite)
	if err != nil {
		return "", fmt.Errorf("encode invite: %w", err)
	}

	return InvitePrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecodeInvite parses and validates a `moltnet-invite:` code.
func DecodeInvite(code string) (Invite, error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(code), InvitePrefix)
	if !ok {
		return Invite{}, fmt.Errorf("invite code must start with %q", InvitePrefix)
	}

	payload, err := base64.RawURLEncoding.DecodeString(rest)
	if err != nil {
		return Invite{}, fmt.Errorf("decode invite: %w", err)
	}

	var invite Invite
	if err := json.Unmarshal(payload, &invite); err != nil {
		return Invite{}, fmt.Errorf("decode invite JSON: %w", err)
	}
	if err := invite.Validate(); err != nil {
		return Invite{}, err
	}

	return invite, nil
}

// Validate checks the invite's version, relay URL scheme, required fields,
// and expiry. It does not know the local network id, so it cannot check for
// a network-id collision; callers that have a local config must do that
// themselves.
func (invite Invite) Validate() error {
	if invite.V != InviteVersion {
		return fmt.Errorf("unsupported invite version %d", invite.V)
	}
	if err := validateInviteRelayURL(invite.RelayURL); err != nil {
		return err
	}
	if strings.TrimSpace(invite.Room) == "" {
		return errors.New("invite room is required")
	}
	if strings.TrimSpace(invite.RelayToken) == "" {
		return errors.New("invite relay_token is required")
	}
	if strings.TrimSpace(invite.PairingToken) == "" {
		return errors.New("invite pairing_token is required")
	}
	if strings.TrimSpace(invite.PairingID) == "" {
		return errors.New("invite pairing_id is required")
	}
	if strings.TrimSpace(invite.NetworkID) == "" {
		return errors.New("invite network_id is required")
	}
	if invite.Exp.IsZero() {
		return errors.New("invite exp is required")
	}
	if time.Now().After(invite.Exp) {
		return fmt.Errorf("invite expired at %s", invite.Exp.Format(time.RFC3339))
	}
	for _, roomID := range UniqueTrimmedStrings(invite.SharedRooms) {
		if err := ValidateRoomID(roomID); err != nil {
			return fmt.Errorf("invite shared_rooms entry %q is invalid: %w", roomID, err)
		}
	}

	return nil
}

func validateInviteRelayURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("invite relay_url is invalid: %w", err)
	}

	switch parsed.Scheme {
	case "ws", "wss":
	default:
		return fmt.Errorf("invite relay_url scheme %q is unsupported", parsed.Scheme)
	}
	if parsed.Host == "" {
		return errors.New("invite relay_url host is required")
	}

	return nil
}
