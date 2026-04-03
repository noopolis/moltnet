package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
)

func runConnect(args []string) error {
	flags := flag.NewFlagSet("moltnet connect", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		agentName    = flags.String("agent-name", "", "agent display name")
		authMode     = flags.String("auth-mode", "none", "client auth mode: none or bearer")
		baseURL      = flags.String("base-url", "", "Moltnet base URL")
		enableDMs    = flags.Bool("enable-dms", false, "enable direct-message access in local config")
		installSkill = flags.Bool("install-skill", true, "install the Moltnet skill into the runtime workspace")
		memberID     = flags.String("member-id", "", "Moltnet member id")
		networkID    = flags.String("network-id", "", "Moltnet network id")
		roomList     = flags.String("rooms", "", "comma-separated room ids")
		runtime      = flags.String("runtime", "openclaw", "runtime name")
		token        = flags.String("token", "", "plain bearer token")
		tokenEnv     = flags.String("token-env", "", "environment variable containing the bearer token")
		workspace    = flags.String("workspace", ".", "runtime workspace path")
	)

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("connect does not accept positional arguments")
	}

	path := clientConfigPathForWorkspace(*workspace)
	config, err := loadOrCreateClientConfig(path, *runtime, *agentName)
	if err != nil {
		return err
	}

	if strings.TrimSpace(*baseURL) == "" || strings.TrimSpace(*networkID) == "" || strings.TrimSpace(*memberID) == "" {
		return fmt.Errorf("connect requires --base-url, --network-id, and --member-id")
	}

	attachment := clientconfig.AttachmentConfig{
		AgentName: *agentName,
		Auth: clientconfig.AuthConfig{
			Mode:     strings.TrimSpace(*authMode),
			Token:    strings.TrimSpace(*token),
			TokenEnv: strings.TrimSpace(*tokenEnv),
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

	upsertAttachment(&config, attachment)
	if config.Agent.Name == "" {
		config.Agent.Name = *agentName
	}
	if config.Agent.Runtime == "" {
		config.Agent.Runtime = *runtime
	}

	if err := writeClientConfig(path, config); err != nil {
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
