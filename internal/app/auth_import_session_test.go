package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowserSessionWizardTextIncludesManualTokenAndCookieSteps(t *testing.T) {
	text := browserSessionWizardText("work-browser")
	for _, want := range []string{
		"Browser session import wizard",
		"Profile: work-browser",
		"Step 1: copy XOXC_TOKEN",
		"allow pasting",
		browserSessionTokenSnippet,
		"Step 2: copy XOXD_TOKEN",
		"cookie named d",
		"hidden terminal input",
		"slacky auth import-session --wizard --name work-browser",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("wizard text missing %q\n%s", want, text)
		}
	}
}

func TestAuthImportSessionHeadlessPrintsGuideWithoutWritingAuth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SLACKY_HOME", home)
	authPath := filepath.Join(home, "slack.json")

	text := captureStdout(t, func() {
		cmd := AuthImportSessionCmd{Headless: true, Name: "work-browser"}
		if err := cmd.Run(&Globals{}); err != nil {
			t.Fatalf("headless import-session wizard: %v", err)
		}
	})
	for _, want := range []string{
		"Browser session import wizard",
		"Step 1: copy XOXC_TOKEN",
		"Step 2: copy XOXD_TOKEN",
		"slacky auth import-session --wizard --name work-browser",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("headless output missing %q\n%s", want, text)
		}
	}
	if _, err := os.Stat(authPath); !os.IsNotExist(err) {
		t.Fatalf("headless wizard should not write auth, stat err = %v", err)
	}
}
