package app

import (
	"fmt"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func LoadApplyFile(path string) (protocol.ApplyConfigRequest, string, error) {
	resolvedPath, ok, err := DiscoverPath(path)
	if err != nil {
		return protocol.ApplyConfigRequest{}, "", err
	}
	if !ok {
		return protocol.ApplyConfigRequest{}, "", fmt.Errorf("moltnet config not found")
	}

	config, err := loadApplyFileConfig(resolvedPath)
	if err != nil {
		return protocol.ApplyConfigRequest{}, "", err
	}
	request, err := applyRequestFromRawConfig(config)
	if err != nil {
		return protocol.ApplyConfigRequest{}, "", fmt.Errorf("build apply request from %q: %w", resolvedPath, err)
	}
	return request, resolvedPath, nil
}

func applyRequestFromConfig(config Config) (protocol.ApplyConfigRequest, error) {
	agents := make([]protocol.ApplyAgentRequest, 0)
	agentCredentials := make(map[string]string)
	for _, token := range config.Auth.Tokens {
		credentialKey := authn.TokenConfigCredentialKey(token)
		for _, agentID := range token.Agents {
			if err := appendApplyAgent(&agents, agentCredentials, agentID, credentialKey); err != nil {
				return protocol.ApplyConfigRequest{}, err
			}
		}
	}

	return protocol.ApplyConfigRequest{
		NetworkID: config.NetworkID,
		Rooms:     createRoomRequests(config.Rooms),
		Agents:    agents,
	}, nil
}

func loadApplyFileConfig(path string) (rawConfigFile, error) {
	contents, err := readConfigFile(path)
	if err != nil {
		return rawConfigFile{}, err
	}

	var config rawConfigFile
	if err := decodeConfigBytes(path, contents, &config); err != nil {
		return rawConfigFile{}, err
	}
	if err := validateApplyConfigFile(config); err != nil {
		return rawConfigFile{}, fmt.Errorf("validate Moltnet config %q: %w", path, err)
	}
	if hasPlaintextPairingTokens(config.Pairings) || hasPlaintextAuthTokens(config.Auth) || hasSensitivePostgresConfig(config.Storage) {
		if err := validatePrivateConfigMode(path); err != nil {
			return rawConfigFile{}, err
		}
	}

	return config, nil
}

func validateApplyConfigFile(config rawConfigFile) error {
	version := strings.TrimSpace(config.Version)
	if version != "" && version != defaultConfigSchema {
		return fmt.Errorf("unsupported version %q", version)
	}
	if err := validateRooms(config.Rooms); err != nil {
		return err
	}
	return validateApplyAuth(config.Auth)
}

func validateApplyAuth(config rawAuthConfig) error {
	switch strings.TrimSpace(config.Mode) {
	case "", authn.ModeNone, authn.ModeBearer, authn.ModeOpen:
	default:
		return fmt.Errorf("unsupported auth mode %q", config.Mode)
	}
	switch strings.TrimSpace(config.AgentRegistration) {
	case "", authn.AgentRegistrationDisabled, authn.AgentRegistrationToken, authn.AgentRegistrationOpen:
	default:
		return fmt.Errorf("unsupported auth.agent_registration %q", config.AgentRegistration)
	}
	for tokenIndex, token := range config.Tokens {
		for agentIndex, agentID := range token.Agents {
			if err := protocol.ValidateMemberID(strings.TrimSpace(agentID)); err != nil {
				return fmt.Errorf("auth.tokens[%d].agents[%d] %w", tokenIndex, agentIndex, err)
			}
		}
		if len(token.Agents) > 0 && strings.TrimSpace(token.ID) == "" && strings.TrimSpace(token.Value) == "" {
			return fmt.Errorf("auth.tokens[%d].id is required when agents are declared without a token value", tokenIndex)
		}
	}
	return nil
}

func applyRequestFromRawConfig(config rawConfigFile) (protocol.ApplyConfigRequest, error) {
	base := mergeFileConfig(defaultConfig(""), config)
	request := protocol.ApplyConfigRequest{
		NetworkID: base.NetworkID,
		Rooms:     createRoomRequests(base.Rooms),
	}

	agentCredentials := make(map[string]string)
	for _, token := range config.Auth.Tokens {
		tokenConfig := authn.TokenConfig{
			ID:    token.ID,
			Value: token.Value,
		}
		credentialKey := authn.TokenConfigCredentialKey(tokenConfig)
		for _, agentID := range token.Agents {
			if err := appendApplyAgent(&request.Agents, agentCredentials, agentID, credentialKey); err != nil {
				return protocol.ApplyConfigRequest{}, err
			}
		}
	}
	return request, nil
}

func appendApplyAgent(
	agents *[]protocol.ApplyAgentRequest,
	agentCredentials map[string]string,
	agentID string,
	credentialKey string,
) error {
	id := strings.TrimSpace(agentID)
	key := strings.TrimSpace(credentialKey)
	if id == "" {
		return nil
	}
	if existing, ok := agentCredentials[id]; ok {
		if existing != key {
			return fmt.Errorf("agent %q is declared by multiple auth tokens", id)
		}
		return nil
	}
	agentCredentials[id] = key
	*agents = append(*agents, protocol.ApplyAgentRequest{
		ID:            id,
		CredentialKey: key,
	})
	return nil
}

func createRoomRequests(rooms []RoomConfig) []protocol.CreateRoomRequest {
	requests := make([]protocol.CreateRoomRequest, 0, len(rooms))
	for _, room := range rooms {
		requests = append(requests, protocol.CreateRoomRequest{
			ID:          room.ID,
			Name:        room.Name,
			Members:     append([]string(nil), room.Members...),
			Visibility:  room.Visibility,
			WritePolicy: room.WritePolicy,
		})
	}
	return requests
}
