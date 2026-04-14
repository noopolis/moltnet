package main

import (
	"context"
	"errors"
	"fmt"
)

func runCLI(args []string, buildVersion string, factory signalContextFactory) error {
	ctx, stop := factory()
	defer stop()

	return run(ctx, args, buildVersion)
}

func run(ctx context.Context, args []string, buildVersion string) error {
	command, rest := parseCommand(args)

	switch command {
	case "", "start", "server":
		return runServer(ctx, buildVersion)
	case "bridge":
		return runBridgeCommand(ctx, rest)
	case "connect":
		return runConnect(rest)
	case "conversations":
		return runConversations(rest)
	case "init":
		return runInit(rest)
	case "participants":
		return runParticipants(rest)
	case "read":
		return runRead(rest)
	case "register-agent":
		return runRegisterAgent(rest)
	case "send":
		return runSend(rest)
	case "skill":
		return runSkillCommand(rest)
	case "validate":
		return runValidate(rest)
	case "node":
		return runNodeCommand(ctx, rest)
	case "attachment":
		return runAttachmentCommand(ctx, rest)
	case "version":
		fmt.Fprintln(stdout, buildVersion)
		return nil
	case "help":
		fmt.Fprint(stdout, buildUsage())
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\n%s", command, buildUsage())
	}
}

func parseCommand(args []string) (string, []string) {
	if len(args) == 0 {
		return "", nil
	}

	return args[0], args[1:]
}

func runNodeCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return runNode(ctx, nil)
	}
	if args[0] == "start" {
		return runNode(ctx, args[1:])
	}
	if args[0] == "help" {
		fmt.Fprint(stdout, buildNodeUsage())
		return nil
	}

	return runNode(ctx, args)
}

func runAttachmentCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("attachment runner config path required")
	}
	if args[0] == "help" {
		fmt.Fprint(stdout, buildAttachmentUsage())
		return nil
	}
	if args[0] == "run" {
		return runAttachment(ctx, args[1:])
	}

	return runAttachment(ctx, args)
}

func runBridgeCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("bridge runner config path required")
	}
	if args[0] == "help" {
		fmt.Fprint(stdout, buildBridgeUsage())
		return nil
	}
	if args[0] == "run" {
		return runAttachment(ctx, args[1:])
	}

	return runAttachment(ctx, args)
}
