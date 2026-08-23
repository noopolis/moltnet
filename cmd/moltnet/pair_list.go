package main

import (
	"flag"
	"fmt"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// runPairList implements `moltnet pair list` (PLAN.md 7B.4): an operator
// could not see their own peers from the CLI at all before this. It is a
// thin GET /v1/pairings, resolved the same way `admin room members add`
// resolves its client (bindAdminClientResolverFlags/resolveAdminClient,
// remove.go) -- explicit --base-url/--config/--token win outright, and with
// neither given it falls back to deriving both from the local Moltnet
// server config for --network (admin_config_fallback.go).
func runPairList(args []string) error {
	flags := flag.NewFlagSet("moltnet pair list", flag.ContinueOnError)
	flags.SetOutput(stdout)
	options := bindAdminClientResolverFlags(flags, false)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("pair list does not accept positional arguments")
	}

	client, err := resolveAdminClient(flags, options)
	if err != nil {
		return err
	}
	page, err := client.ListPairings(commandContext(), protocol.PageRequest{})
	if err != nil {
		return err
	}
	return printJSON(page)
}
