package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/noopolis/moltnet/internal/app"
)

// roomRemoveConfigWriteback persists a `room remove`/`admin room remove`
// deletion into the local Moltnet server config's rooms[] list, mirroring
// adminRoomMembersConfigWriteback's (admin_room_members_writeback.go)
// aftercare shape: the live DELETE has already succeeded by the time this
// runs, so a failure here is a warning, never a reason to fail the command.
//
// This closes P0-1 (final-gate review): store.RemoveRoomContext is a soft
// delete that tombstones a room's primary key without freeing it. Leaving
// the room's rooms[] entry in config meant the next restart's
// applyRequestFromConfig replayed it straight into that tombstoned id --
// CreateRoomContext's INSERT collides with it, the fallback
// ReconcileRoomContext never finds a live row to reconcile, and the whole
// network fails to start. Removing the config entry here, right alongside
// the live deletion, means a restart never attempts to recreate a room
// that no longer exists.
//
// localServerConfigPath must be exactly resolveAdminClientResolution's
// adminClientResolution.LocalServerConfigPath -- empty unless that call
// itself resolved through the local-server-config fallback (no --base-url,
// no client config found on disk at all). Reusing that value rather than
// re-deriving one (F4, admin_client_resolve.go's own doc comment) means
// this can only ever write to the exact file the live DELETE was just
// guaranteed to have gone to.
func roomRemoveConfigWriteback(localServerConfigPath string, roomID string) {
	path := strings.TrimSpace(localServerConfigPath)
	if path == "" {
		fmt.Fprintf(stderr, "  %s room %q was removed from the running service, but this command has no local config file to remove it from\n",
			yellow("warning:"), roomID)
		fmt.Fprintln(stderr, "  if a config file elsewhere still declares this room, its next restart may fail to start the network until that rooms[] entry is removed by hand")
		return
	}

	switch writeErr := removeRoomFromConfigWithRollback(path, roomID); {
	case writeErr == nil:
		return
	case errors.Is(writeErr, app.ErrRoomConfigNotFound):
		// Room was removed live but was never declared in this config's
		// rooms[] (created directly via the API, the console, or
		// `moltnet apply`) -- nothing to clean up, and no restart hazard,
		// since applyRequestFromConfig only replays rooms this file
		// actually lists.
		return
	default:
		fmt.Fprintf(stderr, "  %s room %q was removed from the running service but the local config %s could not be updated: %v\n",
			yellow("warning:"), roomID, abbreviateHome(path), writeErr)
		fmt.Fprintln(stderr, "  its next restart may fail to start the network until the stale rooms[] entry is removed from that file by hand")
	}
}

// removeRoomFromConfigWithRollback mirrors updateRoomMembersWithRollback's
// (admin_room_members_writeback.go) snapshot/write/reload/restore-on-failure
// shape for app.RemoveRoomFromConfig: a write that leaves the config unable
// to reload through the same full-startup load path the server uses is
// rolled back instead of left in place.
func removeRoomFromConfigWithRollback(path string, roomID string) error {
	snapshot, err := snapshotFile(path)
	if err != nil {
		return err
	}

	if err := app.RemoveRoomFromConfig(path, roomID); err != nil {
		return err
	}

	if _, err := app.LoadConfigForPath(path, ""); err != nil {
		if restoreErr := snapshot.restore(path); restoreErr != nil {
			return fmt.Errorf("removed room but the config failed to reload (%v); restore also failed: %w", err, restoreErr)
		}
		return fmt.Errorf("removed room but the config failed to reload; rolled back: %w", err)
	}

	return nil
}
