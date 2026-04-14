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
	if args[0] == "help" {
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
		runtime   = flags.String("runtime", "openclaw", "runtime name")
		workspace = flags.String("workspace", ".", "runtime workspace path")
	)

	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("skill install does not accept positional arguments")
	}

	path, err := installMoltnetSkill(*runtime, *workspace, moltnetSkillContent())
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
	default:
		return "", fmt.Errorf("moltnet skill install does not support runtime %q", runtime)
	}

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
