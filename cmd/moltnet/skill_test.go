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
