package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/noopolis/moltnet/pkg/nodeconfig"
	"go.yaml.in/yaml/v3"
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
// order for start/pair/relay/admin/service:
//
//  1. Explicit (--config, or MOLTNET_CONFIG) wins outright.
//  2. Otherwise, when id is non-empty: ~/.moltnet/<id>/Moltnet is resolved
//     first. If ~/.moltnet/<id>/ does not exist at all, this falls back to
//     the same cwd discovery order step 3 uses — but ONLY when that cwd
//     candidate sniffs as text AND its own network.id equals id (P0-2, a
//     --dir install resolving by name from its own directory). This is a
//     name match, never blind cwd precedence: a same-named file (or, on a
//     case-insensitive filesystem such as APFS, a same-named compiled
//     binary like ./moltnet) that doesn't self-identify as id is refused,
//     erroring by name.
//  3. Otherwise (no explicit, no id), the cwd discovery order (./Moltnet
//     and the alternate filenames DiscoverPath recognizes) is tried, then
//     the sole network directory under ~/.moltnet/ — cwd participates only
//     in this no-flag, no-id path (aside from the name-matched fallback in
//     step 2).
//
// found is false, with a nil error, only when nothing exists anywhere in
// that order — callers that treat a missing config as "use defaults"
// (start) and callers that treat it as an error ("run moltnet init first":
// pair, relay) both build on this same tri-state result.
func ResolveConfigPath(explicit string, id string) (path string, found bool, err error) {
	path, found, hasExplicit, err := discoverExplicitConfigPath(explicit)
	if hasExplicit {
		return path, found, err
	}

	if id = strings.TrimSpace(id); id != "" {
		return resolveHomeNetworkConfig(defaultConfigFile, id, cwdIDFallback{
			discover:  discoverCwdConfigPath,
			networkID: cwdConfigNetworkID,
		})
	}

	path, found, err = discoverCwdConfigPath()
	if err != nil || found {
		return path, found, err
	}
	return resolveHomeNetworkConfig(defaultConfigFile, "", cwdIDFallback{})
}

// ResolveNodeConfigPath applies the same fallback tier to MoltnetNode
// discovery, used by `moltnet node`: explicit wins outright; a non-empty id
// resolves ~/.moltnet/<id>/MoltnetNode first, falling back to a
// name-matched cwd candidate (moltnet.network_id == id) only when
// ~/.moltnet/<id>/ does not exist at all (P0-2); with neither explicit nor
// id, cwd is tried before the sole ~/.moltnet/ network. explicit here
// already carries MOLTNET_NODE_CONFIG, resolved by the caller before this
// is called.
func ResolveNodeConfigPath(explicit string, id string) (path string, found bool, err error) {
	if strings.TrimSpace(explicit) != "" {
		return nodeconfig.DiscoverPath(explicit)
	}

	if id = strings.TrimSpace(id); id != "" {
		return resolveHomeNetworkConfig(nodeconfig.DefaultPath, id, cwdIDFallback{
			discover:  func() (string, bool, error) { return nodeconfig.DiscoverPath("") },
			networkID: cwdNodeNetworkID,
		})
	}

	path, found, err = nodeconfig.DiscoverPath("")
	if err != nil || found {
		return path, found, err
	}
	return resolveHomeNetworkConfig(nodeconfig.DefaultPath, "", cwdIDFallback{})
}

// cwdIDFallback bundles what resolveHomeNetworkConfig needs to fall back to
// a name-matched cwd candidate when an explicit --id has no
// ~/.moltnet/<id>/ directory (P0-2): discover locates a cwd candidate using
// the same discovery order the no-id path already uses (so it is already
// text-sniffed — a binary shadow never reaches networkID), and networkID
// reads the network id that candidate declares. Both are nil in the id=""
// case, where the fallback never runs.
type cwdIDFallback struct {
	discover  func() (path string, found bool, err error)
	networkID func(path string) (id string, ok bool)
}

// resolve reports whether f's cwd discovery order turns up a candidate that
// declares network id == id — the only condition under which --id may fall
// back to a cwd config. Discovery failure, no candidate, or a candidate
// that fails to parse or names a different network are all "no match, no
// error": the caller's own "no network found" error covers every one of
// them identically, since none of them is evidence of anything wrong with
// id itself.
func (f cwdIDFallback) resolve(id string) (path string, ok bool) {
	if f.discover == nil || f.networkID == nil {
		return "", false
	}
	candidate, found, err := f.discover()
	if err != nil || !found {
		return "", false
	}
	candidateID, ok := f.networkID(candidate)
	if !ok || candidateID != id {
		return "", false
	}
	return candidate, true
}

