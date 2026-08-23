package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/noopolis/moltnet/internal/app"
)

// adminRoomMembersConfigWriteback persists an `admin room members add/remove`
// mutation into the local Moltnet server config, so the change survives the
// next restart instead of being silently reconciled away (PLAN.md 7B.5 —
// see internal/app/config_writeback_membership.go's doc comment for the bug
// this closes: startup's applyRequestFromConfig replaces every
// config-declared room's member list wholesale, and membership granted only
// through the API used to be undone by that on the very next restart).
//
// localServerConfigPath must be exactly resolveAdminClient's
// adminClientResolution.LocalServerConfigPath -- empty unless that call
// itself resolved through the local-server-config fallback (no --base-url,
// no client config found on disk at all). F4 (confirmed live,
// two-server reproduction): this function used to re-derive a "local"
// config path on its own from --network plus "were --base-url/--config
// passed", which silently wrote to the wrong server whenever a client
// config (.moltnet/config.json and friends) existed and resolved the live
// request to a different, possibly remote, server. Taking the caller's own
// resolution result instead means this can only ever write to the exact
// file the live request was just guaranteed to have gone to.
//
// This is a best-effort aftercare step, not a second source of truth for
// whether the command itself succeeded: the live API mutation it follows
// has already happened by the time this runs. Both a room that is not
// declared in this config at all (app.ErrRoomConfigNotFound — the room was
// created directly via the API, the console, or `moltnet apply`, never
// through `room create`/`pair invite --room`) and a write that fails to
// reload print a warning and return, rather than failing the command over a
// purely local, purely durability-related follow-up.
func adminRoomMembersConfigWriteback(localServerConfigPath string, roomID string, add []string, remove []string) {
	path := strings.TrimSpace(localServerConfigPath)
	if path == "" {
		// F5 (review round 2): this used to return silently. The mutation
		// above has already landed on the server that request went to --
		// which resolveAdminClientResolution's own doc comment says can be
		// any server reached via --base-url or a resolved client config, not
		// just the local one this CLI can find and edit a config file for --
		// so there is nothing here to write back to. Silence made that read
		// as "this change is durable," when it may not survive that server's
		// next restart at all; say so instead.
		//
		// P1-2 (final-gate review): all three of these aftercare warnings
		// used to print to stdout, ahead of printJSON's own result --
		// exactly the caller this warning exists for
		// ("moltnet admin room members add ... | jq") could not pipe its own
		// stdout without the pipeline's JSON parser choking on this line
		// first. Every warning here now goes to stderr, matching every other
		// best-effort aftercare warning in this package (e.g.
		// room_remove_writeback.go): stdout stays exclusively the command's
		// JSON result.
		fmt.Fprintf(stderr, "  %s membership change for room %q was applied over a resolved connection this CLI has no local config file for\n",
			yellow("warning:"), roomID)
		fmt.Fprintln(stderr, "  whether it survives that server's next restart depends on that server's own config, not on anything this command just wrote")
		return
	}

	switch writeErr := updateRoomMembersWithRollback(path, roomID, add, remove); {
	case writeErr == nil:
		return
	case errors.Is(writeErr, app.ErrRoomConfigNotFound):
		fmt.Fprintf(stderr, "  %s room %q is not declared in %s, so this membership change was not saved there\n",
			yellow("warning:"), roomID, abbreviateHome(path))
		fmt.Fprintln(stderr, "  it applies to the running service only and will not survive the next restart")
	default:
		fmt.Fprintf(stderr, "  %s member change applied to the running service but not saved to %s: %v\n",
			yellow("warning:"), abbreviateHome(path), writeErr)
		fmt.Fprintln(stderr, "  this membership will not survive the next restart until the config is fixed")
	}
}

// updateRoomMembersWithRollback mirrors writePairingWithRollback's (pair.go)
// snapshot/write/reload/restore-on-failure shape for app.UpdateRoomMembers:
// a write that leaves the config unable to reload through the same
// full-startup load path the server uses is rolled back instead of left in
// place.
func updateRoomMembersWithRollback(path string, roomID string, add []string, remove []string) error {
	snapshot, err := snapshotFile(path)
	if err != nil {
		return err
	}

	if err := app.UpdateRoomMembers(path, roomID, add, remove); err != nil {
		return err
	}

	if _, err := app.LoadConfigForPath(path, ""); err != nil {
		if restoreErr := snapshot.restore(path); restoreErr != nil {
			return fmt.Errorf("wrote membership but the config failed to reload (%v); restore also failed: %w", err, restoreErr)
		}
		return fmt.Errorf("wrote membership but the config failed to reload; rolled back: %w", err)
	}

	return nil
}
