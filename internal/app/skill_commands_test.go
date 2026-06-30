package app

import (
	"encoding/json"
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
		"NAME",
		"DESCRIPTION",
		"----",
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
		"\x1b[36m  slacky skills get core\x1b[0m",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("styled skills list missing %q\n%s", want, text)
		}
	}
}

func TestSkillsListTextUsesBoundedTable(t *testing.T) {
	guides := []skill.RuntimeGuide{
		{Name: "core", Description: strings.Repeat("long runtime guidance description ", 8), Visible: true},
		{Name: "setup", Description: "Setup guidance.", Visible: true},
	}
	text := ""
	for _, width := range []int{80, 100} {
		text = skillsListTextWithWidth(guides, false, width)
		for _, line := range strings.Split(text, "\n") {
			if got := output.VisibleWidth(line); got > width {
				t.Fatalf("skills list line width = %d, want <= %d\n%s", got, width, line)
			}
		}
	}
	if strings.Contains(text, "TYPE") || strings.Contains(text, "GUIDE") {
		t.Fatalf("skills list should only use NAME and DESCRIPTION columns:\n%s", text)
	}
	if !strings.Contains(text, "...") {
		t.Fatalf("skills list should truncate long descriptions:\n%s", text)
	}
}

func TestSkillsSummaryMatchesSkillsList(t *testing.T) {
	output.SetColor(false)
	defer output.SetColor(false)

	summaryText := captureStdout(t, func() {
		if err := (&SkillsSummaryCmd{}).Run(&Globals{}); err != nil {
			t.Fatalf("skills summary: %v", err)
		}
	})
	listText := captureStdout(t, func() {
		if err := (&SkillsListCmd{}).Run(&Globals{}); err != nil {
			t.Fatalf("skills list: %v", err)
		}
	})
	if summaryText != listText {
		t.Fatalf("skills and skills list should match\nsummary:\n%s\nlist:\n%s", summaryText, listText)
	}
}

func TestSkillsListJSONOmitsMarkdownAndANSI(t *testing.T) {
	output.SetColor(true)
	defer output.SetColor(false)

	text := captureStdout(t, func() {
		if err := (&SkillsListCmd{}).Run(&Globals{JSON: true}); err != nil {
			t.Fatalf("skills list json: %v", err)
		}
	})
	if !strings.HasPrefix(text, "{") {
		t.Fatalf("skills list JSON should start at byte 1, got %q", text[:min(len(text), 20)])
	}
	if strings.Contains(text, "\x1b[") {
		t.Fatalf("skills list JSON should not contain ANSI:\n%s", text)
	}
	var envelope struct {
		Skills []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Markdown    string `json:"markdown"`
		} `json:"skills"`
	}
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		t.Fatalf("decode skills list JSON: %v\n%s", err, text)
	}
	if len(envelope.Skills) == 0 {
		t.Fatalf("expected visible skills in JSON")
	}
	for _, guide := range envelope.Skills {
		if guide.Name == "" || guide.Description == "" {
			t.Fatalf("guide should include name and description: %#v", guide)
		}
		if guide.Markdown != "" {
			t.Fatalf("skills list JSON should not include Markdown bodies: %#v", guide)
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
