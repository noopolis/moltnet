package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallMoltnetSkillOpenClaw(t *testing.T) {
	workspace := t.TempDir()

	installed, err := installMoltnetSkill("openclaw", workspace, "name: moltnet\n")
	if err != nil {
		t.Fatalf("installMoltnetSkill() error = %v", err)
	}

	targetPath := filepath.Join(workspace, "skills", "moltnet", "SKILL.md")
	if installed != targetPath {
		t.Fatalf("installMoltnetSkill() path = %q, want %q", installed, targetPath)
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "name: moltnet\n" {
		t.Fatalf("unexpected skill content %q", string(content))
	}
}

func TestInstallMoltnetSkillTinyClaw(t *testing.T) {
	workspace := t.TempDir()

	installed, err := installMoltnetSkill("tinyclaw", workspace, "name: moltnet\n")
	if err != nil {
		t.Fatalf("installMoltnetSkill() error = %v", err)
	}

	paths := strings.Split(installed, ", ")
	if len(paths) != 2 {
		t.Fatalf("expected two installed paths, got %q", installed)
	}

	for _, targetPath := range []string{
		filepath.Join(workspace, ".agents", "skills", "moltnet", "SKILL.md"),
		filepath.Join(workspace, ".claude", "skills", "moltnet", "SKILL.md"),
	} {
		content, err := os.ReadFile(targetPath)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", targetPath, err)
		}
		if string(content) != "name: moltnet\n" {
			t.Fatalf("unexpected skill content at %q: %q", targetPath, string(content))
		}
	}
}

func TestInstallMoltnetSkillClaudeCodeAndCodex(t *testing.T) {
	workspace := t.TempDir()

	installed, err := installMoltnetSkill("claude-code", workspace, "name: moltnet\n")
	if err != nil {
		t.Fatalf("installMoltnetSkill() claude-code error = %v", err)
	}
	claudePath := filepath.Join(workspace, ".claude", "skills", "moltnet", "SKILL.md")
	if installed != claudePath {
		t.Fatalf("installMoltnetSkill() claude path = %q, want %q", installed, claudePath)
	}
	assertFileExists(t, claudePath)

	installed, err = installMoltnetSkill("codex", workspace, "name: moltnet\n")
	if err != nil {
		t.Fatalf("installMoltnetSkill() codex error = %v", err)
	}
	paths := strings.Split(installed, ", ")
	if len(paths) != 2 {
		t.Fatalf("expected two codex skill paths, got %q", installed)
	}
	for _, targetPath := range []string{
		filepath.Join(workspace, ".agents", "skills", "moltnet", "SKILL.md"),
		filepath.Join(workspace, ".codex", "skills", "moltnet", "SKILL.md"),
	} {
		assertFileExists(t, targetPath)
	}
}

// TestInstallMoltnetSkillUnknownRuntimeIsAccepted covers PLAN.md phase 6b:
// `moltnet skill install` must never refuse a runtime name. An unrecognized
// name (the exact shape of the field report's Grok agent) still gets a
// usable skill file at the generic `.agents/skills/moltnet/` path, teaching
// the same `moltnet conversations`/`read`/`send` contract every runtime
// gets.
func TestInstallMoltnetSkillUnknownRuntimeIsAccepted(t *testing.T) {
	for _, runtime := range []string{"grok", "agy", "some-future-runtime"} {
		t.Run(runtime, func(t *testing.T) {
			workspace := t.TempDir()

			installed, err := installMoltnetSkill(runtime, workspace, moltnetSkillContent())
			if err != nil {
				t.Fatalf("installMoltnetSkill(%q) error = %v, want no error (runtimes are never refused)", runtime, err)
			}

			targetPath := filepath.Join(workspace, ".agents", "skills", "moltnet", "SKILL.md")
			if installed != targetPath {
				t.Fatalf("installMoltnetSkill(%q) path = %q, want %q", runtime, installed, targetPath)
			}
			assertFileExists(t, targetPath)

			content, err := os.ReadFile(targetPath)
			if err != nil {
				t.Fatalf("ReadFile(%q) error = %v", targetPath, err)
			}
			for _, want := range []string{
				"moltnet conversations",
				"moltnet read --target",
				"moltnet send --target",
			} {
				if !strings.Contains(string(content), want) {
					t.Fatalf("unknown-runtime skill content missing %q:\n%s", want, content)
				}
			}
		})
	}
}

