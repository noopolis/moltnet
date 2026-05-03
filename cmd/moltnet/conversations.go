package main

import (
	"flag"
	"fmt"
	"slices"
	"strings"

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

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("conversations does not accept positional arguments")
	}

	_, attachment, client, err := resolveClientForMember(*configPath, *networkID, *memberID)
	if err != nil {
		return err
	}

	roomPage, err := client.ListRooms(commandContext())
	if err != nil {
		return err
	}
	allowedRooms := make(map[string]struct{}, len(attachment.Rooms))
	for _, room := range attachment.Rooms {
		allowedRooms[room.ID] = struct{}{}
	}

	filteredRooms := make([]protocol.Room, 0, len(roomPage.Rooms))
	for _, room := range roomPage.Rooms {
		if _, ok := allowedRooms[room.ID]; ok {
			filteredRooms = append(filteredRooms, room)
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

	if attachment.DMs != nil && attachment.DMs.Enabled {
		dmPage, err := client.ListDMs(commandContext())
		if err != nil {
			return err
		}
		view.DMs = dmPage.DMs
	}

	return printJSON(view)
}
