package main

import (
	"flag"
	"fmt"
	"slices"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func runConversations(args []string) error {
	flags := flag.NewFlagSet("moltnet conversations", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		configPath = flags.String("config", "", "explicit Moltnet client config path")
		memberID   = flags.String("member", "", "Moltnet member id when a network has multiple attachments")
		networkID  = flags.String("network", "", "Moltnet network id when multiple attachments are configured")
	)
	override := bindOperatorOverrideFlags(flags)

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("conversations does not accept positional arguments")
	}

	attachment, client, usingFallback, err := resolveClientOrOperator(*configPath, *networkID, *memberID, *override, authn.ScopeObserve)
	if err != nil {
		return err
	}

	roomPage, err := client.ListRooms(commandContext())
	if err != nil {
		return err
	}

	// In the zero-setup operator fallback there is no agent-side
	// attachment.Rooms allowlist to filter against — the token's own
	// server-side visibility is already the whole answer, so every room
	// ListRooms returned is shown, not silently filtered down to an empty
	// set (an empty allowedRooms map here would otherwise hide everything).
	var filteredRooms []protocol.Room
	if usingFallback {
		filteredRooms = append([]protocol.Room(nil), roomPage.Rooms...)
	} else {
		allowedRooms := make(map[string]struct{}, len(attachment.Rooms))
		for _, room := range attachment.Rooms {
			allowedRooms[room.ID] = struct{}{}
		}
		filteredRooms = make([]protocol.Room, 0, len(roomPage.Rooms))
		for _, room := range roomPage.Rooms {
			if _, ok := allowedRooms[room.ID]; ok {
				filteredRooms = append(filteredRooms, room)
			}
		}
	}
	slices.SortFunc(filteredRooms, func(left, right protocol.Room) int {
		return strings.Compare(left.ID, right.ID)
	})

	view := conversationsView{
		MemberID:  attachment.MemberID,
		NetworkID: attachment.NetworkID,
		Rooms:     filteredRooms,
	}

	if usingFallback || (attachment.DMs != nil && attachment.DMs.Enabled) {
		dmPage, err := client.ListDMs(commandContext())
		if err != nil {
			return err
		}
		view.DMs = dmPage.DMs
	}

	return printJSON(view)
}
