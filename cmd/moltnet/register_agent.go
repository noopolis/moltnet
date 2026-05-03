package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	moltnetclient "github.com/noopolis/moltnet/internal/client"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const identityVersionV1 = "moltnet.identity.v1"

type identityFile struct {
	Version     string `json:"version"`
	NetworkID   string `json:"network_id"`
	AgentID     string `json:"agent_id"`
	ActorUID    string `json:"actor_uid"`
	ActorURI    string `json:"actor_uri"`
	DisplayName string `json:"display_name,omitempty"`
}

func runRegisterAgent(args []string) error {
	flags := flag.NewFlagSet("moltnet register-agent", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		agentID       = flags.String("agent", "", "requested stable agent id")
		authMode      = flags.String("auth-mode", "none", "client auth mode: none, bearer, or open")
		baseURL       = flags.String("base-url", "", "Moltnet base URL")
		configPath    = flags.String("config", "", "existing Moltnet client config path")
		name          = flags.String("name", "", "agent display name")
		networkID     = flags.String("network", "", "Moltnet network id when reading an existing config")
		token         = flags.String("token", "", "plain bearer token")
		tokenEnv      = flags.String("token-env", "", "environment variable containing the bearer token")
		tokenPath     = flags.String("token-path", "", "file containing the bearer token")
		workspace     = flags.String("workspace", ".", "runtime workspace path for .moltnet/identity.json")
		writeIdentity = flags.Bool("write-identity", true, "write .moltnet/identity.json in the workspace")
	)

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("register-agent does not accept positional arguments")
	}

	attachment := clientconfig.AttachmentConfig{
		AgentName: strings.TrimSpace(*name),
		Auth: clientconfig.AuthConfig{
			Mode:      strings.TrimSpace(*authMode),
			Token:     strings.TrimSpace(*token),
			TokenEnv:  strings.TrimSpace(*tokenEnv),
			TokenPath: strings.TrimSpace(*tokenPath),
		},
		BaseURL:   strings.TrimSpace(*baseURL),
		MemberID:  strings.TrimSpace(*agentID),
		NetworkID: strings.TrimSpace(*networkID),
	}

	var loadedConfig *clientconfig.Config
	loadedPath := ""
	if strings.TrimSpace(*configPath) != "" || attachment.BaseURL == "" {
		config, path, err := loadClientConfig(*configPath)
		if err != nil {
			return err
		}
		resolved, err := resolveRegisterConfigAttachment(config, *networkID, *agentID)
		if err != nil {
			return err
		}
		loadedConfig = &config
		loadedPath = path

		if attachment.BaseURL == "" {
			attachment.BaseURL = resolved.BaseURL
		}
		if attachment.MemberID == "" {
			attachment.MemberID = resolved.MemberID
		}
		if attachment.AgentName == "" {
			attachment.AgentName = resolved.AgentName
		}
		authModeSet := flagWasSet(flags, "auth-mode")
		sourceExplicit := flagWasSet(flags, "token") ||
			flagWasSet(flags, "token-env") ||
			flagWasSet(flags, "token-path")
		attachment.Auth = mergeAuthFromConfig(resolved.Auth, attachment.Auth, authModeSet, sourceExplicit)
	}

	if attachment.BaseURL == "" {
		return fmt.Errorf("register-agent requires --base-url or --config")
	}
	if err := validateOpenRegisterWriteback(loadedConfig, loadedPath, attachment); err != nil {
		return err
	}

	registration, err := registerAttachmentAgent(attachment)
	if err != nil {
		return err
	}
	if strings.TrimSpace(registration.AgentToken) != "" {
		if loadedConfig != nil {
			if err := writeAgentTokenToConfig(loadedPath, loadedConfig, attachment, registration.AgentToken); err != nil {
				return err
			}
		} else {
			fmt.Fprintln(os.Stderr, "warning: open agent token was not stored; save agent_token from the JSON output")
		}
	}

	if *writeIdentity {
		if err := writeIdentityFile(*workspace, registration); err != nil {
			return err
		}
	}

	return printJSON(registration)
}

func registerAttachmentAgent(attachment clientconfig.AttachmentConfig) (protocol.AgentRegistration, error) {
	client, err := moltnetclient.New(attachment)
	if err != nil {
		return protocol.AgentRegistration{}, err
	}
	return client.RegisterAgent(commandContext(), protocol.RegisterAgentRequest{
		RequestedAgentID: attachment.MemberID,
		Name:             attachment.AgentName,
	})
}

func resolveRegisterConfigAttachment(
	config clientconfig.Config,
	networkID string,
	agentID string,
) (clientconfig.AttachmentConfig, error) {
	resolved, err := config.ResolveAttachmentFor(networkID, agentID)
	if err == nil {
		return resolved, nil
	}
	if strings.TrimSpace(agentID) == "" {
		return clientconfig.AttachmentConfig{}, err
	}

	fallback, fallbackErr := config.ResolveAttachmentFor(networkID, "")
	if fallbackErr != nil {
		return clientconfig.AttachmentConfig{}, err
	}
	return fallback, nil
}

func validateOpenRegisterWriteback(
	config *clientconfig.Config,
	path string,
	attachment clientconfig.AttachmentConfig,
) error {
	if strings.TrimSpace(attachment.Auth.Mode) != bridgeconfig.AuthModeOpen {
		return nil
	}
	token, err := attachment.ResolveToken()
	if err != nil {
		return err
	}
	if strings.TrimSpace(token) != "" || config == nil {
		return nil
	}
	if findAttachmentIndex(*config, attachment.NetworkID, attachment.MemberID) < 0 {
		return fmt.Errorf("open registration needs a writable config attachment for network %q and member %q", attachment.NetworkID, attachment.MemberID)
	}
	if err := validateClientConfigWriteTarget(path); err != nil {
		return err
	}
	return nil
}

func writeAgentTokenToConfig(
	path string,
	config *clientconfig.Config,
	attachment clientconfig.AttachmentConfig,
	token string,
) error {
	index := findAttachmentIndex(*config, attachment.NetworkID, attachment.MemberID)
	if index < 0 {
		return fmt.Errorf("no writable config attachment for network %q and member %q", attachment.NetworkID, attachment.MemberID)
	}

	config.Attachments[index].Auth = applyAgentTokenToAuth(config.Attachments[index].Auth, token)
	return writeClientConfig(path, *config)
}

func findAttachmentIndex(config clientconfig.Config, networkID string, memberID string) int {
	for index, attachment := range config.Attachments {
		if attachment.NetworkID == strings.TrimSpace(networkID) &&
			attachment.MemberID == strings.TrimSpace(memberID) {
			return index
		}
	}
	return -1
}

func writeIdentityFile(workspace string, registration protocol.AgentRegistration) error {
	root := strings.TrimSpace(workspace)
	if root == "" {
		root = "."
	}
	path := filepath.Join(root, ".moltnet", "identity.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create Moltnet identity directory: %w", err)
	}

	payload, err := json.MarshalIndent(identityFile{
		Version:     identityVersionV1,
		NetworkID:   registration.NetworkID,
		AgentID:     registration.AgentID,
		ActorUID:    registration.ActorUID,
		ActorURI:    registration.ActorURI,
		DisplayName: registration.DisplayName,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Moltnet identity: %w", err)
	}

	if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
		return fmt.Errorf("write Moltnet identity: %w", err)
	}

	return nil
}
