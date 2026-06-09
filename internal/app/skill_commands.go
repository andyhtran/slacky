package app

import (
	"fmt"
	"strings"

	"github.com/andyhtran/slacky/internal/paths"
	"github.com/andyhtran/slacky/internal/skill"
)

type SkillCmd struct {
	Default   SkillSummaryCmd   `cmd:"" default:"noargs" hidden:""`
	Install   SkillInstallCmd   `cmd:"" help:"Install the slacky agent skill discovery stub"`
	Status    SkillStatusCmd    `cmd:"" help:"Show install state for the slacky agent skill"`
	Uninstall SkillUninstallCmd `cmd:"" help:"Remove the managed slacky agent skill discovery stub"`
	Nudge     SkillNudgeCmd     `cmd:"" help:"Toggle the install-prompt nudge"`
}

type SkillTargetFlags struct {
	Target   string `help:"Agent target (claude or codex)" name:"target"`
	Codex    bool   `help:"Shortcut for --target codex" name:"codex"`
	SkillDir string `help:"Override agent skill directory" name:"skill-dir"`
}

type (
	SkillSummaryCmd   struct{ SkillTargetFlags }
	SkillInstallCmd   struct{ SkillTargetFlags }
	SkillStatusCmd    struct{ SkillTargetFlags }
	SkillUninstallCmd struct{ SkillTargetFlags }
)

type SkillNudgeCmd struct {
	State string `arg:"" optional:"" help:"on, off, or status"`
}

type SkillsCmd struct {
	Default SkillsSummaryCmd `cmd:"" default:"noargs" hidden:""`
	List    SkillsListCmd    `cmd:"" help:"List bundled runtime skills"`
	Get     SkillsGetCmd     `cmd:"" help:"Print bundled runtime guidance"`
}

type (
	SkillsSummaryCmd struct{}
	SkillsListCmd    struct{}
)

type SkillsGetCmd struct {
	Name string `arg:"" optional:"" help:"Runtime skill name, such as core"`
	All  bool   `name:"all" help:"Print all visible runtime skills"`
	Full bool   `name:"full" help:"Include full references/templates where supported"`
}

func (cmd *SkillSummaryCmd) Run(globals *Globals) error {
	target, status, err := resolveSkillStatus(cmd.SkillTargetFlags)
	if err != nil {
		return err
	}
	text := strings.Join([]string{
		"Skill",
		"",
		fmt.Sprintf("Target: %s (%s)", target.Target, target.TargetSource),
		fmt.Sprintf("Strategy: %s", status.Strategy),
		fmt.Sprintf("State: %s", status.State),
		fmt.Sprintf("Agent skill path: %s", target.SkillPath),
		"",
		"Commands:",
		"  slacky skill install",
		"  slacky skill status",
		"  slacky skill uninstall",
		"  slacky skill install --codex",
		"",
		"Runtime guidance:",
		"  slacky skills get core",
		"  slacky skills get setup",
		"  slacky skills get auth",
	}, "\n")
	return writeEnvelope(globals, Envelope{
		OK:    true,
		Text:  text,
		Skill: status,
	})
}

func (cmd *SkillStatusCmd) Run(globals *Globals) error {
	_, status, err := resolveSkillStatus(cmd.SkillTargetFlags)
	if err != nil {
		return err
	}
	text := skillStatusText(status)
	return writeEnvelope(globals, Envelope{
		OK:    true,
		Text:  text,
		Skill: status,
	})
}

func (cmd *SkillInstallCmd) Run(globals *Globals) error {
	target, err := skill.ResolveTarget(cmd.Target, cmd.Codex, cmd.SkillDir)
	if err != nil {
		return err
	}
	status, err := skill.Install(target, appVersion)
	if err != nil {
		return appError("skill_install_refused", err.Error())
	}
	text := strings.Join([]string{
		"Skill installed",
		"",
		fmt.Sprintf("Target: %s", target.Target),
		fmt.Sprintf("Path: %s", target.SkillPath),
		fmt.Sprintf("State: %s", status.State),
		"",
		"Next:",
		"  slacky skills get core",
		"  slacky skills get setup",
		"  slacky skills get auth",
		"  slacky skill status",
	}, "\n")
	return writeEnvelope(globals, Envelope{
		OK:    true,
		Text:  text,
		Skill: status,
	})
}

func (cmd *SkillUninstallCmd) Run(globals *Globals) error {
	target, err := skill.ResolveTarget(cmd.Target, cmd.Codex, cmd.SkillDir)
	if err != nil {
		return err
	}
	status, removed, err := skill.Uninstall(target, appVersion)
	if err != nil {
		return appError("skill_uninstall_refused", err.Error())
	}
	text := strings.Join([]string{
		"Skill uninstall",
		"",
		fmt.Sprintf("Target: %s", target.Target),
		fmt.Sprintf("Path: %s", target.SkillPath),
		fmt.Sprintf("Removed: %t", removed),
		fmt.Sprintf("State: %s", status.State),
	}, "\n")
	return writeEnvelope(globals, Envelope{
		OK:    true,
		Text:  text,
		Skill: map[string]any{"status": status, "removed": removed},
	})
}

