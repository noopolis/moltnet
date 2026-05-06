package main

import (
	"context"
	"strings"
	"testing"
)

func TestRunVersionCommand(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"version"}, "1.2.3"); err != nil {
			t.Fatalf("run() version error = %v", err)
		}
	})

	if strings.TrimSpace(output) != "1.2.3" {
		t.Fatalf("unexpected version output %q", output)
	}
}

func TestRunVersionFlag(t *testing.T) {
	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{"--version"}, "1.2.3"); err != nil {
			t.Fatalf("run() --version error = %v", err)
		}
	})

	if strings.TrimSpace(output) != "1.2.3" {
		t.Fatalf("unexpected version output %q", output)
	}
}

func TestRunMainUsesVersion(t *testing.T) {
	output := captureStdout(t, func() {
		if err := runMain([]string{"version"}); err != nil {
			t.Fatalf("runMain() error = %v", err)
		}
	})

	if strings.TrimSpace(output) != version {
		t.Fatalf("unexpected version output %q", output)
	}
}

func TestMainVersion(t *testing.T) {
	output := captureMainOutput(t, []string{"moltnet", "version"}, func() {
		main()
	})
	if strings.TrimSpace(output) != version {
		t.Fatalf("unexpected main() version output %q", output)
	}
}
