package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/pkg/nodeconfig"
)

func runInit(args []string) error {
	if len(args) > 1 {
		return os.ErrInvalid
	}

	directory := "."
	if len(args) == 1 {
		directory = args[0]
	}

	root := filepath.Clean(strings.TrimSpace(directory))
	if root == "" {
		root = "."
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create init directory %q: %w", root, err)
	}
	if root != "." {
		if err := os.Chmod(root, 0o700); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("secure init directory %q: %w", root, err)
		}
	}

	serverPath := filepath.Join(root, app.DefaultPath)
	nodePath := filepath.Join(root, nodeconfig.DefaultPath)

	serverCreated, err := writeFileIfMissing(serverPath, defaultMoltnetConfig())
	if err != nil {
		return err
	}
	nodeCreated, err := writeFileIfMissing(nodePath, defaultMoltnetNodeConfig())
	if err != nil {
		return err
	}

	if !serverCreated {
		fmt.Fprintf(stdout, "exists %s\n", serverPath)
	}
	if !nodeCreated {
		fmt.Fprintf(stdout, "exists %s\n", nodePath)
	}

	fmt.Fprintf(stdout, "initialized %s\n", root)
	if !serverCreated && !nodeCreated {
		fmt.Fprintln(stdout, "no files created")
		return nil
	}

	if serverCreated {
		fmt.Fprintf(stdout, "created %s\n", serverPath)
	}
	if nodeCreated {
		fmt.Fprintf(stdout, "created %s\n", nodePath)
	}

	return nil
}

func writeFileIfMissing(path string, contents string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect %q: %w", path, err)
	}

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		return false, fmt.Errorf("write %q: %w", path, err)
	}

	return true, nil
}
