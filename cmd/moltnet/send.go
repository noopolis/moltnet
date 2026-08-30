package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func runSend(args []string) error {
	flags := flag.NewFlagSet("moltnet send", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		configPath = flags.String("config", "", "explicit Moltnet client config path")
		memberID   = flags.String("member", "", "Moltnet member id when a network has multiple attachments (or the sender identity in the zero-setup operator fallback; default \"operator\")")
		networkID  = flags.String("network", "", "Moltnet network id when multiple attachments are configured")
		targetArg  = flags.String("target", "", "target in the form room:<id>, thread:<id>, or dm:<id> (or the first positional argument)")
		text       = flags.String("text", "", "plain text message content (or the trailing positional argument)")
	)
	override := bindOperatorOverrideFlags(flags)

	// A bare "send help" never reaches Go's flag package as a help request
	// (it is a plain positional argument, not "-h"/"--help", both of which
	// flags.Parse below already turns into flag.ErrHelp after printing
	// usage — see help_test.go's TestFlagHelpNeverErrors). Catch only the
	// literal word here, before Parse, so it prints usage instead of
	// falling through to parseTarget's "target must be room:<id> or
	// dm:<id>" error on "help" treated as the target.
	if len(args) > 0 && args[0] == "help" {
		flags.Usage()
		return nil
	}

	if err := flags.Parse(args); err != nil {
		return err
	}

	targetValue, textValue, err := resolveSendPositionalArgs(flags.Args(), *targetArg, *text)
	if err != nil {
		return err
	}

	target, err := parseTarget(targetValue)
	if err != nil {
		return err
	}
	if strings.TrimSpace(textValue) == "" {
		return fmt.Errorf("send requires --text, or a trailing message argument")
	}

	attachment, client, usingFallback, err := resolveClientOrOperator(*configPath, *networkID, *memberID, *override, authn.ScopeWrite)
	if err != nil {
		return err
	}

	// Unlike read's, this resolution runs unconditionally (fallback or
	// not): protocol.ValidateTarget requires target.room_id on every
	// thread-kind Target, so the wire request built below needs it
	// regardless of whether the local ensureTargetAllowed check that also
	// consumes it gets to run.
	if target.kind == protocol.TargetKindThread {
		resolved, err := resolveThreadTarget(commandContext(), client, target)
		if err != nil {
			return err
		}
		target = resolved
	}

	var fromActor protocol.Actor
	if usingFallback {
		// No agent-config rooms/DMs allowlist exists in this mode (there is
		// no attachment.Rooms to check) — the server's own write-policy and
		// access checks are authoritative instead; see
		// resolveClientOrOperator's doc comment.
		fromActor = operatorSenderActor(*networkID, *memberID)
	} else {
		if err := ensureTargetAllowed(attachment, target); err != nil {
			return err
		}
		fromActor = buildFromActor(attachment)
	}

	request := protocol.SendMessageRequest{
		From:  fromActor,
		Parts: []protocol.Part{{Kind: "text", Text: strings.TrimSpace(textValue)}},
	}

	switch target.kind {
	case protocol.TargetKindRoom:
		request.Target = protocol.Target{Kind: protocol.TargetKindRoom, RoomID: target.id}
	case protocol.TargetKindThread:
		request.Target = protocol.Target{Kind: protocol.TargetKindThread, ThreadID: target.id, RoomID: target.roomID}
	case protocol.TargetKindDM:
		dm, err := client.GetDM(commandContext(), target.id)
		if err != nil {
			return err
		}
		request.Target = protocol.Target{
			Kind:           protocol.TargetKindDM,
			DMID:           dm.ID,
			ParticipantIDs: append([]string(nil), dm.ParticipantIDs...),
		}
	}
	if !usingFallback {
		if deliveryID, ok := daimonWakeDeliveryID(os.Getenv(bridgeutil.DaimonWakeIDEnv)); ok {
			request.ID = bridgeutil.DeliveryReplyMessageID(fromActor.ID, deliveryID, request.Target)
			request.CauseEventIDs = []string{deliveryID}
		}
	}

	accepted, err := client.SendMessage(commandContext(), request)
	if err != nil {
		if usingFallback && strings.Contains(err.Error(), "human ingress is disabled") {
			return fmt.Errorf("%w; run `moltnet connect` to set up an agent identity for `moltnet send` on this network instead", err)
		}
		return err
	}
	return printJSON(accepted)
}

func daimonWakeDeliveryID(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	messageID, ok := strings.CutPrefix(trimmed, protocol.CausalSystemMoltnet+":")
	if !ok || protocol.ValidateMessageID(messageID) != nil || protocol.MessageEventID(messageID) != trimmed {
		return "", false
	}
	return trimmed, true
}

// resolveSendPositionalArgs merges send's positional arguments into the
// --target/--text flag values: `moltnet send room:chat "hola"` and `moltnet
// send --target room:chat "hola"` (target as the first positional argument
// when --target is absent, text as the next/trailing one(s) when --text is
// absent) both resolve to the same target/text pair as the fully-flagged
// form, without breaking it.
//
// When --target is already set and exactly one positional argument remains
// that itself looks like a target (room:<id> or dm:<id> — targetLikePattern),
// that is almost certainly a mistake, not a message: `send --target room:a
// room:b` used to silently post the literal text "room:b" instead of
// erroring, since nothing here previously distinguished "the trailing
// message" from "a second target the caller forgot a flag for". This is
// refused outright, mirroring runRead's existing "does not accept
// positional arguments once --target is given" rejection so the two
// commands agree on the same mistake.
//
// Once target is resolved, any remaining positional words become the
// message when --text was not given — `moltnet send room:chat hola mundo`
// joins "hola mundo" into one message, the natural way an operator types a
// multi-word message unquoted at a terminal. --text was given explicitly,
// any remaining positional is still an error (never silently dropped or
// merged into a flag value the caller already set), naming the actual
// extra value so the fix is obvious from the message alone.
func resolveSendPositionalArgs(positional []string, targetFlag string, textFlag string) (string, string, error) {
	target := targetFlag
	text := textFlag
	remaining := positional

	if target != "" && text == "" && len(remaining) == 1 && targetLikePattern.MatchString(remaining[0]) {
		return "", "", fmt.Errorf(
			"send: %q looks like a target, but --target is already %q; did you mean --text %q?",
			remaining[0], target, remaining[0],
		)
	}

	if target == "" && len(remaining) > 0 {
		target = remaining[0]
		remaining = remaining[1:]
	}

	if text != "" {
		if len(remaining) > 0 {
			return "", "", fmt.Errorf("send does not accept a third positional argument (got %q); use --text to include extra words as one message", remaining[0])
		}
		return target, text, nil
	}

	if len(remaining) > 0 {
		text = strings.Join(remaining, " ")
	}

	return target, text, nil
}

// targetLikePattern matches the room:<id>/thread:<id>/dm:<id> target
// shorthand — resolveSendPositionalArgs' guard for a stray positional that
// looks like a forgotten second target rather than a message.
var targetLikePattern = regexp.MustCompile(`^(room|thread|dm):`)
