package app

import (
	"strings"
	"testing"

	"github.com/andyhtran/slacky/internal/output"
	"github.com/andyhtran/slacky/internal/skill"
)

func TestSkillsListTextChoosesVisibleGuides(t *testing.T) {
	output.SetColor(false)
	defer output.SetColor(false)

	text := skillsListText(skill.ListGuides(), false)
	for _, want := range []string{
		"Skills",
		"core",
		"setup",
		"auth",
		"Day-to-day read-only Slack search",
		"Next:",
		"slacky skills get core",
		"slacky skills get setup",
		"slacky skills get auth",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("skills list missing %q\n%s", want, text)
		}
	}
	if strings.Contains(text, "--all") || strings.Contains(text, "--full") {
		t.Fatalf("skills list should not advertise deprecated flags:\n%s", text)
	}
}

func TestSkillsListTextStylesTTYOutput(t *testing.T) {
	output.SetColor(true)
	defer output.SetColor(false)

	text := skillsListText(skill.ListGuides(), true)
	for _, want := range []string{
		"\x1b[1mSkills\x1b[0m",
		"\x1b[2mNext:\x1b[0m",
		"  \x1b[36mslacky skills get core\x1b[0m",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("styled skills list missing %q\n%s", want, text)
		}
	}
}

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
