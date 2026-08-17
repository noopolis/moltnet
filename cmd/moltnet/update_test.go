package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/updater"
)

func TestRunUpdateParsesFlags(t *testing.T) {
	processStateMu.Lock()
	defer processStateMu.Unlock()

	previousRunner := runUpdater
	previousStdout := stdout
	defer func() {
		runUpdater = previousRunner
		stdout = previousStdout
	}()
	stdout = io.Discard

	var captured updater.Options
	runUpdater = func(_ context.Context, options updater.Options) (updater.Result, error) {
		captured = options
		return updater.Result{CheckOnly: options.CheckOnly, CurrentVersion: options.CurrentVersion}, nil
	}

	err := run(context.Background(), []string{
		"update",
		"--check",
		"--version", "v0.2.0",
		"--dry-run",
		"--yes",
		"--server", "http://127.0.0.1:8787",
		"--server-token-env", "MOLTNET_UPDATE_TOKEN",
	}, "v0.1.0")
	if err != nil {
		t.Fatalf("run() update error = %v", err)
	}
	if !captured.CheckOnly || !captured.DryRun || !captured.Yes {
		t.Fatalf("boolean flags were not captured: %#v", captured)
	}
	if captured.CurrentVersion != "v0.1.0" ||
		captured.TargetVersion != "v0.2.0" ||
		captured.ServerURL != "http://127.0.0.1:8787" ||
		captured.ServerTokenEnv != "MOLTNET_UPDATE_TOKEN" {
		t.Fatalf("unexpected update options %#v", captured)
	}
}

func TestRunUpdateWritesMutationRefusalOutput(t *testing.T) {
	processStateMu.Lock()
	defer processStateMu.Unlock()

	previousRunner := runUpdater
	defer func() {
		runUpdater = previousRunner
	}()

	const refusal = "self-update is not available for source or development builds; install a release tarball with curl -fsSL https://moltnet.dev/install.sh | sh"
	runUpdater = func(_ context.Context, options updater.Options) (updater.Result, error) {
		return updater.Result{
			AssetName:       "moltnet_linux_amd64.tar.gz",
			CurrentVersion:  options.CurrentVersion,
			Install:         updater.Install{Method: updater.InstallMethodSource, Path: "/tmp/moltnet"},
			MutationRefused: true,
			RefusalReason:   refusal,
			TargetVersion:   "v0.2.0",
			UpdateAvailable: true,
		}, errors.New(refusal)
	}

	var runErr error
	output := captureStdoutLocked(t, func() {
		runErr = run(context.Background(), []string{"update"}, "0.0.0-dev")
	})
	if runErr == nil {
		t.Fatal("expected mutation refusal error")
	}
	if !strings.Contains(output, "Self-update is not available") ||
		!strings.Contains(output, "install a release tarball") {
		t.Fatalf("expected actionable refusal output, got %q", output)
	}
	if strings.Count(output, "install a release tarball") != 1 {
		t.Fatalf("expected the refusal reason to print exactly once, got %q", output)
	}
}

func TestRunUpdatePassesSourceCheckoutStamp(t *testing.T) {
	processStateMu.Lock()
	defer processStateMu.Unlock()

	previousRunner := runUpdater
	previousStdout := stdout
	previousSourceCheckout := sourceCheckout
	defer func() {
		runUpdater = previousRunner
		stdout = previousStdout
		sourceCheckout = previousSourceCheckout
	}()
	stdout = io.Discard
	sourceCheckout = "/home/dev/checkouts/moltnet"

	var captured updater.Options
	runUpdater = func(_ context.Context, options updater.Options) (updater.Result, error) {
		captured = options
		return updater.Result{}, nil
	}

	if err := run(context.Background(), []string{"update", "--check"}, "0.0.0-dev"); err != nil {
		t.Fatalf("run() update error = %v", err)
	}
	if captured.SourceCheckout != "/home/dev/checkouts/moltnet" {
		t.Fatalf("SourceCheckout not forwarded, got %#v", captured)
	}
}

func TestRunUpdateSourceStepsOnlyPrintWithVerbose(t *testing.T) {
	processStateMu.Lock()
	defer processStateMu.Unlock()

	previousRunner := runUpdater
	defer func() {
		runUpdater = previousRunner
	}()

	runUpdater = func(_ context.Context, options updater.Options) (updater.Result, error) {
		return updater.Result{
			CurrentVersion: options.CurrentVersion,
			Install:        updater.Install{Method: updater.InstallMethodSource, Path: "/tmp/moltnet"},
			SourceUpdate: &updater.SourceUpdatePlan{
				Branch:   "main",
				Checkout: "/tmp/checkout",
				Clean:    true,
				Steps: []string{
					"pulled latest changes in /tmp/checkout",
					"ran `make build`",
				},
			},
			TargetVersion: "0.0.0-dev",
			Updated:       true,
		}, nil
	}

	quietOutput := captureStdoutLocked(t, func() {
		if err := run(context.Background(), []string{"update"}, "0.0.0-dev"); err != nil {
			t.Fatalf("run() update error = %v", err)
		}
	})
	if strings.Contains(quietOutput, "pulled latest changes") {
		t.Fatalf("expected quiet output to omit source steps, got %q", quietOutput)
	}

	verboseOutput := captureStdoutLocked(t, func() {
		if err := run(context.Background(), []string{"update", "--verbose"}, "0.0.0-dev"); err != nil {
			t.Fatalf("run() update --verbose error = %v", err)
		}
	})
	if !strings.Contains(verboseOutput, "pulled latest changes") || !strings.Contains(verboseOutput, "ran `make build`") {
		t.Fatalf("expected verbose output to include source steps, got %q", verboseOutput)
	}
}

func TestRunUpdateRejectsPositionalArguments(t *testing.T) {
	err := run(context.Background(), []string{"update", "extra"}, "test")
	if err == nil {
		t.Fatal("expected positional argument error")
	}
	if !strings.Contains(err.Error(), "update does not accept positional arguments") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestRunHelpIncludesUpdateCommand(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"help"}, "test"); err != nil {
			t.Fatalf("run() help error = %v", err)
		}
	})

	if !strings.Contains(output, "moltnet update") {
		t.Fatalf("expected update usage, got %q", output)
	}
}

func TestBuildUsageUpdateSynopsisIncludesVerbose(t *testing.T) {
	usage := buildUsage()
	for _, line := range strings.Split(usage, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "moltnet update ") {
			if !strings.Contains(line, "[--verbose]") {
				t.Fatalf("expected update synopsis to list --verbose, got %q", line)
			}
			return
		}
	}
	t.Fatal("expected buildUsage() to contain a moltnet update synopsis line")
}
