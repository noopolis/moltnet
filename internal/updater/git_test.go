package updater

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// statusArgSpyRunner is a minimal CommandRunner that only answers the git
// subcommands inspectGitCheckout issues, recording exactly what `git
// status` was invoked with, so a test can pin the pathspec used without
// driving the full fakeSourceRunner harness.
type statusArgSpyRunner struct {
	statusArgs []string
}

func (r *statusArgSpyRunner) Run(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
	if name != "git" || len(args) == 0 {
		return nil, fmt.Errorf("unexpected command %s %v", name, args)
	}
	switch {
	case args[0] == "rev-parse" && len(args) >= 2 && args[1] == "--is-inside-work-tree":
		return []byte("true\n"), nil
	case args[0] == "rev-parse" && len(args) >= 2 && args[1] == "--abbrev-ref":
		return []byte("main\n"), nil
	case args[0] == "status":
		r.statusArgs = append([]string{}, args...)
		return []byte(""), nil
	case args[0] == "rev-parse" && len(args) >= 2 && args[1] == "HEAD":
		return []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"), nil
	case args[0] == "rev-parse" && len(args) >= 2 && args[1] == "@{u}":
		return nil, fmt.Errorf("fatal: no upstream configured")
	}
	return nil, fmt.Errorf("unexpected git args %v", args)
}

// TestInspectGitCheckoutExcludesGeneratedWebDist is the regression test for
// P2-7: `make build` regenerates the tracked web/dist directory (vite's
// emptyOutDir), so without this exclusion, verifying a rebuild would dirty
// the very tree a source update requires to be clean, permanently blocking
// every update after the first.
func TestInspectGitCheckoutExcludesGeneratedWebDist(t *testing.T) {
	spy := &statusArgSpyRunner{}
	if _, err := inspectGitCheckout(context.Background(), spy, "/tmp/checkout"); err != nil {
		t.Fatalf("inspectGitCheckout: %v", err)
	}
	if len(spy.statusArgs) == 0 {
		t.Fatal("expected git status to run")
	}
	joined := strings.Join(spy.statusArgs, " ")
	if !strings.Contains(joined, "web/dist") {
		t.Fatalf("expected git status args to exclude web/dist, got %v", spy.statusArgs)
	}
}

func TestCommandOutputTail(t *testing.T) {
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	output := []byte(strings.Join(lines, "\n"))

	tail := commandOutputTail(output, 20)
	if strings.Contains(tail, "line 1\n") {
		t.Fatalf("expected tail to drop early lines, got %q", tail)
	}
	if !strings.Contains(tail, "line 30") {
		t.Fatalf("expected tail to keep the last line, got %q", tail)
	}
	if commandOutputTail(nil, 20) != "" {
		t.Fatal("expected empty output to produce an empty tail")
	}
}

func TestWrapCommandErrorAppendsOutputTail(t *testing.T) {
	base := fmt.Errorf("exit status 2")
	wrapped := wrapCommandError(base, []byte("go build failed: undefined: foo\n"))
	if !strings.Contains(wrapped.Error(), "undefined: foo") {
		t.Fatalf("expected wrapped error to include command output, got %v", wrapped)
	}
	if wrapCommandError(nil, []byte("irrelevant")) != nil {
		t.Fatal("expected a nil error to stay nil")
	}
}
