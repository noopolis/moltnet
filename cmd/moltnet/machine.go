package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/noopolis/moltnet/internal/client"
	"github.com/noopolis/moltnet/internal/machine"
	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

var errMachineStartup = errors.New("machine startup failed")

type machineIO struct {
	input  io.ReadCloser
	output io.Writer
}

var machineStdio = func() machineIO {
	return machineIO{input: os.Stdin, output: stdout}
}

type machineSessionRunner func(
	context.Context,
	clientconfig.AttachmentConfig,
	*client.Client,
	machine.DeliveryRegistry,
	machine.Executor,
	machineIO,
) error

var runMachineSession machineSessionRunner = runConfiguredMachineSession

func runMachine(ctx context.Context, args []string) error {
	return runMachineWithIO(ctx, args, machineStdio())
}

func runMachineWithIO(ctx context.Context, args []string, stream machineIO) error {
	if len(args) == 1 && args[0] == "help" {
		fmt.Fprint(stdout, buildMachineUsage())
		return nil
	}

	flags := flag.NewFlagSet("moltnet machine", flag.ContinueOnError)
	flags.SetOutput(stdout)
	flags.Usage = func() { fmt.Fprint(stdout, buildMachineUsage()) }

	configPath := flags.String("config", "", "explicit Moltnet client config path")
	networkID := flags.String("network", "", "Moltnet network id when multiple attachments are configured")
	memberID := flags.String("member", "", "Moltnet member id when a network has multiple attachments")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("machine does not accept positional arguments")
	}
	if strings.TrimSpace(*configPath) == "" {
		return fmt.Errorf("machine requires --config")
	}
	if stream.input == nil || stream.output == nil {
		return fmt.Errorf("machine standard I/O is unavailable")
	}

	config, _, err := loadClientConfig(*configPath)
	if err != nil {
		return errMachineStartup
	}
	attachment, err := config.ResolveAttachmentFor(*networkID, *memberID)
	if err != nil {
		return errMachineStartup
	}
	configuredClient, err := client.New(attachment)
	if err != nil {
		return errMachineStartup
	}
	registry := machine.NewDeliveryRegistry(protocol.MachineMaxDeliveryRegistry)
	executor := machine.NewProviderExecutor(attachment, configuredClient, registry)
	return runMachineSession(ctx, attachment, configuredClient, registry, executor, stream)
}

func runConfiguredMachineSession(
	ctx context.Context,
	attachment clientconfig.AttachmentConfig,
	configuredClient *client.Client,
	registry machine.DeliveryRegistry,
	executor machine.Executor,
	stream machineIO,
) error {
	_ = attachment
	_ = configuredClient
	session := machine.NewSession(ctx, stream.input, stream.output,
		machine.WithDeliveryRegistry(registry), machine.WithExecutor(executor))
	return session.Run()
}
