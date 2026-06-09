package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	bundledskills "github.com/andyhtran/slacky/skills"
)

const (
	Name       = "slacky"
	CoreGuide  = "core"
	SetupGuide = "setup"
	MarkerFile = ".slacky-managed-skill"
)

type RuntimeGuide struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Markdown    string `json:"markdown,omitempty"`
	Visible     bool   `json:"visible"`
}

func ListGuides() []RuntimeGuide {
	return []RuntimeGuide{
		{
			Name:        CoreGuide,
			Description: "Day-to-day read-only Slack search, context, and compact agent output guidance.",
			Visible:     true,
		},
		{
			Name:        SetupGuide,
			Description: "First-run install, Slack app setup, token import, auth verification, and handoff guidance.",
			Visible:     true,
		},
	}
}

func GetGuide(name string) (RuntimeGuide, error) {
	guides := ListGuides()
	for index := range guides {
		if guides[index].Name != name {
			continue
		}
		markdown, err := guideMarkdown(name)
		if err != nil {
			return RuntimeGuide{}, err
		}
		guides[index].Markdown = markdown
		return guides[index], nil
	}
	return RuntimeGuide{}, fmt.Errorf("unknown runtime skill %q; valid skills: %s", name, validGuideNames())
}

func GetAllGuides() ([]RuntimeGuide, error) {
	guides := ListGuides()
	for index := range guides {
		full, err := GetGuide(guides[index].Name)
		if err != nil {
			return nil, err
		}
		guides[index] = full
	}
	return guides, nil
}

func StubMarkdown(version string) string {
	return fmt.Sprintf(`---
name: slacky
description: Use when searching Slack and getting read-only context with the slacky CLI.
---

# Slacky

This managed discovery stub is installed by slacky %s.

Run version-matched runtime guidance from the installed binary:

%s

Use setup guidance for first-run installs, Slack app creation, token import, or auth troubleshooting:

%s

Safety:
- Slacky is read-only against Slack.
- Start search tasks directly with the runtime guide; use auth status or doctor only when a command reports an issue.
- Use slacky skills get <name> --json only when structured skill output is needed; the default is Markdown.
`, version, "```sh\nslacky skills get core\n```", "```sh\nslacky skills get setup\n```")
}

func StubHash(version string) string {
	sum := sha256.Sum256([]byte(StubMarkdown(version)))
	return hex.EncodeToString(sum[:])
}

func RuntimeHash() (string, error) {
	var paths []string
	if err := fs.WalkDir(bundledskills.FS, bundledskills.Root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		paths = append(paths, path)
		return nil
	}); err != nil {
		return "", err
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, path := range paths {
		data, err := bundledskills.FS.ReadFile(path)
		if err != nil {
			return "", err
		}
		hash.Write([]byte(path))
		hash.Write([]byte{0})
		hash.Write(data)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func validGuideNames() string {
	guides := ListGuides()
	names := make([]string, 0, len(guides))
	for _, guide := range guides {
		if guide.Visible {
			names = append(names, guide.Name)
		}
	}
	return strings.Join(names, ", ")
}

func guideMarkdown(name string) (string, error) {
	fileName := "SKILL.md"
	if name == SetupGuide {
		fileName = "setup.md"
	}
	data, err := bundledskills.FS.ReadFile(bundledskills.Root + "/" + fileName)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(data), "\n") + "\n", nil
}
