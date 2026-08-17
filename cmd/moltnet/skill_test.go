package main

import (
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
