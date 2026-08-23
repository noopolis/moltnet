package main

import (
	"fmt"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// printSetupPairingSummary prints Q7's own completion-screen addendum, once
// the rest of printSetupSummary (setup_execute.go) has already printed the
// network/room/URL lines. inviteCode is "" for every branch except a
// successful "open it up" run — executeSetupPlan passes back whatever
// `pair invite show` retrieved (setup_pair.go's buildPairInviteShowInvocation),
// which is empty on Q7 = "not now" or "I have a code", and would also be
// empty on --print-commands (which never executes anything to retrieve it
// from).
func printSetupPairingSummary(answers setupAnswers, inviteCode string) {
	switch {
	case answers.connect == setupConnectOpenUp && answers.pairing.openUp != nil:
		printSetupOpenUpSummary(answers.pairing.openUp, answers.id, inviteCode)
	case answers.connect == setupConnectHaveCode && answers.pairing.haveCode != nil:
		printSetupHaveCodeSummary(answers.pairing.haveCode, answers.id)
	}
}

// printSetupOpenUpSummary prints the retrieved invite as one copyable
// command (mirroring `pair invite`'s own aftercare shape, pair_aftercare.go)
// plus the membership command a peer needs once they've paired — with the
// friend's agent id still a placeholder, per SETUP.md point 6: `pair show`
// is a live remote fetch that errors on a peer that has not answered the
// invite yet, so the wizard must not attempt it here. A missing inviteCode
// (retrieval failed, or this is a --print-commands preview) prints nothing
// rather than a broken or empty command line.
func printSetupOpenUpSummary(plan *setupPairOpenUpPlan, networkID, inviteCode string) {
	if plan == nil {
		return
	}
	if inviteCode == "" {
		// An empty-but-successful `pair invite show` (no error, just "") --
		// hard to reach today, the child always either prints the code or
		// errors, but silently rendering the rest of the completion screen
		// with the invite just absent would leave the operator with no clue
		// their friend's invite was never actually shown.
		fmt.Fprintf(stdout, "  %s the pairing was created, but the invite code could not be retrieved; run `moltnet pair invite show %s`\n", yellow("warning:"), plan.pairingID)
		return
	}

	fmt.Fprintln(stdout)
	printInitConfigCheckLine("pairing ready", "")

	days := int(protocol.DefaultInviteTTL.Hours() / 24)
	fmt.Fprintf(stdout, "  share this with your friend — expires in %d days\n\n", days)
	fmt.Fprintf(stdout, "    %s\n", bold(fmt.Sprintf("moltnet pair '%s'", inviteCode)))

	printNextStep(nextStep{
		command:     pairMembershipCommand(plan.room, "<their-network-id>:<their-agent-id>", networkID),
		description: "grant access once they've paired",
	})
}

// printSetupHaveCodeSummary prints the "paired with <network>" checkmark and
// one membership command per shared room the invite declared — the friend's
// agent id is still `<their-agent-id>` (the invite never carries it), the
// same placeholder vocabulary printPairJoinAftercare uses for the plain
// `pair '<code>'` command (pair_aftercare.go).
func printSetupHaveCodeSummary(plan *setupPairHaveCodePlan, networkID string) {
	if plan == nil {
		return
	}

	fmt.Fprintln(stdout)
	printInitConfigCheckLine(fmt.Sprintf("paired with %s", plan.remoteNetworkID), "")

	if len(plan.sharedRooms) == 0 {
		return
	}
	steps := make([]nextStep, len(plan.sharedRooms))
	for i, room := range plan.sharedRooms {
		steps[i] = nextStep{
			command:     pairMembershipCommand(room, plan.remoteNetworkID+":<their-agent-id>", networkID),
			description: "grant their agent access",
		}
	}
	printNextStepList(steps)
}
