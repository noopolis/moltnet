package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/noopolis/moltnet/pkg/nodeconfig"
)

// HomeMoltnetDir returns ~/.moltnet, the root `moltnet init` writes into for
// global per-network installs when --dir is not given (PLAN.md phase 4's
// "onboarding fast path").
func HomeMoltnetDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".moltnet"), nil
}

// NetworkHomeDir returns the per-network directory `moltnet init` creates
// under ~/.moltnet for network id.
func NetworkHomeDir(id string) (string, error) {
	root, err := HomeMoltnetDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, strings.TrimSpace(id)), nil
}

// ResolveConfigPath implements the shared Moltnet server config discovery
// order for start/pair/relay/admin: explicit (--config, or MOLTNET_CONFIG,
// or one of the alternate cwd filenames DiscoverPath already recognizes)
// wins outright; otherwise the sole network directory under ~/.moltnet/
// is used, disambiguated by id when several exist. found is false, with a
// nil error, only when nothing exists anywhere in the discovery order —
// callers that treat a missing config as "use defaults" (start) and callers
// that treat it as an error ("run moltnet init first": pair, relay) both
// build on this same tri-state result.
func ResolveConfigPath(explicit string, id string) (path string, found bool, err error) {
	path, found, err = DiscoverPath(explicit)
	if err != nil || found {
		return path, found, err
	}
	return resolveHomeNetworkConfig(defaultConfigFile, id)
}

// ResolveNodeConfigPath applies the same fallback tier to MoltnetNode
// discovery, used by `moltnet node`.
func ResolveNodeConfigPath(explicit string, id string) (path string, found bool, err error) {
	path, found, err = nodeconfig.DiscoverPath(explicit)
	if err != nil || found {
		return path, found, err
	}
	return resolveHomeNetworkConfig(nodeconfig.DefaultPath, id)
}

// ListNetworkIDs returns the sorted subdirectory names under ~/.moltnet,
// each one a network id `moltnet init` created there — regardless of
// whether the network still contains a config file. Unlike
// resolveHomeNetworkConfig (which only counts a directory when it holds a
// specific filename), this is used by `moltnet uninstall` to enumerate
// every network home so a config file removed by hand does not hide a
// network's leftover service or data from the uninstall plan. A missing
// ~/.moltnet is not an error: it just means no networks were ever created
// there.
func ListNetworkIDs() ([]string, error) {
	root, err := HomeMoltnetDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("list %s: %w", root, err)
	}

	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			ids = append(ids, entry.Name())
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// resolveHomeNetworkConfig looks for filename under ~/.moltnet/<network-id>/
// directories. With id set, only that network's directory is considered.
// With id empty, exactly one network directory containing filename must
// exist; zero is reported as "not found" (found=false, err=nil) so callers
// can fall back to their own default behavior, and more than one is an
// error listing the candidates and pointing at --id.
func resolveHomeNetworkConfig(filename string, id string) (string, bool, error) {
	root, err := HomeMoltnetDir()
	if err != nil {
		return "", false, err
	}

	id = strings.TrimSpace(id)
	if id != "" {
		path := filepath.Join(root, id, filename)
		info, statErr := os.Stat(path)
		if statErr == nil && !info.IsDir() {
			return path, true, nil
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return "", false, fmt.Errorf("inspect %q: %w", path, statErr)
		}
		return "", false, fmt.Errorf("no network %q found under %s", id, root)
	}

	candidates, err := homeNetworkCandidates(root, filename)
	if err != nil {
		return "", false, err
	}

	switch len(candidates) {
	case 0:
		return "", false, nil
	case 1:
		return filepath.Join(root, candidates[0], filename), true, nil
	default:
		return "", false, fmt.Errorf(
			"multiple networks found under %s: %s; pass --id to choose one",
			root, strings.Join(candidates, ", "),
		)
	}
}

// homeNetworkCandidates lists the network ids (subdirectory names) under
// root that contain filename. A missing root is not an error: it just means
// no networks have been created there yet.
func homeNetworkCandidates(root string, filename string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("list %s: %w", root, err)
	}

	candidates := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), filename)
		if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
			candidates = append(candidates, entry.Name())
		}
	}
	sort.Strings(candidates)
	return candidates, nil
}
