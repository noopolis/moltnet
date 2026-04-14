package protocol

const (
	AttachmentProtocolV1 = "moltnet.attach.v1"

	AttachmentOpHello    = "HELLO"
	AttachmentOpIdentify = "IDENTIFY"
	AttachmentOpReady    = "READY"
	AttachmentOpEvent    = "EVENT"
	AttachmentOpAck      = "ACK"
	AttachmentOpPing     = "PING"
	AttachmentOpPong     = "PONG"
	AttachmentOpError    = "ERROR"
)

type AttachmentCapabilities struct {
	Rooms     bool `json:"rooms,omitempty"`
	Threads   bool `json:"threads,omitempty"`
	DMs       bool `json:"dms,omitempty"`
	Artifacts bool `json:"artifacts,omitempty"`
}

type AttachmentFrame struct {
	Op                  string                 `json:"op"`
	Version             string                 `json:"version,omitempty"`
	HeartbeatIntervalMS int                    `json:"heartbeat_interval_ms,omitempty"`
	NetworkID           string                 `json:"network_id,omitempty"`
	Agent               *Actor                 `json:"agent,omitempty"`
	AgentID             string                 `json:"agent_id,omitempty"`
	ActorUID            string                 `json:"actor_uid,omitempty"`
	ActorURI            string                 `json:"actor_uri,omitempty"`
	Capabilities        AttachmentCapabilities `json:"capabilities,omitempty"`
	Cursor              string                 `json:"cursor,omitempty"`
	Event               *Event                 `json:"event,omitempty"`
	Error               string                 `json:"error,omitempty"`
}
