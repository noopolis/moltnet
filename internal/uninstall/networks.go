// Package uninstall backs `moltnet uninstall` (PLAN.md phase 5): the
// non-CLI mechanics of enumerating every network a `moltnet init` (or
// `moltnet service install`) may have left behind, scanning $PATH for other
// installed copies of the binary, and deleting the running executable
// itself. Flag parsing, prompting, and printing stay in cmd/moltnet.
package uninstall

import (
	"errors"
	"sort"
	"strings"

	"github.com/noopolis/moltnet/internal/app"
	"github.com/noopolis/moltnet/internal/service"
)

// NetworkIDs returns every network id `moltnet uninstall` should stop and
// remove the service for: the union of network directories under
// ~/.moltnet/ (internal/app.ListNetworkIDs) and installed unit/plist files
// for manager's GOOS (service.Manager.InstalledNetworkIDs). The union
// matters because either half of that pairing can go missing independently
// of the other: a network directory removed by hand while its service is
// still loaded, or a service uninstalled by hand while ~/.moltnet/<id>/
// still holds data. The result is sorted and deduplicated.
//
// A manager for an unsupported OS (anything but darwin/linux) is treated as
// "no installed services" rather than a hard error, so `moltnet uninstall`
// still enumerates and can `--purge` network directories on a host
// internal/service does not manage services on.
func NetworkIDs(manager *service.Manager) ([]string, error) {
	fromHome, err := app.ListNetworkIDs()
	if err != nil {
		return nil, err
	}

	fromUnits, err := manager.InstalledNetworkIDs()
	if err != nil {
		var unsupported service.ErrUnsupportedOS
		if !errors.As(err, &unsupported) {
			return nil, err
		}
		fromUnits = nil
	}

	seen := make(map[string]bool, len(fromHome)+len(fromUnits))
	ids := make([]string, 0, len(fromHome)+len(fromUnits))
	for _, group := range [][]string{fromHome, fromUnits} {
		for _, id := range group {
			if !validNetworkID(id) || seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// validNetworkID rejects two kinds of id that must never reach the
// uninstall plan: dot-directories (".", "..", or a hidden directory such as
// ".DS_Store" that os.ReadDir happens to also return) that were never a
// `moltnet init` network, and any id with leading/trailing whitespace,
// which would build a unit/plist path for "acme" (service.SystemdUnitPath
// trims first) while every other part of the plan still prints and acts on
// the untrimmed "acme " — two different ids for what should be one
// network.
func validNetworkID(id string) bool {
	if strings.HasPrefix(id, ".") {
		return false
	}
	return id != "" && strings.TrimSpace(id) == id
}
