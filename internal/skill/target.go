package skill

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	TargetClaude = "claude"
	TargetCodex  = "codex"

	ClaudeSkillDirEnv = "SLACKY_CLAUDE_SKILL_DIR"
	CodexSkillDirEnv  = "SLACKY_CODEX_SKILL_DIR"
)

type Target struct {
	Target         string `json:"target"`
	TargetSource   string `json:"target_source"`
	SkillDir       string `json:"skill_dir"`
	SkillDirSource string `json:"skill_dir_source"`
	SkillPath      string `json:"skill_path"`
	SkillFilePath  string `json:"skill_file_path"`
	MarkerPath     string `json:"marker_path"`
}

func ResolveTarget(target string, codex bool, skillDir string) (Target, error) {
	selected := target
	targetSource := "default"
	if codex {
		if selected != "" && selected != TargetCodex {
			return Target{}, fmt.Errorf("--codex conflicts with --target %s", selected)
		}
		selected = TargetCodex
		targetSource = "flag:--codex"
	} else if selected != "" {
		targetSource = "flag:--target"
	}
	if selected == "" {
		selected = TargetClaude
	}
	if selected != TargetClaude && selected != TargetCodex {
		return Target{}, fmt.Errorf("--target must be one of: claude, codex (got: %s)", selected)
	}

	resolvedSkillDir, skillDirSource, err := resolveSkillDir(selected, skillDir)
	if err != nil {
		return Target{}, err
	}
	skillPath := filepath.Join(resolvedSkillDir, Name)
	return Target{
		Target:         selected,
		TargetSource:   targetSource,
		SkillDir:       resolvedSkillDir,
		SkillDirSource: skillDirSource,
		SkillPath:      skillPath,
		SkillFilePath:  filepath.Join(skillPath, "SKILL.md"),
		MarkerPath:     filepath.Join(skillPath, MarkerFile),
	}, nil
}

func resolveSkillDir(target string, override string) (string, string, error) {
	if override != "" {
		path, err := absExpandHome(override)
		if err != nil {
			return "", "", err
		}
		return path, "flag:--skill-dir", nil
	}
	envName := ClaudeSkillDirEnv
	if target == TargetCodex {
		envName = CodexSkillDirEnv
	}
	if value := os.Getenv(envName); value != "" {
		path, err := absExpandHome(value)
		if err != nil {
			return "", "", err
		}
		return path, "env:" + envName, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	if target == TargetCodex {
		return filepath.Join(home, ".codex", "skills"), "default", nil
	}
	return filepath.Join(home, ".claude", "skills"), "default", nil
}

func absExpandHome(path string) (string, error) {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = home
	} else if len(path) > 2 && path[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[2:])
	}
	return filepath.Abs(path)
}
