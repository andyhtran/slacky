package app

import (
	"strings"
	"testing"

	"github.com/andyhtran/slacky/internal/skill"
)

func TestSkillStatusTextPrintsTargetName(t *testing.T) {
	text := skillStatusText(skill.InstallStatus{
		Target: skill.Target{
			Target:         "claude",
			TargetSource:   "default",
			SkillDir:       "/tmp/skills",
			SkillDirSource: "flag",
			SkillPath:      "/tmp/skills/slacky",
		},
		Strategy:   "managed-stub",
		State:      "managed_stub",
		Managed:    true,
		Repairable: true,
		Message:    "managed stub is installed",
	})
	if !strings.Contains(text, "Target: claude (default)") {
		t.Fatalf("skill status should print target name:\n%s", text)
	}
	if strings.Contains(text, "Target: {") {
		t.Fatalf("skill status should not print target struct:\n%s", text)
	}
}
