package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	moltnetclient "github.com/noopolis/moltnet/internal/client"
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
		authMode      = flags.String("auth-mode", "none", "client auth mode: none or bearer")
		baseURL       = flags.String("base-url", "", "Moltnet base URL")
		configPath    = flags.String("config", "", "existing Moltnet client config path")
		name          = flags.String("name", "", "agent display name")
		networkID     = flags.String("network", "", "Moltnet network id when reading an existing config")
		token         = flags.String("token", "", "plain bearer token")
		tokenEnv      = flags.String("token-env", "", "environment variable containing the bearer token")
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
			Mode:     strings.TrimSpace(*authMode),
			Token:    strings.TrimSpace(*token),
			TokenEnv: strings.TrimSpace(*tokenEnv),
		},
		BaseURL:   strings.TrimSpace(*baseURL),
		MemberID:  strings.TrimSpace(*agentID),
		NetworkID: strings.TrimSpace(*networkID),
	}

	if strings.TrimSpace(*configPath) != "" || attachment.BaseURL == "" {
		_, resolved, _, err := resolveClient(*configPath, *networkID)
		if err != nil {
			return err
		}
		if attachment.BaseURL == "" {
			attachment.BaseURL = resolved.BaseURL
		}
		if attachment.MemberID == "" {
			attachment.MemberID = resolved.MemberID
		}
		if attachment.AgentName == "" {
			attachment.AgentName = resolved.AgentName
		}
		if strings.TrimSpace(attachment.Auth.Mode) == "" || attachment.Auth.Mode == "none" {
			attachment.Auth = resolved.Auth
		}
	}

	if attachment.BaseURL == "" {
		return fmt.Errorf("register-agent requires --base-url or --config")
	}

	client, err := moltnetclient.New(attachment)
	if err != nil {
		return err
	}
	registration, err := client.RegisterAgent(commandContext(), protocol.RegisterAgentRequest{
		RequestedAgentID: attachment.MemberID,
		Name:             attachment.AgentName,
	})
	if err != nil {
		return err
	}

	if *writeIdentity {
		if err := writeIdentityFile(*workspace, registration); err != nil {
			return err
		}
	}

	return printJSON(registration)
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
