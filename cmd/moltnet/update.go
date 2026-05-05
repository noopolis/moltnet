package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/noopolis/moltnet/internal/updater"
)

var runUpdater = updater.Run

func runUpdate(ctx context.Context, args []string, buildVersion string) error {
	flags := flag.NewFlagSet("moltnet update", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		check          = flags.Bool("check", false, "check for an available Moltnet update without mutating")
		dryRun         = flags.Bool("dry-run", false, "show the planned update without replacing the installed binary")
		serverURL      = flags.String("server", "", "Moltnet server URL to probe during the update check")
		serverTokenEnv = flags.String("server-token-env", "", "environment variable containing a server probe bearer token")
		targetVersion  = flags.String("version", "", "specific Moltnet release version to install")
		yes            = flags.Bool("yes", false, "accept update prompts")
	)

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("update does not accept positional arguments")
	}

	result, err := runUpdater(ctx, updater.Options{
		CheckOnly:      *check,
		CurrentVersion: buildVersion,
		DryRun:         *dryRun,
		ServerTokenEnv: *serverTokenEnv,
		ServerURL:      *serverURL,
		TargetVersion:  *targetVersion,
		Yes:            *yes,
	})
	if err != nil {
		if result.MutationRefused {
			_ = updater.WriteResult(stdout, result)
		}
		return err
	}
	return updater.WriteResult(stdout, result)
}
