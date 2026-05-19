package protocol

type ApplyConfigRequest struct {
	NetworkID string              `json:"network_id,omitempty"`
	Rooms     []CreateRoomRequest `json:"rooms,omitempty"`
	Agents    []ApplyAgentRequest `json:"agents,omitempty"`
}

type ApplyAgentRequest struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	CredentialKey string `json:"credential_key,omitempty"`
}

type ApplyConfigResult struct {
	Applied bool                `json:"applied"`
	Rooms   []Room              `json:"rooms,omitempty"`
	Agents  []AgentRegistration `json:"agents,omitempty"`
}
