package main

import (
	"flag"
	"fmt"
)

// runRoomList implements `moltnet room list`. PLAN.md's Phase 7
// "deliberately not built" list is explicit that a third rooms listing
// command must not exist alongside `conversations` and the admin surface —
// this is a thin alias over the same client method (moltnetclient.ListRooms,
// GET /v1/rooms) conversations.go already calls, printed directly rather
// than merged into a DM view: this command's audience is an operator
// managing rooms, not a configured agent listing its own attachments.
func runRoomList(args []string) error {
	flags := flag.NewFlagSet("moltnet room list", flag.ContinueOnError)
	flags.SetOutput(stdout)

	options := bindAdminOnlyFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("room list does not accept positional arguments")
	}

	client, err := resolveAdminClient(flags, options)
	if err != nil {
		return err
	}
	page, err := client.ListRooms(commandContext())
	if err != nil {
		return err
	}
	return printJSON(page)
}