// TestInstallMoltnetSkillGlobalWritesGlobalHomesNotWorkspace is the PLAN
// 7C.4 regression test for `skill install --global`: it must write the
// skill into every well-known global agent home (~/.claude, ~/.codex,
// ~/.agents) and must NOT write the workspace copy at all, since --global
// is a distinct destination, not an addition to the workspace default.
func TestInstallMoltnetSkillGlobalWritesGlobalHomesNotWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"skill", "install", "--global", "--workspace", workspace,
		}, "test"); err != nil {
			t.Fatalf("run() skill install --global error = %v", err)
		}
	})
	if !strings.Contains(output, "installed ") {
		t.Fatalf("unexpected skill install --global output %q", output)
	}

	for _, targetPath := range []string{
		filepath.Join(home, ".claude", "skills", "moltnet", "SKILL.md"),
		filepath.Join(home, ".codex", "skills", "moltnet", "SKILL.md"),
		filepath.Join(home, ".agents", "skills", "moltnet", "SKILL.md"),
	} {
		assertFileExists(t, targetPath)
	}

	// --workspace was given but must be ignored entirely by --global: no
	// skill file should exist anywhere under it.
	if entries, err := os.ReadDir(workspace); err != nil {
		t.Fatalf("ReadDir(workspace) error = %v", err)
	} else if len(entries) != 0 {
		t.Fatalf("workspace = %v, want no files written when --global is given", entries)
	}
}

// TestInstallMoltnetSkillGlobalIgnoresRuntime confirms --global's target set
// does not change based on --runtime: the whole point of --global is
// reaching every agent home regardless of which runtime happens to read the
// file next (see installMoltnetSkillGlobal's doc comment), so a --runtime
// that would normally narrow the workspace placement (e.g. "claude-code" to
// just ~/.claude/) must not narrow the global one.
func TestInstallMoltnetSkillGlobalIgnoresRuntime(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"skill", "install", "--global", "--runtime", "claude-code",
		}, "test"); err != nil {
			t.Fatalf("run() skill install --global --runtime claude-code error = %v", err)
		}
	})

	for _, targetPath := range []string{
		filepath.Join(home, ".claude", "skills", "moltnet", "SKILL.md"),
		filepath.Join(home, ".codex", "skills", "moltnet", "SKILL.md"),
		filepath.Join(home, ".agents", "skills", "moltnet", "SKILL.md"),
	} {
		assertFileExists(t, targetPath)
	}
}

// TestInstallMoltnetSkillDefaultWorkspaceBehaviorIsUnchanged pins the
// no-flags-at-all workspace path: --global is purely additive/opt-in, so
// its introduction must not change what a plain "skill install --workspace
// <path>" does. This exercise necessarily also exercises the --runtime
// default fix (PLAN 7C.4's second requirement): with no --runtime given at
// all, the arbitrary "openclaw" default is gone, so this lands at the
// generic .agents/skills/moltnet/SKILL.md path every runtime can read,
// mirroring TestInstallMoltnetSkillUnknownRuntimeIsAccepted's assertion
// that "no known runtime" and "unknown runtime name" behave identically.
func TestInstallMoltnetSkillDefaultWorkspaceBehaviorIsUnchanged(t *testing.T) {
	workspace := t.TempDir()

	output := captureStdout(t, func() {
		if err := run(context.Background(), []string{
			"skill", "install", "--workspace", workspace,
		}, "test"); err != nil {
			t.Fatalf("run() skill install error = %v", err)
		}
	})

	targetPath := filepath.Join(workspace, ".agents", "skills", "moltnet", "SKILL.md")
	if !strings.Contains(output, targetPath) {
		t.Fatalf("skill install output = %q, want it to name %q", output, targetPath)
	}
	assertFileExists(t, targetPath)

	// The old default's openclaw-specific path must NOT have been written.
	openclawPath := filepath.Join(workspace, "skills", "moltnet", "SKILL.md")
	if _, err := os.Stat(openclawPath); err == nil {
		t.Fatalf("openclaw path %q exists, want the arbitrary openclaw default gone", openclawPath)
	}
}

func TestMoltnetSkillContentUsesExplicitSendContract(t *testing.T) {
	content := moltnetSkillContent()

	for _, want := range []string{
		"There is no automatic reply path",
		"moltnet send --target room:research",
		"Use the local `moltnet` CLI through the `exec` tool",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("skill content missing %q", want)
		}
	}
}
