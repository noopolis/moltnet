package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

const dmTopologyControlMarker = "moltnet.dm-topology.v1"

func runAdminDMCommand(args []string) error {
	if len(args) == 0 || args[0] == "help" {
		fmt.Fprint(stdout, buildAdminUsage())
		return nil
	}
	if args[0] != "ensure" {
		return fmt.Errorf("unknown admin dm command %q\n\n%s", args[0], buildAdminUsage())
	}
	return runAdminEnsureDM(args[1:])
}

func runAdminEnsureDM(args []string) error {
	flags := flag.NewFlagSet("moltnet admin dm ensure", flag.ContinueOnError)
	flags.SetOutput(stdout)
	options := bindAdminClientResolverFlags(flags, false)
	sender := flags.String("sender", "", "participant id used to seed the direct conversation")
	var members repeatedStringFlag
	flags.Var(&members, "member", "direct-message member id; exactly two required")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("admin dm ensure does not accept positional arguments")
	}

	participantIDs, err := canonicalDMTopologyMembers(members)
	if err != nil {
		return err
	}
	senderID := strings.TrimSpace(*sender)
	if err := protocol.ValidateMemberID(senderID); err != nil {
		return fmt.Errorf("admin dm ensure requires a valid --sender: %w", err)
	}
	if senderID != participantIDs[0] && senderID != participantIDs[1] {
		return fmt.Errorf("admin dm ensure sender must be one of the two members")
	}

	digest := sha256.Sum256([]byte(strings.Join(participantIDs, "\x00")))
	suffix := hex.EncodeToString(digest[:16])
	client, err := resolveAdminClient(flags, options)
	if err != nil {
		return err
	}
	result, err := client.SendMessage(commandContext(), protocol.SendMessageRequest{
		ID: "dm_topology_" + suffix,
		Target: protocol.Target{
			Kind:           protocol.TargetKindDM,
			DMID:           "dm_" + suffix,
			ParticipantIDs: participantIDs,
		},
		From: protocol.Actor{Type: "agent", ID: senderID},
		Parts: []protocol.Part{{
			Kind: protocol.PartKindData,
			Data: map[string]any{"control_marker": dmTopologyControlMarker},
		}},
	})
	if err != nil {
		return err
	}
	if !result.Accepted {
		return fmt.Errorf("admin dm ensure was not accepted")
	}
	return printJSON(result)
}

func canonicalDMTopologyMembers(raw []string) ([]string, error) {
	if len(raw) != 2 {
		return nil, fmt.Errorf("admin dm ensure requires exactly two --member values")
	}
	members := []string{strings.TrimSpace(raw[0]), strings.TrimSpace(raw[1])}
	for _, member := range members {
		if err := protocol.ValidateMemberID(member); err != nil {
			return nil, fmt.Errorf("admin dm ensure member %q is invalid: %w", member, err)
		}
	}
	sort.Strings(members)
	if members[0] == members[1] {
		return nil, fmt.Errorf("admin dm ensure members must be distinct")
	}
	return members, nil
}
