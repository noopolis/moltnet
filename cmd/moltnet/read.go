package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	moltnetclient "github.com/noopolis/moltnet/internal/client"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func runRead(args []string) error {
	flags := flag.NewFlagSet("moltnet read", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		configPath = flags.String("config", "", "explicit Moltnet client config path")
		limit      = flags.Int("limit", 20, "message limit")
		memberID   = flags.String("member", "", "Moltnet member id when a network has multiple attachments")
		networkID  = flags.String("network", "", "Moltnet network id when multiple attachments are configured")
		// Cursor storage and staleness behavior (PLAN.md 7C.2) are documented
		// in read_cursor.go, not here -- this help text is user/agent-facing,
		// and those are internal doc references.
		sinceLast = flags.Bool("since-last", false, "only messages after this identity's last successfully read message for this target")
		targetArg = flags.String("target", "", "target in the form room:<id>, thread:<id>, or dm:<id> (or the first positional argument)")
	)
	override := bindOperatorOverrideFlags(flags)

	// See send.go's runSend for why this checks only the literal word
	// "help" here, before Parse: "-h"/"--help" already reach Go's flag
	// package below and become flag.ErrHelp after printing usage.
	if len(args) > 0 && args[0] == "help" {
		flags.Usage()
		return nil
	}

	if err := flags.Parse(args); err != nil {
		return err
	}

	targetValue := *targetArg
	if targetValue == "" && flags.NArg() > 0 {
		targetValue = flags.Arg(0)
	}
	if flags.NArg() > 1 || (flags.NArg() == 1 && *targetArg != "") {
		return fmt.Errorf("read does not accept positional arguments once --target is given")
	}

	target, err := parseTarget(targetValue)
	if err != nil {
		return err
	}

	attachment, client, usingFallback, err := resolveClientOrOperator(*configPath, *networkID, *memberID, *override, authn.ScopeObserve)
	if err != nil {
		return err
	}
	if !usingFallback {
		// The resolved room lookup is only needed to run the local
		// ensureTargetAllowed check below (ListThreadMessages, further
		// down, takes the thread id directly and needs no room id of its
		// own), so it is skipped entirely in the fallback path, which
		// skips that check too.
		if target.kind == protocol.TargetKindThread {
			resolved, err := resolveThreadTarget(commandContext(), client, target)
			if err != nil {
				return err
			}
			target = resolved
		}
		if err := ensureTargetAllowed(attachment, target); err != nil {
			return err
		}
	}

	pageRequest := protocol.PageRequest{Limit: *limit}

	// PLAN.md 7C.2: --since-last rides the existing PageRequest cursor
	// pagination by resolving this identity's own stored cursor for this
	// target and, if one exists, filtering the fetch to only what comes
	// after it (read_cursor.go's resolveReadCursorIdentity/lookupReadCursor
	// implement the binding storage decision -- one file, two key
	// derivations, never another identity's entry).
	var cursor readCursorIdentity
	if *sinceLast {
		cursor, err = resolveReadCursorIdentity(*configPath, *networkID, attachment, usingFallback, strings.TrimSpace(override.baseURL) != "")
		if err != nil {
			return err
		}
		stored, err := lookupReadCursor(cursor.path, readCursorKey(cursor, target))
		if err != nil {
			return err
		}
		if stored != "" {
			pageRequest.After = stored
		}
	}

	cursorReset := false
	page, err := fetchTargetMessages(commandContext(), client, target, pageRequest)
	if err != nil {
		if *sinceLast && pageRequest.After != "" && isInvalidCursorError(err) {
			// PLAN.md 7C.2's documented "cursor older than retained
			// history" behavior: rather than hard-failing an otherwise
			// healthy identity/target resolution, this self-heals to a
			// fresh baseline -- exactly first-use's unfiltered fetch --
			// and the success path below persists a fresh, valid cursor
			// from it, same as any other successful --since-last read.
			// cursorReset also stamps a machine-readable "cursor_reset"
			// marker on the printed payload below, so an agent -- the
			// audience --since-last exists for -- parsing stdout can tell
			// this gap apart from a clean delta, not just a human reading
			// this stderr note.
			fmt.Fprintf(stderr, "note: stored --since-last cursor for %s:%s is older than retained history; showing the latest messages instead\n", target.kind, target.id)
			cursorReset = true
			pageRequest.After = ""
			page, err = fetchTargetMessages(commandContext(), client, target, pageRequest)
		}
		if err != nil {
			return err
		}
	}

	// The cursor advances on a successful fetch only -- never on error, and
	// never before the fetch above has actually returned -- and only when
	// there is a new "last seen" message to remember; an empty result (no
	// history yet, or genuinely nothing new since last time) leaves any
	// existing cursor untouched rather than clobbering it with nothing.
	// Every message page (ascending order, whether via the default,
	// unfiltered branch or the After-filtered one -- internal/store's
	// pageMessagesDescending/pageInfoForSlice) puts the newest message
	// fetched last, so that is always the correct next cursor regardless of
	// which branch produced it.
	if *sinceLast && len(page.Messages) > 0 {
		newest := page.Messages[len(page.Messages)-1].ID
		if err := advanceReadCursor(cursor.path, readCursorKey(cursor, target), newest); err != nil {
			return err
		}
	}

	if cursorReset {
		return printJSON(readPage{MessagePage: page, CursorReset: true})
	}
	return printJSON(page)
}

// readPage is `read`'s printed stdout shape. It embeds protocol.MessagePage
// unchanged (so a plain `read` or a clean --since-last delta prints exactly
// the same JSON shape as before) and only ever sets CursorReset -- via
// omitempty, so it never appears at all outside the self-heal branch above --
// on the "stored cursor was stale, this is a fresh unfiltered window, not a
// clean delta" case. The stderr note above says the same thing for a human;
// this is the machine-readable form of it for --since-last's stated
// audience, an agent parsing stdout, which cannot see stderr at all in the
// common case of a caller that only captures stdout.
type readPage struct {
	protocol.MessagePage
	CursorReset bool `json:"cursor_reset,omitempty"`
}

// fetchTargetMessages dispatches to the one ListXMessages client call
// target.kind names. Factored out of runRead so the --since-last stale-cursor
// retry (above) can reuse the exact same dispatch instead of duplicating the
// switch.
func fetchTargetMessages(
	ctx context.Context,
	client *moltnetclient.Client,
	target targetRef,
	page protocol.PageRequest,
) (protocol.MessagePage, error) {
	switch target.kind {
	case protocol.TargetKindRoom:
		return client.ListRoomMessages(ctx, target.id, page)
	case protocol.TargetKindThread:
		return client.ListThreadMessages(ctx, target.id, page)
	case protocol.TargetKindDM:
		return client.ListDMMessages(ctx, target.id, page)
	default:
		return protocol.MessagePage{}, fmt.Errorf("unsupported target kind %q", target.kind)
	}
}
