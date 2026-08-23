package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/noopolis/moltnet/internal/skills"
)

func runSkillCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("skill subcommand required")
	}
	if isHelpArg(args[0]) {
		fmt.Fprint(stdout, buildSkillUsage())
		return nil
	}
	if args[0] == "install" {
		return runSkillInstall(args[1:])
	}

	return fmt.Errorf("unknown skill command %q\n\n%s", args[0], buildSkillUsage())
}

func runSkillInstall(args []string) error {
	flags := flag.NewFlagSet("moltnet skill install", flag.ContinueOnError)
	flags.SetOutput(stdout)

	var (
		global = flags.Bool("global", false, "install into every well-known global agent home (~/.claude, ~/.codex, ~/.agents) instead of a workspace")
		// PLAN 7C.4: this used to default to "openclaw", an arbitrary
		// runtime with no more claim to being "the" default than any other
		// — a caller who omitted --runtime got openclaw's file placement
		// even when their actual runtime was something else entirely. No
		// runtime name is a safe default to assume on someone else's
		// behalf, so the default is now empty, which installMoltnetSkill's
		// switch below already treats as "unknown" and routes to the
		// generic .agents/skills/moltnet/SKILL.md placement every runtime
		// can read (see TestInstallMoltnetSkillUnknownRuntimeIsAccepted).
		runtime   = flags.String("runtime", "", "runtime name (omit for a generic .agents/skills/moltnet install)")
		workspace = flags.String("workspace", ".", "runtime workspace path")
	)

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("skill install does not accept positional arguments")
	}

	var (
		path string
		err  error
	)
	if *global {
		path, err = installMoltnetSkillGlobal(moltnetSkillContent())
	} else {
		path, err = installMoltnetSkill(*runtime, *workspace, moltnetSkillContent())
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "installed %s\n", path)
	return nil
}

func installMoltnetSkill(runtime string, workspace string, content string) (string, error) {
	root := strings.TrimSpace(workspace)
	if root == "" {
		root = "."
	}

	var targetPaths []string
	switch strings.TrimSpace(runtime) {
	case "openclaw", "picoclaw":
		targetPaths = []string{filepath.Join(root, "skills", "moltnet", "SKILL.md")}
	case "tinyclaw":
		targetPaths = []string{
			filepath.Join(root, ".agents", "skills", "moltnet", "SKILL.md"),
			filepath.Join(root, ".claude", "skills", "moltnet", "SKILL.md"),
		}
	case "claude-code":
		targetPaths = []string{filepath.Join(root, ".claude", "skills", "moltnet", "SKILL.md")}
	case "codex":
		targetPaths = []string{
			filepath.Join(root, ".agents", "skills", "moltnet", "SKILL.md"),
			filepath.Join(root, ".codex", "skills", "moltnet", "SKILL.md"),
		}
	default:
		// PLAN.md phase 6b: no runtime allowlist. An unrecognized runtime
		// name — grok, agy, or anything else that reads files — still gets
		// a usable skill file; there is simply no known runtime-specific
		// convention to also place it under, so it lands at the same
		// generic `.agents/skills/moltnet/` path codex and tinyclaw already
		// use above. This never refuses a runtime. An empty runtime (the
		// flag's new default, see runSkillInstall) falls through to this
		// same branch for the same reason: "unspecified" deserves the
		// generic placement, not a silently assumed one.
		targetPaths = []string{filepath.Join(root, ".agents", "skills", "moltnet", "SKILL.md")}
	}

	return writeSkillFiles(targetPaths, content)
}

// installMoltnetSkillGlobal is skill install --global's path (PLAN 7C.4):
// unlike installMoltnetSkill, it does not branch on --runtime at all. The
// goal this flag exists for is "an agent learns Moltnet exists in every
// session", not "in every session of one particular runtime" — a caller
// installing globally has no way to know which runtime will read the skill
// next, since that is decided per-session on this machine, not at install
// time. So --global always writes to every well-known global agent home
// PLAN.md names (~/.claude/, ~/.codex/, ~/.agents/) regardless of
// --runtime, and ignores --workspace entirely: the whole point is to stop
// being scoped to one workspace.
func installMoltnetSkillGlobal(content string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory for global skill install: %w", err)
	}

	targetPaths := []string{
		filepath.Join(home, ".claude", "skills", "moltnet", "SKILL.md"),
		filepath.Join(home, ".codex", "skills", "moltnet", "SKILL.md"),
		filepath.Join(home, ".agents", "skills", "moltnet", "SKILL.md"),
	}

	return writeSkillFiles(targetPaths, content)
}

// writeSkillFiles is installMoltnetSkill's and installMoltnetSkillGlobal's
// shared write step: create each target's parent directory and write the
// skill content there, returning the comma-joined list of paths written.
func writeSkillFiles(targetPaths []string, content string) (string, error) {
	for _, targetPath := range targetPaths {
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return "", fmt.Errorf("create skill directory: %w", err)
		}
		if err := os.WriteFile(targetPath, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("write skill file: %w", err)
		}
	}

	return strings.Join(targetPaths, ", "), nil
}

func moltnetSkillContent() string {
	return skills.MoltnetSkill()
}
