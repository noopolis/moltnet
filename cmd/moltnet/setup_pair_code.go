package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// errSetupPairNetworkIDCollision is Q7 = "I have an invite code"'s own
// pre-mutation refusal (P2-1): `pair` itself refuses an invite whose
// NetworkID collides with the local network id (pair_join.go), but only
// after `init` has already written the config to disk. Checking the same
// collision here — before planSetupPairHaveCode ever returns, which is
// before runSetupInteractive builds or executes any invocation at all — lets
// this land pre-mutation like every other Q7 refusal.
var errSetupPairNetworkIDCollision = errors.New("setup: invite network id collides with the local network id")

// setupPairHaveCodePlan is Q7 = "I have an invite code"'s fully resolved
// plan. remoteNetworkID/remoteNetworkName/sharedRooms are decoded locally
// from the code itself (protocol.DecodeInvite), not learned from the `pair`
// child's own suppressed stdout, so the wizard's completion screen
// (setup_pair_summary.go) can name the real peer and membership commands
// without needing anything back from that step.
type setupPairHaveCodePlan struct {
	code              string
	remoteNetworkID   string
	remoteNetworkName string
	sharedRooms       []string
}

// planSetupPairHaveCode implements Q7 = "I have an invite code": prompts
// for the code (a plain, echoed paste — SETUP.md: "not a secret in the
// credential sense, but it is long"), then decodes and validates it locally
// via protocol.DecodeInvite before ever building a child invocation, so a
// malformed or expired code produces protocol's own clear message
// ("invite expired at ...", "invite code must start with ...") instead of
// whatever raw error `pair '<code>'` would otherwise surface from deeper in
// the config-write path. id is Q2's own answer — the local network id this
// invite would be paired into — checked against the invite's own NetworkID
// (P2-1, errSetupPairNetworkIDCollision above) before this ever returns.
func planSetupPairHaveCode(ctx context.Context, id string) (setupPairHaveCodePlan, error) {
	code, err := setupPromptLineCtx(ctx, "  paste your invite code: ")
	if err != nil && !errors.Is(err, io.EOF) {
		return setupPairHaveCodePlan{}, err
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return setupPairHaveCodePlan{}, errors.New(`"I have an invite code" needs one — paste the code your friend sent you`)
	}

	invite, err := protocol.DecodeInvite(code)
	if err != nil {
		return setupPairHaveCodePlan{}, fmt.Errorf("that invite code looks expired or malformed: %w", err)
	}
	if invite.NetworkID == id {
		return setupPairHaveCodePlan{}, fmt.Errorf(
			"%w: the invite's network id %q is the same as the name you gave at \"Name it\"; ask the sender to regenerate the invite with a distinct network.id, or rerun and give this network a distinct name",
			errSetupPairNetworkIDCollision, invite.NetworkID,
		)
	}

	printInitConfigCheckLine(fmt.Sprintf("invite from %s", invite.NetworkID), "")
	return setupPairHaveCodePlan{
		code:              code,
		remoteNetworkID:   invite.NetworkID,
		remoteNetworkName: invite.NetworkName,
		sharedRooms:       protocol.UniqueTrimmedStrings(invite.SharedRooms),
	}, nil
}