func (cmd *SkillNudgeCmd) Run(globals *Globals) error {
	pathSet, err := paths.Resolve()
	if err != nil {
		return err
	}
	state := cmd.State
	if state == "" {
		state = "status"
	}
	switch state {
	case "on":
		if err := skill.SetNudge(pathSet.Home.Path, true); err != nil {
			return err
		}
	case "off":
		if err := skill.SetNudge(pathSet.Home.Path, false); err != nil {
			return err
		}
	case "status":
	default:
		return appError("invalid_nudge_state", "--state must be one of: on, off, status")
	}
	enabled := skill.NudgeEnabled(pathSet.Home.Path)
	text := strings.Join([]string{
		"Skill nudge",
		"",
		fmt.Sprintf("Enabled: %t", enabled),
		fmt.Sprintf("State file: %s", skill.NudgePath(pathSet.Home.Path)),
	}, "\n")
	return writeEnvelope(globals, Envelope{
		OK:   true,
		Text: text,
		Skill: map[string]any{
			"nudge_enabled": enabled,
			"nudge_path":    skill.NudgePath(pathSet.Home.Path),
		},
	})
}

func (cmd *SkillsSummaryCmd) Run(globals *Globals) error {
	guides := skill.ListGuides()
	names := make([]string, 0, len(guides))
	for _, guide := range guides {
		if guide.Visible {
			names = append(names, guide.Name)
		}
	}
	text := strings.Join([]string{
		"Runtime skills",
		"",
		"Available:",
		"  " + strings.Join(names, ", "),
		"",
		"Commands:",
		"  slacky skills list",
		"  slacky skills get core",
		"  slacky skills get setup",
		"  slacky skills get auth",
		"  slacky skills get --all",
	}, "\n")
	return writeEnvelope(globals, Envelope{
		OK:     true,
		Text:   text,
		Skills: guides,
	})
}

func (cmd *SkillsListCmd) Run(globals *Globals) error {
	guides := skill.ListGuides()
	lines := []string{"Runtime skills", ""}
	for _, guide := range guides {
		if guide.Visible {
			lines = append(lines, fmt.Sprintf("- %s: %s", guide.Name, guide.Description))
		}
	}
	lines = append(lines, "", "Next:", "  slacky skills get core", "  slacky skills get setup", "  slacky skills get auth")
	return writeEnvelope(globals, Envelope{
		OK:     true,
		Text:   strings.Join(lines, "\n"),
		Skills: guides,
	})
}

func (cmd *SkillsGetCmd) Run(globals *Globals) error {
	if cmd.All {
		guides, err := skill.GetAllGuides()
		if err != nil {
			return err
		}
		text := joinGuideMarkdown(guides)
		return writeEnvelope(globals, Envelope{
			OK:     true,
			Text:   text,
			Skills: guides,
		})
	}
	if cmd.Name == "" {
		return missingUsage("missing runtime skill name", "slacky skills get <name>", "slacky skills get core", "slacky skills get --all")
	}
	guide, err := skill.GetGuide(cmd.Name)
	if err != nil {
		return appError("unknown_runtime_skill", err.Error())
	}
	return writeEnvelope(globals, Envelope{
		OK:     true,
		Text:   guide.Markdown,
		Skills: []skill.RuntimeGuide{guide},
	})
}

func resolveSkillStatus(flags SkillTargetFlags) (skill.Target, skill.InstallStatus, error) {
	target, err := skill.ResolveTarget(flags.Target, flags.Codex, flags.SkillDir)
	if err != nil {
		return skill.Target{}, skill.InstallStatus{}, err
	}
	return target, skill.Status(target, appVersion), nil
}

func skillStatusText(status skill.InstallStatus) string {
	return strings.Join([]string{
		"Skill status",
		"",
		fmt.Sprintf("Target: %s (%s)", status.Target.Target, status.TargetSource),
		fmt.Sprintf("Skill dir: %s (%s)", status.SkillDir, status.SkillDirSource),
		fmt.Sprintf("Path: %s", status.SkillPath),
		fmt.Sprintf("Strategy: %s", status.Strategy),
		fmt.Sprintf("State: %s", status.State),
		fmt.Sprintf("Managed: %t", status.Managed),
		fmt.Sprintf("Repairable: %t", status.Repairable),
		fmt.Sprintf("Message: %s", status.Message),
	}, "\n")
}

func joinGuideMarkdown(guides []skill.RuntimeGuide) string {
	parts := make([]string, 0, len(guides))
	for _, guide := range guides {
		if guide.Visible {
			parts = append(parts, guide.Markdown)
		}
	}
	return strings.Join(parts, "\n")
}
