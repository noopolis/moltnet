package main

import (
	"flag"
	"fmt"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func runParticipants(args []string) error {
	flags := flag.NewFlagSet("moltnet participants", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		configPath = flags.String("config", "", "explicit Moltnet client config path")
		memberID   = flags.String("member", "", "Moltnet member id when a network has multiple attachments")
		networkID  = flags.String("network", "", "Moltnet network id when multiple attachments are configured")
		targetArg  = flags.String("target", "", "explicit target in the form room:<id>, thread:<id>, or dm:<id>")
	)
	override := bindOperatorOverrideFlags(flags)

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("participants does not accept positional arguments")
	}

	target, err := parseTarget(*targetArg)
	if err != nil {
		return err
	}

	attachment, client, usingFallback, err := resolveClientOrOperator(*configPath, *networkID, *memberID, *override, authn.ScopeObserve)
	if err != nil {
		return err
	}

	// Resolved unconditionally (fallback or not): the room fetch below
	// needs target.roomID either way, unlike read's equivalent guard which
	// only needs it for the local check.
	if target.kind == protocol.TargetKindThread {
		resolved, err := resolveThreadTarget(commandContext(), client, target)
		if err != nil {
			return err
		}
		target = resolved
	}
	if !usingFallback {
		if err := ensureTargetAllowed(attachment, target); err != nil {
			return err
		}
	}

	switch target.kind {
	case protocol.TargetKindRoom:
		room, err := client.GetRoom(commandContext(), target.id)
		if err != nil {
			return err
		}
		return printJSON(room)
	case protocol.TargetKindThread:
		// PLAN 7C.3's deliberate choice: threads carry no membership or
		// write-policy of their own (see resolveThreadTarget's doc
		// comment), and protocol.Thread itself has no participant list at
		// all to report (pkg/protocol/threads.go). "Who can I talk to in
		// this thread" is exactly "who is in the room the thread lives
		// in", so this prints the resolved parent room rather than
		// refusing the target or fabricating a thread-shaped participants
		// view that would just duplicate the room's.
		room, err := client.GetRoom(commandContext(), target.roomID)
		if err != nil {
			return err
		}
		return printJSON(room)
	case protocol.TargetKindDM:
		dm, err := client.GetDM(commandContext(), target.id)
		if err != nil {
			return err
		}
		return printJSON(dm)
	default:
		return fmt.Errorf("unsupported target kind %q", target.kind)
	}
}