// cwdConfigNetworkID reads path's declared network.id without running the
// full validation loadFileConfig performs (permission checks, room/pairing
// validation) — it exists solely so cwdIDFallback can compare a cwd
// candidate against a requested --id (P0-2) before anything tries to fully
// load it. The full loader still runs, and can still fail loudly, once the
// path is actually chosen. A decode failure reports ("", false): the
// candidate just cannot be identified as any network, which is not itself
// an error.
func cwdConfigNetworkID(path string) (string, bool) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var config rawConfigFile
	if err := decodeConfigBytes(path, contents, &config); err != nil {
		return "", false
	}
	id := strings.TrimSpace(config.Network.ID)
	return id, id != ""
}

// cwdNodeNetworkID is cwdConfigNetworkID's MoltnetNode counterpart: it
// decodes moltnet.network_id from a cwd candidate using a plain (non-strict)
// unmarshal into nodeconfig.Config's exported fields, skipping the
// validation, permission checks, and token-path resolution nodeconfig.LoadFile
// performs — those still run, in full, once the path is actually chosen.
func cwdNodeNetworkID(path string) (string, bool) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var config nodeconfig.Config
	var decodeErr error
	if strings.EqualFold(filepath.Ext(path), ".json") {
		decodeErr = json.Unmarshal(contents, &config)
	} else {
		decodeErr = yaml.Unmarshal(contents, &config)
	}
	if decodeErr != nil {
		return "", false
	}
	id := strings.TrimSpace(config.Moltnet.NetworkID)
	return id, id != ""
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
// directories. With id set, only that network's directory is considered
// first; when ~/.moltnet/<id>/ does not exist at all, cwdFallback lets a
// --dir install still resolve by name (P0-2) — see cwdIDFallback.resolve.
// With id empty, exactly one network directory containing filename must
// exist; zero is reported as "not found" (found=false, err=nil) so callers
// can fall back to their own default behavior, and more than one is an
// error listing the candidates and pointing at --id.
func resolveHomeNetworkConfig(filename string, id string, cwdFallback cwdIDFallback) (string, bool, error) {
	root, err := HomeMoltnetDir()
	if err != nil {
		return "", false, err
	}

	id = strings.TrimSpace(id)
	if id != "" {
		path := filepath.Join(root, id, filename)
		info, statErr := os.Stat(path)
		if statErr == nil && !info.IsDir() {
			ok, sniffErr := looksLikeTextConfig(path)
			if sniffErr != nil {
				return "", false, fmt.Errorf("inspect %q: %w", path, sniffErr)
			}
			if !ok {
				return "", false, homeNetworkNotTextError(path)
			}
			return path, true, nil
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return "", false, fmt.Errorf("inspect %q: %w", path, statErr)
		}
		if statErr != nil {
			// No config file for id under ~/.moltnet: a --dir install
			// named differently from its network id can still resolve
			// here, but only by a positive name match against a cwd
			// candidate (P0-2) — never blind cwd precedence. An unrelated
			// or mismatched cwd candidate falls straight through to the
			// "no network" error below, exactly as if cwd held nothing.
			if cwdPath, ok := cwdFallback.resolve(id); ok {
				return cwdPath, true, nil
			}
		}
		return "", false, homeNetworkNotFoundError(root, id)
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

// homeNetworkNotTextError is the P2-5 error for a network directory that
// does exist but whose config content fails the text sniff — distinct from
// homeNetworkNotFoundError's "no such network" case, since here the
// directory and file are both right there; only their content is wrong.
func homeNetworkNotTextError(path string) error {
	return fmt.Errorf("%s is not a text config file (did a compiled binary shadow the config filename on a case-insensitive filesystem?)", path)
}

// homeNetworkNotFoundError is the P2-5 error for "no such network anywhere
// this id could resolve" — ~/.moltnet/<id>/ absent and no name-matched cwd
// fallback either. It names the ids that do exist under root (if any, and
// if root is itself listable) so a typo is obvious at a glance, and
// suggests --config as the escape hatch for a network this id genuinely
// cannot reach (a --dir install under a different name with no matching
// cwd, or one run from an unrelated directory).
func homeNetworkNotFoundError(root string, id string) error {
	ids, _ := ListNetworkIDs()
	if len(ids) == 0 {
		return fmt.Errorf("no network %q found under %s; pass --config to use an explicit path", id, root)
	}
	return fmt.Errorf(
		"no network %q found under %s (available: %s); pass --config to use an explicit path",
		id, root, strings.Join(ids, ", "),
	)
}

// homeNetworkCandidates lists the network ids (subdirectory names) under
// root that contain filename. A missing root is not an error: it just means
// no networks have been created there yet. A candidate whose config exists
// but cannot be sniffed (a permission error on that one sibling network, for
// example) is skipped like any other unusable candidate (P2-3) rather than
// failing the whole no-flag/no-id resolution for every other network under
// root.
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
		info, statErr := os.Stat(path)
		if statErr != nil || info.IsDir() {
			continue
		}
		ok, sniffErr := looksLikeTextConfig(path)
		if sniffErr != nil {
			continue
		}
		if !ok {
			warnIgnoringBinaryConfig(path)
			continue
		}
		candidates = append(candidates, entry.Name())
	}
	sort.Strings(candidates)
	return candidates, nil
}
