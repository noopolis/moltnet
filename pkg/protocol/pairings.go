package protocol

import "time"

const (
	PairingStatusConnected    = "connected"
	PairingStatusDegraded     = "degraded"
	PairingStatusIncompatible = "incompatible"
	PairingStatusError        = "error"
	PairingStatusUnknown      = "unknown"
)

type PairingDiagnostics struct {
	CheckedAt       time.Time        `json:"checked_at,omitempty" yaml:"checked_at,omitempty"`
	RemoteVersion   string           `json:"remote_version,omitempty" yaml:"remote_version,omitempty"`
	RemoteNetworkID string           `json:"remote_network_id,omitempty" yaml:"remote_network_id,omitempty"`
	RemoteProtocols NetworkProtocols `json:"remote_protocols,omitempty" yaml:"remote_protocols,omitempty"`
	Reason          string           `json:"reason,omitempty" yaml:"reason,omitempty"`
	Message         string           `json:"message,omitempty" yaml:"message,omitempty"`
}
