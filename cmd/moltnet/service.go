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
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
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
		configPath = flags.String("config", "", "Moltnet config path")
		id         = flags.String("id", "", "network id to select under ~/.moltnet when several exist")
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
		return runServiceInstall(ctx, manager, spec)
	case "uninstall":
		return runServiceUninstall(ctx, manager, spec.NetworkID)
	case "start":
		if err := manager.Start(ctx, spec.NetworkID); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "started the moltnet service for network %q\n", spec.NetworkID)
		return nil
	case "stop":
		if err := manager.Stop(ctx, spec.NetworkID); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "stopped the moltnet service for network %q\n", spec.NetworkID)
		return nil
	default: // "status"
		return runServiceStatus(ctx, manager, spec.NetworkID)
	}
}

func runServiceInstall(ctx context.Context, manager *service.Manager, spec service.Spec) error {
	if err := manager.Install(ctx, spec); err != nil {
		return err
	}
	unitPath, err := manager.UnitPath(spec.NetworkID)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "installed and started the moltnet service for network %q\n", spec.NetworkID)
	fmt.Fprintln(stdout, dim(fmt.Sprintf("unit file: %s", unitPath)))
	fmt.Fprintln(stdout, dim(fmt.Sprintf("logs: %s, %s", spec.StdoutLogPath(), spec.StderrLogPath())))
	return nil
}

func runServiceUninstall(ctx context.Context, manager *service.Manager, networkID string) error {
	if err := manager.Uninstall(ctx, networkID); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "uninstalled the moltnet service for network %q\n", networkID)
	return nil
}

func runServiceStatus(ctx context.Context, manager *service.Manager, networkID string) error {
	status, err := manager.Status(ctx, networkID)
	if err != nil {
		return err
	}
	if !status.Installed {
		fmt.Fprintf(stdout, "network %q has no installed service; run `moltnet service install --id %s`\n", networkID, networkID)
		return nil
	}
	state := "stopped"
	if status.Running {
		state = "running"
	}
	fmt.Fprintf(stdout, "network %q service: installed, %s\n", networkID, state)
	if status.Detail != "" {
		fmt.Fprintln(stdout, status.Detail)
	}
	return nil
}

// resolveServiceSpec resolves the network config the same way
// start/pair/relay do (app.ResolveConfigPath via resolvePairConfigPath),
// then derives the rest of service.Spec from it: NetworkID from the loaded
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
