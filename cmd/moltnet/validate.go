package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/pkg/nodeconfig"
)

func runValidate(args []string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		fmt.Fprint(stdout, buildValidateUsage())
		return nil
	}
	if len(args) > 1 {
		return os.ErrInvalid
	}

	target := "."
	if len(args) == 1 {
		target = args[0]
	}

	serverPath, nodePath, err := discoverValidationTargets(target)
	if err != nil {
		return err
	}
	if serverPath == "" && nodePath == "" {
		return errors.New("no Moltnet or MoltnetNode config found")
	}

	if serverPath != "" {
		// LoadConfigForPath (not LoadFile): the running server always loads
		// through the env-merged path (server.go's LoadConfigForPath call),
		// so validate must see the same environment overrides it would —
		// otherwise `MOLTNET_LISTEN_ADDR=0.0.0.0 moltnet validate` stays
		// silent about an exposure the server itself would warn about the
		// moment it actually started (PLAN.md phase 6a review, P3).
		cfg, err := app.LoadConfigForPath(serverPath, "")
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "validated %s\n", serverPath)
		if warning := app.NonLoopbackAnonymousWriteWarning(cfg); warning != "" {
			fmt.Fprintf(stdout, "  %s %s\n", yellow("warning:"), warning)
		}
	}
	if nodePath != "" {
		if _, err := nodeconfig.LoadFile(nodePath); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "validated %s\n", nodePath)
	}

	return nil
}

func discoverValidationTargets(target string) (string, string, error) {
	info, err := os.Stat(target)
	if err != nil {
		return "", "", fmt.Errorf("inspect %q: %w", target, err)
	}

	if info.IsDir() {
		return discoverConfigsInDirectory(target)
	}

	serverPath, err := validateServerConfigPath(target)
	if err == nil {
		return serverPath, "", nil
	}
	nodePath, nodeErr := validateNodeConfigPath(target)
	if nodeErr == nil {
		return "", nodePath, nil
	}

	return "", "", fmt.Errorf(
		"validate %q: %w",
		target,
		errors.Join(
			fmt.Errorf("as Moltnet config: %w", err),
			fmt.Errorf("as MoltnetNode config: %w", nodeErr),
		),
	)
}

func discoverConfigsInDirectory(directory string) (string, string, error) {
	serverPath, err := findFirstExisting(directory, app.DefaultDiscoveryOrder)
	if err != nil {
		return "", "", err
	}
	nodePath, err := findFirstExisting(directory, nodeconfig.DefaultDiscoveryOrder)
	if err != nil {
		return "", "", err
	}

	return serverPath, nodePath, nil
}

func findFirstExisting(directory string, candidates []string) (string, error) {
	for _, candidate := range candidates {
		path := filepath.Join(directory, candidate)
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect %q: %w", path, err)
		}
	}

	return "", nil
}

func validateServerConfigPath(path string) (string, error) {
	if _, err := app.LoadFile(path, ""); err != nil {
		return "", err
	}

	return path, nil
}

func validateNodeConfigPath(path string) (string, error) {
	if _, err := nodeconfig.LoadFile(path); err != nil {
		return "", err
	}

	return path, nil
}
