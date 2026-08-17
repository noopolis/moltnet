package updater

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sourceGitState is the mutable state a fakeSourceRunner's "git" handler
// reads and (on a successful `git pull --ff-only`) advances. Every test in
// sourceupdate_test.go drives it directly instead of ever touching a real
// git binary or this repository's own checkout.
type sourceGitState struct {
	branch         string
	detached       bool
	dirty          bool
	fetchErr       error
	localCommit    string
	notARepo       bool
	pullErr        error
	upstreamCommit string
	upstreamKnown  bool
}

// sourceMakeBehavior controls what a fakeSourceRunner's "make build" call
// does: succeed and write a working `bin/moltnet` script reporting
// reportedVersion, fail outright (err), or "succeed" while writing a binary
// that itself fails to report a version (reportedVersion left empty).
type sourceMakeBehavior struct {
	err             error
	reportedVersion string
}

// fakeSourceRunner is the CommandRunner tests inject in place of
// defaultCommandRunner, so `git`/`make` are never actually invoked.
type fakeSourceRunner struct {
	calls    []string
	checkout string
	dirs     []string
	git      *sourceGitState
	make     sourceMakeBehavior
}

func (f *fakeSourceRunner) Run(_ context.Context, dir string, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, strings.TrimSpace(name+" "+strings.Join(args, " ")))
	f.dirs = append(f.dirs, dir)
	switch name {
	case "git":
		return f.runGit(args)
	case "make":
		return f.runMake(args)
	default:
		return nil, fmt.Errorf("fakeSourceRunner: unexpected command %s", name)
	}
}

func (f *fakeSourceRunner) runGit(args []string) ([]byte, error) {
	state := f.git
	switch {
	case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--is-inside-work-tree":
		if state.notARepo {
			return nil, fmt.Errorf("fatal: not a git repository (or any of the parent directories)")
		}
		return []byte("true\n"), nil
	case len(args) >= 1 && args[0] == "fetch":
		if state.fetchErr != nil {
			return nil, state.fetchErr
		}
		return []byte(""), nil
	case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--abbrev-ref":
		if state.detached {
			return []byte("HEAD\n"), nil
		}
		return []byte(state.branch + "\n"), nil
	case len(args) >= 2 && args[0] == "status" && args[1] == "--porcelain":
		if state.dirty {
			return []byte(" M some/file.go\n"), nil
		}
		return []byte(""), nil
	case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "HEAD":
		return []byte(state.localCommit + "\n"), nil
	case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "@{u}":
		if !state.upstreamKnown {
			return nil, fmt.Errorf("fatal: no upstream configured for branch %q", state.branch)
		}
		return []byte(state.upstreamCommit + "\n"), nil
	case len(args) >= 2 && args[0] == "pull" && args[1] == "--ff-only":
		if state.pullErr != nil {
			return nil, state.pullErr
		}
		state.localCommit = state.upstreamCommit
		return []byte("Updating to " + state.localCommit + "\n"), nil
	}
	return nil, fmt.Errorf("fakeSourceRunner: unhandled git args %v", args)
}

func (f *fakeSourceRunner) runMake(args []string) ([]byte, error) {
	if len(args) != 1 || args[0] != "build" {
		return nil, fmt.Errorf("fakeSourceRunner: unexpected make args %v", args)
	}
	if f.make.err != nil {
		return nil, f.make.err
	}
	binDir := filepath.Join(f.checkout, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return nil, err
	}
	var script string
	if f.make.reportedVersion == "" {
		// A build that "succeeds" (make exits 0) but produced a binary that
		// itself fails to run — the version-verify failure scenario.
		script = "#!/bin/sh\nexit 1\n"
	} else {
		script = fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"version\" ]; then echo %s; exit 0; fi\n", f.make.reportedVersion)
	}
	if err := os.WriteFile(filepath.Join(binDir, "moltnet"), []byte(script), 0o755); err != nil {
		return nil, err
	}
	return []byte("go build -o bin/moltnet ./cmd/moltnet\n"), nil
}

func (f *fakeSourceRunner) ran(prefix string) bool {
	for _, call := range f.calls {
		if strings.HasPrefix(call, prefix) {
			return true
		}
	}
	return false
}

func sourceUpdateOptions(t *testing.T, installPath string, checkout string, runner CommandRunner) Options {
	t.Helper()
	t.Setenv(moltnetHomeEnv, missingMoltnetHome(t))

	return Options{
		CommandRunner:  runner,
		CurrentVersion: "0.0.0-dev",
		Detector: staticDetector{install: Install{
			Method: InstallMethodSource,
			Path:   installPath,
		}},
		LockPath:       installPath + ".test.update.lock",
		Platform:       Platform{OS: "linux", Arch: "amd64"},
		SourceCheckout: checkout,
	}
}
