package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyhtran/slacky/internal/paths"
)

func TestRenderManifestUsesPKCEUserOnlyShape(t *testing.T) {
	body, err := renderManifest(newManifest("Slacky", defaultRedirectURI), "json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal([]byte(body), &manifest); err != nil {
		t.Fatal(err)
	}
	if _, ok := manifest["features"]; ok {
		t.Fatalf("user-only PKCE manifest should not include bot/app-home features: %v", manifest["features"])
	}
	display, ok := manifest["display_information"].(map[string]any)
	if !ok {
		t.Fatalf("display_information missing or wrong type: %v", manifest["display_information"])
	}
	if display["description"] != setupShortDescription {
		t.Fatalf("description = %v", display["description"])
	}
	got, ok := display["long_description"].(string)
	if !ok {
		t.Fatalf("long_description missing or wrong type: %v", display["long_description"])
	}
	if len(got) < 174 {
		t.Fatalf("long_description too short: %d %q", len(got), got)
	}
	oauth, ok := manifest["oauth_config"].(map[string]any)
	if !ok {
		t.Fatalf("oauth_config missing or wrong type: %v", manifest["oauth_config"])
	}
	if oauth["pkce_enabled"] != true {
		t.Fatalf("pkce_enabled = %v", oauth["pkce_enabled"])
	}
	scopes, ok := oauth["scopes"].(map[string]any)
	if !ok {
		t.Fatalf("scopes missing or wrong type: %v", oauth["scopes"])
	}
	if _, ok := scopes["bot"]; ok {
		t.Fatalf("user-only PKCE manifest should not include bot scopes: %v", scopes["bot"])
	}
	userScopes, ok := scopes["user"].([]any)
	if !ok {
		t.Fatalf("user scopes missing or wrong type: %v", scopes["user"])
	}
	for _, want := range []string{"search:read", "channels:read", "users:read"} {
		if !containsAny(userScopes, want) {
			t.Fatalf("user scopes missing %s: %v", want, userScopes)
		}
	}
	settings, ok := manifest["settings"].(map[string]any)
	if !ok {
		t.Fatalf("settings missing or wrong type: %v", manifest["settings"])
	}
	if settings["token_rotation_enabled"] != false {
		t.Fatalf("token_rotation_enabled = %v", settings["token_rotation_enabled"])
	}
	if _, ok := settings["incoming_webhooks"]; ok {
		t.Fatalf("minimal read-only manifest should not include incoming_webhooks: %v", settings["incoming_webhooks"])
	}
}

func TestSetupStepsTextPrintsManifestBeforeSteps(t *testing.T) {
	manifest := "{\n  \"display_information\": {\n    \"name\": \"Slacky\"\n  }\n}\n"
	text := setupStepsText("Slacky", "json", manifest, false)

	manifestIndex := strings.Index(text, "Manifest JSON (copy/paste this into Slack):\n"+strings.TrimRight(manifest, "\n"))
	stepsIndex := strings.Index(text, "Setup steps:\n1. Open https://api.slack.com/apps")
	if manifestIndex == -1 {
		t.Fatalf("manifest not printed:\n%s", text)
	}
	if stepsIndex == -1 {
		t.Fatalf("setup steps not printed:\n%s", text)
	}
	if manifestIndex > stepsIndex {
		t.Fatalf("manifest should be printed before setup steps:\n%s", text)
	}
	for _, want := range []string{
		"2. Click Create New App -> From a manifest",
		"4. Paste the manifest shown above",
		"6. Run slacky auth login --client-id <client-id>",
		"slacky auth import",
		"slacky auth status",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("setup steps missing %q\n%s", want, text)
		}
	}
}

func TestSetupStepsTextMentionsClipboardWhenCopied(t *testing.T) {
	text := setupStepsText("Slacky", "json", "{}", true)
	if !strings.Contains(text, "Copied manifest to clipboard.") {
		t.Fatalf("copied setup steps should mention clipboard:\n%s", text)
	}
}

func TestSetupWizardTextIncludesManifestIconAndSteps(t *testing.T) {
	manifest := "{\n  \"display_information\": {\n    \"name\": \"Slacky\"\n  }\n}\n"
	plan := setupWizardPlanFor(
		"Slacky",
		defaultRedirectURI,
		"json",
		manifest,
		"/tmp/slacky/slacky-read-only.json",
		setupIconFiles{
			FileName:  "slacky-icon.png",
			Directory: "/tmp/slacky",
			PNGPath:   "/tmp/slacky/slacky-icon.png",
		},
	)
	text := setupWizardText(plan)

	manifestIndex := strings.Index(text, "Manifest JSON (copy/paste this into Slack):\n"+strings.TrimRight(manifest, "\n"))
	stepsIndex := strings.Index(text, "Setup steps:\n1. Open https://api.slack.com/apps")
	if manifestIndex == -1 {
		t.Fatalf("manifest not printed:\n%s", text)
	}
	if stepsIndex == -1 {
		t.Fatalf("setup steps not printed:\n%s", text)
	}
	if manifestIndex > stepsIndex {
		t.Fatalf("manifest should be printed before setup steps:\n%s", text)
	}
	for _, want := range []string{
		"Optional app icon:\n  File name: slacky-icon.png",
		"press Cmd+Shift+G and paste this folder:\n    /tmp/slacky",
		"Full path: /tmp/slacky/slacky-icon.png",
		"5. Create the app; optionally upload the icon PNG shown above",
		"6. slacky will start a localhost OAuth callback",
		"slacky auth import",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("wizard output missing %q\n%s", want, text)
		}
	}
	if strings.Contains(text, "File URL:") {
		t.Fatalf("file URL should not be printed in human wizard output:\n%s", text)
	}
}

func TestWriteSetupIconFilesWritesSlackyPNG(t *testing.T) {
	home := t.TempDir()
	pathSet := paths.Set{
		CacheDB: paths.Entry{Path: filepath.Join(home, "cache", "index.db")},
	}
	icon, err := writeSetupIconFiles(pathSet)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(icon.PNGPath); err != nil {
		t.Fatalf("icon asset missing %s: %v", icon.PNGPath, err)
	}
	rel, err := filepath.Rel(home, icon.PNGPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("icon path should be under temp home: %v", err)
	}
	if icon.FileName != "slacky-icon.png" || !strings.HasSuffix(icon.Directory, filepath.Join("cache", "manifests")) {
		t.Fatalf("unexpected icon location: %+v", icon)
	}
	if !strings.HasPrefix(icon.PNGFileURL, "file://") || !strings.HasSuffix(icon.PNGFileURL, "slacky-icon.png") {
		t.Fatalf("unexpected icon file URL: %s", icon.PNGFileURL)
	}
}

func containsAny(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
