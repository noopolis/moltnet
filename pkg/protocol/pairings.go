package protocol

import "time"

const (
	PairingStatusConnected = "connected"
	// PairingStatusPending marks a pairing whose peer has never answered:
	// the invite exists but the remote network has not joined yet. It is an
	// expected onboarding state, not a fault, so it raises no operator warning.
	PairingStatusPending      = "pending"
	PairingStatusDegraded     = "degraded"
	PairingStatusIncompatible = "incompatible"
	PairingStatusError        = "error"
	PairingStatusUnknown      = "unknown"
	// PairingStatusRevoked marks a pairing `moltnet pair revoke` has torn
	// down: both the pairings[] entry and the peer's inbound auth.tokens[]
	// credential are removed from config together (see
	// internal/app/config_writeback.go's RevokePairing), so in practice a
	// revoked pairing no longer exists in config at all rather than lingering
	// with this status. It exists as a constant for callers that need to
	// name the state in transit -- e.g. printing what is about to happen, or
	// a future soft-revoke that marks status before the config write lands --
	// without inventing an ad hoc string.
	PairingStatusRevoked = "revoked"
)

type PairingRelay struct {
	URL  string `json:"url" yaml:"url"`
	Room string `json:"room,omitempty" yaml:"room,omitempty"`
	// Token is used solely to open the relay WebSocket connection and matches
	// the relay's RELAY_TOKEN. When unset, internal/app falls back to
	// Pairing.Token for that connection; this transport-neutral type makes no
	// fallback policy decision itself.
	Token SecretString `json:"token,omitempty" yaml:"token,omitempty"`
}

type PairingDiagnostics struct {
	CheckedAt       time.Time        `json:"checked_at,omitempty" yaml:"checked_at,omitempty"`
	RemoteVersion   string           `json:"remote_version,omitempty" yaml:"remote_version,omitempty"`
	RemoteNetworkID string           `json:"remote_network_id,omitempty" yaml:"remote_network_id,omitempty"`
	RemoteProtocols NetworkProtocols `json:"remote_protocols,omitempty" yaml:"remote_protocols,omitempty"`
	Reason          string           `json:"reason,omitempty" yaml:"reason,omitempty"`
	Message         string           `json:"message,omitempty" yaml:"message,omitempty"`
}
