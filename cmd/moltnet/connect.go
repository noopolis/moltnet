package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
)

func runConnect(args []string) error {
	flags := flag.NewFlagSet("moltnet connect", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		agentName    = flags.String("agent-name", "", "agent display name")
		authMode     = flags.String("auth-mode", "none", "client auth mode: none, bearer, or open")
		baseURL      = flags.String("base-url", "", "Moltnet base URL")
		enableDMs    = flags.Bool("enable-dms", false, "enable direct-message access in local config")
		installSkill = flags.Bool("install-skill", true, "install the Moltnet skill into the runtime workspace")
		memberID     = flags.String("member-id", "", "Moltnet member id")
		networkID    = flags.String("network-id", "", "Moltnet network id")
		roomList     = flags.String("rooms", "", "comma-separated room ids")
		runtime      = flags.String("runtime", "openclaw", "runtime name")
		token        = flags.String("token", "", "plain bearer token")
		tokenEnv     = flags.String("token-env", "", "environment variable containing the bearer token")
		tokenPath    = flags.String("token-path", "", "file containing the bearer token")
		workspace    = flags.String("workspace", ".", "runtime workspace path")
	)

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("connect does not accept positional arguments")
	}

	path := clientConfigPathForWorkspace(*workspace)
	rollback, err := snapshotFile(path)
	if err != nil {
		return err
	}
	config, err := loadOrCreateClientConfig(path, *runtime, *agentName)
	if err != nil {
		return err
	}

	if strings.TrimSpace(*baseURL) == "" || strings.TrimSpace(*networkID) == "" || strings.TrimSpace(*memberID) == "" {
		return fmt.Errorf("connect requires --base-url, --network-id, and --member-id")
	}

	authModeSet := flagWasSet(flags, "auth-mode")
	sourceExplicit := flagWasSet(flags, "token") || flagWasSet(flags, "token-env") || flagWasSet(flags, "token-path")
	attachment := clientconfig.AttachmentConfig{
		AgentName: *agentName,
		Auth: clientconfig.AuthConfig{
			Mode:      strings.TrimSpace(*authMode),
			Token:     strings.TrimSpace(*token),
			TokenEnv:  strings.TrimSpace(*tokenEnv),
			TokenPath: strings.TrimSpace(*tokenPath),
		},
		BaseURL:   strings.TrimSpace(*baseURL),
		MemberID:  strings.TrimSpace(*memberID),
		NetworkID: strings.TrimSpace(*networkID),
		Rooms:     parseRooms(*roomList),
		Runtime:   strings.TrimSpace(*runtime),
	}
	if *enableDMs {
		attachment.DMs = &bridgeconfig.DMConfig{Enabled: true}
	}
	if existing, err := config.ResolveAttachmentFor(attachment.NetworkID, attachment.MemberID); err == nil {
		attachment.Auth = mergeAuthFromConfig(existing.Auth, attachment.Auth, authModeSet, sourceExplicit)
	}

	upsertAttachment(&config, attachment)
	if config.Agent.Name == "" {
		config.Agent.Name = *agentName
	}
	if config.Agent.Runtime == "" {
		config.Agent.Runtime = *runtime
	}

	if strings.TrimSpace(attachment.Auth.Mode) == bridgeconfig.AuthModeOpen {
		if _, err := attachment.ResolveToken(); err != nil {
			return err
		}
		if err := writeClientConfig(path, config); err != nil {
			return err
		}
		registration, err := registerAttachmentAgent(attachment)
		if err != nil {
			_ = rollback.restore(path)
			return err
		}
		if strings.TrimSpace(registration.AgentToken) != "" {
			attachment.Auth = applyAgentTokenToAuth(attachment.Auth, registration.AgentToken)
			upsertAttachment(&config, attachment)
			if err := writeClientConfig(path, config); err != nil {
				return err
			}
		}
		if err := writeIdentityFile(*workspace, registration); err != nil {
			return err
		}
	} else if err := writeClientConfig(path, config); err != nil {
		return err
	}

	if *installSkill {
		skillPath, err := installMoltnetSkill(*runtime, *workspace, moltnetSkillContent())
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "installed skill %s\n", skillPath)
	}

	fmt.Fprintf(stdout, "wrote Moltnet client config %s\n", path)
	return nil
}

type fileSnapshot struct {
	contents []byte
	exists   bool
}

func snapshotFile(path string) (fileSnapshot, error) {
	contents, err := os.ReadFile(path)
	if err == nil {
		return fileSnapshot{contents: contents, exists: true}, nil
	}
	if os.IsNotExist(err) {
		return fileSnapshot{}, nil
	}
	return fileSnapshot{}, fmt.Errorf("read existing Moltnet client config %q: %w", path, err)
}

func (s fileSnapshot) restore(path string) error {
	if s.exists {
		return os.WriteFile(path, s.contents, 0o600)
	}
	return os.Remove(path)
}
