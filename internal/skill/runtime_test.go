package skill

import (
	"strings"
	"testing"
)

func TestListGuidesIncludesCoreAndSetup(t *testing.T) {
	guides := ListGuides()
	names := make([]string, 0, len(guides))
	for _, guide := range guides {
		if guide.Visible {
			names = append(names, guide.Name)
		}
	}
	got := strings.Join(names, ",")
	want := CoreGuide + "," + SetupGuide
	if got != want {
		t.Fatalf("visible guide names = %q, want %q", got, want)
	}
}

func TestGetSetupGuide(t *testing.T) {
	guide, err := GetGuide(SetupGuide)
	if err != nil {
		t.Fatalf("GetGuide(%q): %v", SetupGuide, err)
	}
	if guide.Name != SetupGuide {
		t.Fatalf("guide name = %q", guide.Name)
	}
	for _, want := range []string{
		"# Slacky Setup",
		"slacky setup wizard",
		"slacky setup steps",
		"slacky auth import",
		"slacky auth login --client-id <client-id>",
		"slacky skills get core",
	} {
		if !strings.Contains(guide.Markdown, want) {
			t.Fatalf("setup guide missing %q\n%s", want, guide.Markdown)
		}
	}
}

func TestUnknownGuideErrorListsValidGuides(t *testing.T) {
	_, err := GetGuide("missing")
	if err == nil {
		t.Fatal("expected unknown guide error")
	}
	message := err.Error()
	for _, want := range []string{CoreGuide, SetupGuide} {
		if !strings.Contains(message, want) {
			t.Fatalf("unknown guide error should mention %q: %s", want, message)
		}
	}
}

func TestStubMarkdownRoutesSetupAndCore(t *testing.T) {
	markdown := StubMarkdown("test")
	for _, want := range []string{
		"slacky skills get core",
		"slacky skills get setup",
		"setting up",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("stub markdown missing %q\n%s", want, markdown)
		}
	}
}
