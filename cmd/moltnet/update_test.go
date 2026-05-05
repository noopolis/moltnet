package main

import (
	"context"
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
	}, "v0.1.0")
	if err != nil {
		t.Fatalf("run() update error = %v", err)
	}
	if !captured.CheckOnly || !captured.DryRun || !captured.Yes {
		t.Fatalf("boolean flags were not captured: %#v", captured)
	}
	if captured.CurrentVersion != "v0.1.0" ||
		captured.TargetVersion != "v0.2.0" ||
		captured.ServerURL != "http://127.0.0.1:8787" {
		t.Fatalf("unexpected update options %#v", captured)
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
