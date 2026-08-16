package main

import (
	"context"
	"errors"
	"flag"
	"strings"
	"testing"
)

// TestFlagHelpNeverErrors is the P0 onboarding fix: `--help`/`-h` on any
// flag.FlagSet-backed leaf command must return an error satisfying
// errors.Is(err, flag.ErrHelp) — the one sentinel main.go now special-cases
// (like context.Canceled and errConsoleServerDown) to exit 0 without
// printing "error: flag: help requested" on top of the usage text Go's flag
// package already wrote to stdout (every flag.FlagSet in this CLI calls
// flags.SetOutput(stdout), so f.usage() already lands there before
// ErrHelp is returned). This covers a representative set of commands
// spanning both real subcommands (send/read/...) and ones nested a level
// deeper under a string-dispatched router (admin agent remove, pair
// invite, service install).
func TestFlagHelpNeverErrors(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		flag    string
		wantSub string
	}{
		{"send long", []string{"send"}, "--help", "Usage of moltnet send:"},
		{"send short", []string{"send"}, "-h", "Usage of moltnet send:"},
		{"read", []string{"read"}, "--help", "Usage of moltnet read:"},
		{"conversations", []string{"conversations"}, "--help", "Usage of moltnet conversations:"},
		{"participants", []string{"participants"}, "--help", "Usage of moltnet participants:"},
		{"connect", []string{"connect"}, "--help", "Usage of moltnet connect:"},
		{"register-agent", []string{"register-agent"}, "--help", "Usage of moltnet register-agent:"},
		{"apply", []string{"apply"}, "--help", "Usage of moltnet apply:"},
		{"admin agent remove", []string{"admin", "agent", "remove"}, "--help", "Usage of moltnet admin agent remove:"},
		{"admin room members add", []string{"admin", "room", "members", "add"}, "--help", "Usage of moltnet admin room members add:"},
		{"pair invite", []string{"pair", "invite"}, "--help", "Usage of moltnet pair invite:"},
		{"service install", []string{"service", "install"}, "--help", "Usage of moltnet service install:"},
		{"skill install", []string{"skill", "install"}, "--help", "Usage of moltnet skill install:"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			args := append(append([]string(nil), testCase.args...), testCase.flag)

			var err error
			output := captureStdout(t, func() {
				err = run(context.Background(), args, "test")
			})

			if !errors.Is(err, flag.ErrHelp) {
				t.Fatalf("run(%v) error = %v, want an error satisfying errors.Is(err, flag.ErrHelp)", args, err)
			}
			if !strings.Contains(output, testCase.wantSub) {
				t.Fatalf("run(%v) stdout = %q, want it to contain %q", args, output, testCase.wantSub)
			}
		})
	}
}

// TestMainExitsZeroOnFlagHelp exercises main.go's own errors.Is(err,
// flag.ErrHelp) branch directly (runMain, not just run()) to guard against
// a future refactor accidentally routing ErrHelp back through the generic
// "error: ..." branch. It cannot observe main()'s os.Exit(0) without a
// subprocess (main() is untestable in-process by design), so it asserts
// the same thing every other main.go sentinel test in this package asserts
// for its own case (see init_signal_test.go's context.Canceled checks):
// runMain returns an error for which errors.Is(err, flag.ErrHelp) is true.
func TestMainExitsZeroOnFlagHelp(t *testing.T) {
	var err error
	output := captureStdout(t, func() {
		err = runMain([]string{"send", "--help"})
	})

	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("runMain() error = %v, want an error satisfying errors.Is(err, flag.ErrHelp)", err)
	}
	if !strings.Contains(output, "Usage of moltnet send:") {
		t.Fatalf("runMain() stdout = %q, want send usage", output)
	}
}

// TestDispatcherHelpFlagsReturnNil covers the second class of --help bug
// this fix closes: a few subcommand routers (admin, skill, attachment,
// bridge) dispatch on a bare string match against args[0] *before* any
// flag.FlagSet exists, so `--help`/`-h` never reached Go's flag package at
// all — they fell through to "unknown command" (admin, skill) or were
// handed to unrelated code as if they were a file path (attachment,
// bridge). isHelpArg (remove.go) is the shared three-way check
// ("help"/"--help"/"-h") that now backs every one of these routers; this
// asserts each accepts all three spellings and returns nil, not an error.
func TestDispatcherHelpFlagsReturnNil(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantSub string
	}{
		{"admin --help", []string{"admin", "--help"}, "moltnet admin"},
		{"admin -h", []string{"admin", "-h"}, "moltnet admin"},
		{"admin agent --help", []string{"admin", "agent", "--help"}, "moltnet admin"},
		{"admin room --help", []string{"admin", "room", "--help"}, "moltnet admin"},
		{"admin room members --help", []string{"admin", "room", "members", "--help"}, "moltnet admin"},
		{"skill --help", []string{"skill", "--help"}, "moltnet skill"},
		{"skill -h", []string{"skill", "-h"}, "moltnet skill"},
		{"attachment --help", []string{"attachment", "--help"}, "moltnet attachment run"},
		{"bridge --help", []string{"bridge", "--help"}, "moltnet bridge run"},
		// validate --help used to fall straight into discoverValidationTargets
		// and error as `inspect "--help": stat --help: no such file or
		// directory` — it never had an isHelpArg check like these other
		// dispatchers (validate.go).
		{"validate --help", []string{"validate", "--help"}, "moltnet validate"},
		{"validate -h", []string{"validate", "-h"}, "moltnet validate"},
		{"validate help", []string{"validate", "help"}, "moltnet validate"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var err error
			output := captureStdout(t, func() {
				err = run(context.Background(), testCase.args, "test")
			})

			if err != nil {
				t.Fatalf("run(%v) error = %v, want nil", testCase.args, err)
			}
			if !strings.Contains(output, testCase.wantSub) {
				t.Fatalf("run(%v) stdout = %q, want it to contain %q", testCase.args, output, testCase.wantSub)
			}
		})
	}
}

// TestUninstallHelpFlagReturnsNil covers uninstall separately: it checks
// --help/-h/help itself, before its own flag.FlagSet, so it belongs with
// the dispatcher-level cases above in spirit even though it is not routed
// through isHelpArg's callers list the same way (it calls isHelpArg
// directly on args[0] too — see uninstall.go).
func TestUninstallHelpFlagReturnsNil(t *testing.T) {
	var err error
	output := captureStdout(t, func() {
		err = run(context.Background(), []string{"uninstall", "--help"}, "test")
	})

	if err != nil {
		t.Fatalf("run() uninstall --help error = %v, want nil", err)
	}
	if !strings.Contains(output, "moltnet uninstall") {
		t.Fatalf("unexpected uninstall help output %q", output)
	}
}
