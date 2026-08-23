package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	moltnetclient "github.com/noopolis/moltnet/internal/client"
	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// runRoomJoin implements `moltnet room join <id>` (PLAN.md 7A.3): it lets a
// local agent self-serve into a room created with --credential, instead of
// requiring the operator's `admin room members add`. It requires a
// configured room credential, so it applies to rooms created with
// --credential, not to a credential-less room (POST /join is always
// forbidden without one — internal/rooms/join.go).
//
// Unlike send/read/conversations, this deliberately does not fall through
// to resolveClientOrOperator's zero-setup operator fallback: that fallback
// (selectLeastPrivilegedToken, client_operator_fallback.go) never selects an
// agent-restricted token, and POST /v1/rooms/{id}/join always requires
// exactly that (join.go's joinActorAuthorized) — so a fallback attempt could
// only ever 403 server-side, with a far less actionable error than refusing
// here. resolveRoomJoinAttachment below resolves either a real client config
// (the default — written by `moltnet register-agent`/`moltnet connect`) or
// an explicit --base-url paired with --member naming the agent id to join
// as.
func runRoomJoin(args []string) error {
	flagArgs, roomIDArg, err := splitSingleRoomIDArgs(args, roomJoinValueFlags, "room join")
	if err != nil {
		return err
	}

	flags := flag.NewFlagSet("moltnet room join", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		configPath    = flags.String("config", "", "explicit Moltnet client config path")
		memberID      = flags.String("member", "", "Moltnet member id (agent id) to join as")
		networkID     = flags.String("network", "", "Moltnet network id when multiple attachments are configured")
		credentialEnv = flags.String("credential-env", "", "environment variable holding the room's join credential")
	)
	override := bindOperatorOverrideFlags(flags)

	if err := flags.Parse(flagArgs); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("room join does not accept extra positional arguments")
	}
	roomID := strings.TrimSpace(roomIDArg)
	if roomID == "" {
		return fmt.Errorf("room join requires exactly one room id")
	}

	env := strings.TrimSpace(*credentialEnv)
	if env == "" {
		return fmt.Errorf("room join requires --credential-env naming the environment variable holding the room's credential")
	}
	credential := strings.TrimSpace(os.Getenv(env))
	if credential == "" {
		return fmt.Errorf("environment variable %q named by --credential-env is empty or not set", env)
	}

	attachment, client, err := resolveRoomJoinAttachment(*configPath, *networkID, *memberID, *override)
	if err != nil {
		return err
	}

	room, err := client.JoinRoom(commandContext(), roomID, protocol.JoinRoomRequest{
		From:       buildFromActor(attachment),
		Credential: protocol.NewSecretString(credential),
	})
	if err != nil {
		return err
	}
	return printJSON(room)
}

// resolveRoomJoinAttachment resolves the agent identity `room join` presents
// itself as: an explicit --base-url requires --member alongside it (naming
// the agent id join's From actor declares — buildFromActor reads
// attachment.MemberID), since there is no client config to read one from in
// that path. Otherwise it reads the real client config
// (resolveClientForMember, `.moltnet/config.json`, the same file
// `moltnet register-agent`/`moltnet connect` write), which always carries a
// genuine agent identity and bearer token — see runRoomJoin's doc comment
// for why the zero-setup operator fallback is not offered here at all.
func resolveRoomJoinAttachment(
	configPath string,
	networkID string,
	memberID string,
	override operatorOverrideOptions,
) (clientconfig.AttachmentConfig, *moltnetclient.Client, error) {
	if strings.TrimSpace(override.baseURL) != "" {
		attachment := override.attachment()
		attachment.MemberID = strings.TrimSpace(memberID)
		attachment.NetworkID = strings.TrimSpace(networkID)
		if attachment.MemberID == "" {
			return clientconfig.AttachmentConfig{}, nil, fmt.Errorf("room join --base-url requires --member naming the agent id to join as")
		}
		client, err := moltnetclient.New(attachment)
		if err != nil {
			return clientconfig.AttachmentConfig{}, nil, err
		}
		return attachment, client, nil
	}

	_, attachment, client, err := resolveClientForMember(configPath, networkID, memberID)
	if err != nil {
		if errors.Is(err, errClientConfigNotFound) {
			return clientconfig.AttachmentConfig{}, nil, fmt.Errorf(
				"room join requires a configured agent identity (%w); run `moltnet register-agent` or `moltnet connect` first, or pass --base-url with --member",
				err,
			)
		}
		return clientconfig.AttachmentConfig{}, nil, err
	}
	return attachment, client, nil
}
