package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type InstallStatus struct {
	Target
	Strategy     string `json:"strategy"`
	State        string `json:"state"`
	Installed    bool   `json:"installed"`
	Managed      bool   `json:"managed"`
	Repairable   bool   `json:"repairable"`
	ExpectedHash string `json:"expected_hash"`
	CurrentHash  string `json:"current_hash,omitempty"`
	Marker       string `json:"marker,omitempty"`
	Message      string `json:"message,omitempty"`
}

func Status(target Target, version string) InstallStatus {
	status := InstallStatus{
		Target:       target,
		Strategy:     "managed-stub",
		State:        "missing",
		Repairable:   true,
		ExpectedHash: StubHash(version),
	}

	info, err := os.Lstat(target.SkillPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			status.Message = "skill is not installed"
			return status
		}
		status.State = "error"
		status.Repairable = false
		status.Message = err.Error()
		return status
	}

	if info.Mode()&os.ModeSymlink != 0 {
		status.State = "foreign_symlink"
		status.Message = "refusing to manage symlink at agent skill path"
		status.Repairable = false
		return status
	}
	if !info.IsDir() {
		status.State = "foreign_file"
		status.Message = "refusing to manage non-directory at agent skill path"
		status.Repairable = false
		return status
	}

	markerData, err := os.ReadFile(target.MarkerPath)
	if err != nil {
		status.State = "foreign_directory"
		status.Message = "directory is not marked as managed by slacky"
		status.Repairable = false
		return status
	}

	status.Installed = true
	status.Managed = true
	status.Marker = strings.TrimSpace(string(markerData))
	data, err := os.ReadFile(target.SkillFilePath)
	if err != nil {
		status.State = "stale_managed_stub"
		status.Message = "managed stub is missing SKILL.md"
		return status
	}
	status.CurrentHash = hashString(string(data))
	if status.CurrentHash != status.ExpectedHash {
		status.State = "stale_managed_stub"
		status.Message = "managed stub differs from the current binary"
		return status
	}
	status.State = "managed_stub"
	status.Message = "managed stub is installed"
	return status
}

func Install(target Target, version string) (InstallStatus, error) {
	status := Status(target, version)
	if !status.Repairable {
		return status, fmt.Errorf("%s: %s", status.State, status.Message)
	}
	if err := os.MkdirAll(target.SkillDir, 0o700); err != nil {
		return status, err
	}
	if err := os.RemoveAll(target.SkillPath); err != nil {
		return status, err
	}
	if err := os.MkdirAll(target.SkillPath, 0o700); err != nil {
		return status, err
	}
	if err := os.WriteFile(target.SkillFilePath, []byte(StubMarkdown(version)), 0o600); err != nil {
		return status, err
	}
	marker := fmt.Sprintf("strategy=managed-stub\nname=%s\nhash=%s\n", Name, StubHash(version))
	if err := os.WriteFile(target.MarkerPath, []byte(marker), 0o600); err != nil {
		return status, err
	}
	return Status(target, version), nil
}

func Uninstall(target Target, version string) (InstallStatus, bool, error) {
	status := Status(target, version)
	if status.State == "missing" {
		return status, false, nil
	}
	if !status.Managed {
		return status, false, fmt.Errorf("%s: %s", status.State, status.Message)
	}
	if err := os.RemoveAll(target.SkillPath); err != nil {
		return status, false, err
	}
	return Status(target, version), true, nil
}

func NudgePath(home string) string {
	return filepath.Join(home, "state", "skill-nudge-disabled")
}

func NudgeEnabled(home string) bool {
	_, err := os.Stat(NudgePath(home))
	return errors.Is(err, os.ErrNotExist)
}

func SetNudge(home string, enabled bool) error {
	path := NudgePath(home)
	if enabled {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("disabled\n"), 0o600)
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
