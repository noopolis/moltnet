package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/internal/service"
)

// newServiceManager constructs the service.Manager runServiceCommand uses.
// It is a var, not a direct service.New() call, purely so tests can inject
// a fake-runner Manager and exercise the full CLI dispatch without ever
// touching a real launchctl/systemctl.
var newServiceManager = service.New

// runServiceCommand implements `moltnet service install|uninstall|start|stop|status`
// (PLAN.md phase 4, item 2): a launchd LaunchAgent on macOS, a systemd user
// unit on Linux, generated for and managing the resolved network's server.
func runServiceCommand(ctx context.Context, args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprint(stdout, buildServiceUsage())
		return nil
	}

	action := args[0]
	switch action {
	case "install", "uninstall", "start", "stop", "status":
	default:
		fmt.Fprint(stdout, buildServiceUsage())
		return fmt.Errorf("unknown service command %q", action)
	}

	flags := flag.NewFlagSet("moltnet service "+action, flag.ContinueOnError)
	flags.SetOutput(stdout)
	var (
		configPath  = flags.String("config", "", "Moltnet config path")
		id          = flags.String("id", "", "network id to select under ~/.moltnet when several exist")
		verboseFlag = flags.Bool("verbose", false, "print full detail: unit file and log paths, raw service manager status")
	)
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("service %s does not accept positional arguments", action)
	}

	spec, err := resolveServiceSpec(*configPath, *id)
	if err != nil {
		return err
	}

	manager := newServiceManager()
	switch action {
	case "install":
		return runServiceInstall(ctx, manager, spec, *verboseFlag)
	case "uninstall":
		return runServiceUninstall(ctx, manager, spec.NetworkID)
	case "start":
		if err := manager.Start(ctx, spec.NetworkID); err != nil {
			return err
		}
		printInitConfigCheckLine("service started", spec.NetworkID)
		return nil
	case "stop":
		if err := manager.Stop(ctx, spec.NetworkID); err != nil {
			return err
		}
		printInitConfigCheckLine("service stopped", spec.NetworkID)
		return nil
	default: // "status"
		return runServiceStatus(ctx, manager, spec.NetworkID, *verboseFlag)
	}
}

// runServiceInstall installs and starts the service, then prints the
// house-style aftercare: by default, a single "service running" outcome
// line and a "next:" nudge toward `relay deploy`; with --verbose, the unit
// file and log paths this command used to always print.
func runServiceInstall(ctx context.Context, manager *service.Manager, spec service.Spec, verbose bool) error {
	if err := manager.Install(ctx, spec); err != nil {
		return err
	}
	printInitConfigCheckLine("service running", "")
	if verbose {
		unitPath, err := manager.UnitPath(spec.NetworkID)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, dim(fmt.Sprintf("    unit file: %s", unitPath)))
		fmt.Fprintln(stdout, dim(fmt.Sprintf("    logs: %s, %s", spec.StdoutLogPath(), spec.StderrLogPath())))
	}
	printNextStep(nextStep{
		command:     fmt.Sprintf("moltnet relay deploy --id %s", spec.NetworkID),
		description: "relay on Cloudflare (pair across NAT)",
	})
	return nil
}

func runServiceUninstall(ctx context.Context, manager *service.Manager, networkID string) error {
	if err := manager.Uninstall(ctx, networkID); err != nil {
		return err
	}
	printInitConfigCheckLine("service removed", "")
	return nil
}

// runServiceStatus prints one outcome line naming the service's state
// (running, stopped-but-installed, or not installed at all — the last two
// are real, actionable facts, so they always print, never behind
// --verbose), plus the platform tool's raw status output when --verbose.
func runServiceStatus(ctx context.Context, manager *service.Manager, networkID string, verbose bool) error {
	status, err := manager.Status(ctx, networkID)
	if err != nil {
		return err
	}
	if !status.Installed {
		fmt.Fprintf(stdout, "  %s no installed service for %q; run `moltnet service install --id %s`\n", yellow("note:"), networkID, networkID)
		return nil
	}
	if status.Running {
		printInitConfigCheckLine("service running", networkID)
	} else {
		fmt.Fprintf(stdout, "  %s service installed but not running for %q; run `moltnet service start --id %s`\n", yellow("note:"), networkID, networkID)
	}
	if verbose && status.Detail != "" {
		fmt.Fprintln(stdout, dim(status.Detail))
	}
	return nil
}

// resolveServiceSpec resolves the network config the same way
// start/pair/relay do (app.ResolveConfigPath via resolvePairConfigPath —
// with --id given, ~/.moltnet/<id>/Moltnet is resolved first, falling back
// to cwd only when its config self-identifies as network id id), then
// derives the rest of service.Spec from it: NetworkID from the loaded
// config (not the directory name, since --dir installs are not necessarily
// named after the network id), BinaryPath from the currently running
// executable (symlink-resolved so a re-exec later still finds the real
// binary), and NetworkDir from the config file's directory.
func resolveServiceSpec(configFlag string, id string) (service.Spec, error) {
	path, err := resolvePairConfigPath(configFlag, id)
	if err != nil {
		return service.Spec{}, err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return service.Spec{}, fmt.Errorf("resolve %q: %w", path, err)
	}

	cfg, err := app.LoadConfigForPath(absPath, "")
	if err != nil {
		return service.Spec{}, err
	}

	binaryPath, err := os.Executable()
	if err != nil {
		return service.Spec{}, fmt.Errorf("resolve moltnet binary path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(binaryPath); err == nil {
		binaryPath = resolved
	}

	return service.Spec{
		NetworkID:  cfg.NetworkID,
		ConfigPath: absPath,
		BinaryPath: binaryPath,
		NetworkDir: filepath.Dir(absPath),
	}, nil
}
