package app

import (
	"path/filepath"

	"github.com/noopolis/moltnet/internal/automation"
)

// The owner-control store is private, local to this network and independent
// of the message database. Other deployments preserve existing bridge policy.
func localAutomation(config Config) (*automation.Manager, error) {
	if !config.Console.LocalOwnerLogin {
		return nil, nil
	}
	path := ""
	switch config.Storage.Kind {
	case storageKindSQLite:
		path = filepath.Join(filepath.Dir(config.Storage.SQLite.Path), "automation.json")
	case storageKindJSON:
		path = filepath.Join(filepath.Dir(config.Storage.JSON.Path), "automation.json")
	}
	return automation.New(path, config.NetworkID)
}
