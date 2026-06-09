package app

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/andyhtran/slacky/internal/api"
	"github.com/andyhtran/slacky/internal/output"
	"github.com/andyhtran/slacky/internal/paths"
	"github.com/andyhtran/slacky/internal/setupassets"
)

const (
	setupShortDescription = "Read-only Slack search for fast context, built for people and agents."
	setupLongDescription  = "Slacky helps people and agents search messages, open the right thread, resolve people and channels, and gather surrounding Slack context for fast handoff without changing workspace data."
	setupBackgroundColor  = "#4A154B"
)

type SetupCmd struct {
	Default  SetupSummaryCmd  `cmd:"" default:"noargs" hidden:""`
	Wizard   SetupWizardCmd   `cmd:"" help:"Guided Slack app setup wizard"`
	Manifest SetupManifestCmd `cmd:"" help:"Generate a read-only Slack app manifest"`
	Steps    SetupStepsCmd    `cmd:"" help:"Show read-only Slack app setup steps"`
}

type SetupSummaryCmd struct{}

type SetupStepsCmd struct {
	AppName     string `help:"Slack app display name" default:"Slacky" name:"app-name"`
	RedirectURI string `help:"Primary OAuth redirect URI" default:"http://127.0.0.1:8888/callback" name:"redirect-uri"`
	Format      string `help:"Manifest output format" enum:"json,yaml" default:"json" name:"format"`
	Copy        bool   `help:"Copy manifest to clipboard on macOS" name:"copy"`
}

type SetupWizardCmd struct {
	AppName      string `help:"Slack app display name" default:"Slacky" name:"app-name"`
	RedirectURI  string `help:"Primary OAuth redirect URI" default:"http://127.0.0.1:8888/callback" name:"redirect-uri"`
	Format       string `help:"Manifest output format" enum:"json,yaml" default:"json" name:"format"`
	Copy         bool   `help:"Copy manifest to clipboard on macOS" name:"copy"`
	Headless     bool   `help:"Print setup plan and do not open, prompt, or run OAuth" name:"headless"`
	Yes          bool   `help:"Skip pause prompts where possible" short:"y" name:"yes"`
	NoOpen       bool   `help:"Do not open Slack app setup or OAuth URLs" name:"no-open"`
	ClientID     string `help:"Slack app client ID" name:"client-id"`
	ClientSecret string `help:"Optional Slack app client secret" name:"client-secret"`
	NoLogin      bool   `help:"Do not start OAuth login after the setup walkthrough" name:"no-login"`
}

type SetupManifestCmd struct {
	AppName     string `help:"Slack app display name" default:"Slacky" name:"app-name"`
	RedirectURI string `help:"Primary OAuth redirect URI" default:"http://127.0.0.1:8888/callback" name:"redirect-uri"`
	Format      string `help:"Manifest output format" enum:"json,yaml" default:"yaml" name:"format"`
	Output      string `help:"Write manifest to this path" name:"output"`
	Copy        bool   `help:"Copy manifest to clipboard on macOS" name:"copy"`
}

type slackManifest struct {
	Metadata           map[string]int      `json:"_metadata"`
	DisplayInformation map[string]string   `json:"display_information"`
	Features           map[string]any      `json:"features,omitempty"`
	OAuthConfig        manifestOAuthConfig `json:"oauth_config"`
	Settings           map[string]any      `json:"settings"`
}

type manifestOAuthConfig struct {
	RedirectURLs []string            `json:"redirect_urls"`
	Scopes       map[string][]string `json:"scopes"`
	PKCEEnabled  bool                `json:"pkce_enabled"`
}

type setupIconFiles struct {
	FileName   string `json:"file_name"`
	Directory  string `json:"directory"`
	PNGPath    string `json:"png_path"`
	PNGFileURL string `json:"png_file_url"`
}

type setupWizardPlan struct {
	AppName       string            `json:"app_name"`
	RedirectURI   string            `json:"redirect_uri"`
	Format        string            `json:"format"`
	Manifest      string            `json:"manifest"`
	ManifestPath  string            `json:"manifest_path"`
	Icon          setupIconFiles    `json:"icon"`
	OpenedAppPage bool              `json:"opened_app_page"`
	Commands      map[string]string `json:"commands"`
	NextSteps     []string          `json:"next_steps"`
}

func (cmd *SetupSummaryCmd) Run(globals *Globals) error {
	text := strings.Join([]string{
		"Setup",
		"",
		"Choose an auth path.",
		"",
		"From scratch:",
		"  slacky setup wizard",
		"  slacky setup steps",
		"",
		"Existing user token:",
		"  slacky auth import",
		"",
		"Browser session fallback:",
		"  slacky auth import-session --wizard --name work-browser",
		"",
		"Switch user or retest auth:",
		"  slacky auth logout",
		"",
		"Utilities:",
		"  slacky setup manifest",
		"  slacky auth login",
	}, "\n")
	return writeEnvelope(globals, Envelope{
		OK:   true,
		Text: text,
		Commands: []string{
			"slacky setup wizard",
			"slacky setup steps",
			"slacky auth import",
			"slacky auth import-session --wizard --name work-browser",
			"slacky auth logout",
			"slacky setup manifest",
			"slacky auth login",
		},
	})
}

func (cmd *SetupWizardCmd) Run(globals *Globals) error {
	pathSet, err := paths.Resolve()
	if err != nil {
		return err
	}
	manifest := newManifest(cmd.AppName, cmd.RedirectURI)
	rendered, err := renderManifest(manifest, cmd.Format)
	if err != nil {
		return err
	}
	manifestPath, err := writeSetupManifestFile(pathSet, cmd.Format, rendered)
	if err != nil {
		return err
	}
	iconFiles, err := writeSetupIconFiles(pathSet)
	if err != nil {
		return err
	}
	plan := setupWizardPlanFor(cmd.AppName, cmd.RedirectURI, cmd.Format, rendered, manifestPath, iconFiles)
	text := setupWizardText(plan)

	if cmd.Headless || globals.JSON {
		return writeEnvelope(globals, Envelope{
			OK:      true,
			Text:    text,
			Results: plan,
		})
	}
	if cmd.Copy {
		if err := copyToClipboard(rendered); err != nil {
			fmt.Fprintf(os.Stderr, "warning: clipboard copy failed: %v\n", err)
		} else {
			fmt.Fprintln(os.Stderr, "Copied manifest to clipboard.")
		}
	}
	if err := output.WriteText(os.Stdout, text); err != nil {
		return err
	}
	if !cmd.NoOpen {
		if err := openBrowser("https://api.slack.com/apps"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not open Slack app page: %v\n", err)
		} else {
			plan.OpenedAppPage = true
		}
	}
	if cmd.NoLogin {
		if _, err := fmt.Fprintln(os.Stdout); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(os.Stdout, "OAuth skipped. Run this when ready:"); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(os.Stdout, "  slacky auth login --client-id <client-id>"); err != nil {
			return err
		}
		return nil
	}

	reader := bufio.NewReader(os.Stdin)
	if !cmd.Yes {
		if err := waitForEnter(reader, "Press Enter after you create the app from the manifest and can see Basic Information..."); err != nil {
			return err
		}
	}
	clientID := strings.TrimSpace(cmd.ClientID)
	if clientID == "" {
		clientID, err = promptRequired(reader, "Client ID")
		if err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(os.Stdout); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(os.Stdout, "Starting local OAuth flow..."); err != nil {
		return err
	}
	return (&AuthLoginCmd{
		ClientID:     clientID,
		ClientSecret: cmd.ClientSecret,
		RedirectURI:  cmd.RedirectURI,
		NoOpen:       cmd.NoOpen,
	}).Run(globals)
}

func (cmd *SetupStepsCmd) Run(globals *Globals) error {
	manifest := newManifest(cmd.AppName, cmd.RedirectURI)
	rendered, err := renderManifest(manifest, cmd.Format)
	if err != nil {
		return err
	}
	copied := false
	if cmd.Copy {
		if err := copyToClipboard(rendered); err != nil {
			return err
		}
		copied = true
	}
	text := setupStepsText(cmd.AppName, cmd.Format, rendered, copied)
	return writeEnvelope(globals, Envelope{
		OK:   true,
		Text: text,
		Results: map[string]any{
			"app_name":          cmd.AppName,
			"redirect_uri":      cmd.RedirectURI,
			"format":            cmd.Format,
			"manifest":          manifest,
			"rendered_manifest": rendered,
			"copied":            copied,
			"scopes":            api.UserScopes,
			"commands": []string{
				"slacky setup steps",
				"slacky auth login --client-id <client-id>",
				"slacky auth import",
				"slacky auth status",
			},
		},
	})
}

func setupStepsText(appName, format, manifest string, copied bool) string {
	format = strings.ToUpper(firstNonEmpty(format, "json"))
	lines := []string{
		"slacky setup steps",
		"",
		fmt.Sprintf("Manifest %s (copy/paste this into Slack):", format),
		strings.TrimRight(manifest, "\n"),
		"",
		"Setup steps:",
		"1. Open https://api.slack.com/apps",
		"2. Click Create New App -> From a manifest",
		"3. Pick the workspace to develop the app in",
		"4. Paste the manifest shown above",
		fmt.Sprintf("5. Create the %q app, then copy Client ID from Basic Information", appName),
		"6. Run slacky auth login --client-id <client-id>",
		"",
		"Alternative if you already have a scoped user token:",
		"  slacky auth import",
		"",
		"Verify:",
		"  slacky auth status",
	}
	if copied {
		lines = append([]string{lines[0], "", "Copied manifest to clipboard."}, lines[2:]...)
	}
	return strings.Join(lines, "\n")
}

func setupWizardPlanFor(appName, redirectURI, format, manifest, manifestPath string, icon setupIconFiles) setupWizardPlan {
	format = strings.ToLower(firstNonEmpty(format, "json"))
	return setupWizardPlan{
		AppName:      appName,
		RedirectURI:  redirectURI,
		Format:       format,
		Manifest:     manifest,
		ManifestPath: manifestPath,
		Icon:         icon,
		Commands: map[string]string{
			"print_manifest": fmt.Sprintf("slacky setup manifest --format %s --app-name %s", format, shellQuote(appName)),
			"wizard":         "slacky setup wizard",
			"oauth_login":    "slacky auth login --client-id <client-id>",
			"token_import":   "slacky auth import",
			"verify":         "slacky auth status",
		},
		NextSteps: []string{
			"Open https://api.slack.com/apps",
			"Click Create New App, then From a manifest",
			"Pick the workspace to develop the app in",
			"Paste the manifest and create the app",
			"Optionally upload the app icon in Display Information",
			"Run OAuth login with the copied Client ID",
		},
	}
}

func setupWizardText(plan setupWizardPlan) string {
	format := strings.ToUpper(firstNonEmpty(plan.Format, "json"))
	lines := []string{
		output.Bold("slacky setup wizard"),
		"",
		output.Bold(fmt.Sprintf("Manifest %s (copy/paste this into Slack):", format)),
		strings.TrimRight(plan.Manifest, "\n"),
		"",
	}
	if plan.Icon.FileName != "" {
		lines = append(
			lines,
			output.Bold("Optional app icon:"),
			fmt.Sprintf("  File name: %s", plan.Icon.FileName),
			"  In Slack's file picker, search for that filename in the top-right Search field.",
			"  If search does not find it, press Cmd+Shift+G and paste this folder:",
			fmt.Sprintf("    %s", plan.Icon.Directory),
			fmt.Sprintf("  Full path: %s", plan.Icon.PNGPath),
			"  Slack app manifests cannot include icon uploads; add this manually in Display Information after creating the app.",
			"",
		)
	}
	lines = append(
		lines,
		output.Bold("Setup steps:"),
		setupStepLine(1, "Open https://api.slack.com/apps"),
		setupStepLine(2, "Click Create New App -> From a manifest"),
		setupStepLine(3, "Pick the workspace to develop your app in"),
		setupStepLine(4, fmt.Sprintf("Paste the manifest %s shown above. It is also saved at: %s", format, plan.ManifestPath)),
		setupStepLine(5, "Create the app; optionally upload the icon PNG shown above in Display Information; then copy Client ID from Basic Information"),
		setupStepLine(6, "slacky will start a localhost OAuth callback and store the returned user token"),
		"",
		output.Bold("Alternative if you already have a scoped user token:"),
		"  slacky auth import",
		"",
		output.Bold("Verify:"),
		"  slacky auth status",
	)
	return strings.Join(lines, "\n")
}

func setupStepLine(number int, text string) string {
	return fmt.Sprintf("%s %s", output.Cyan(fmt.Sprintf("%d.", number)), text)
}

func writeSetupManifestFile(pathSet paths.Set, format, manifest string) (string, error) {
	format = strings.ToLower(firstNonEmpty(format, "json"))
	dir := setupManifestDir(pathSet)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "slacky-read-only."+format)
	return path, os.WriteFile(path, []byte(manifest), 0o600)
}

func writeSetupIconFiles(pathSet paths.Set) (setupIconFiles, error) {
	dir := setupManifestDir(pathSet)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return setupIconFiles{}, err
	}
	pngPath := filepath.Join(dir, "slacky-icon.png")
	if err := os.WriteFile(pngPath, setupassets.IconPNG, 0o644); err != nil {
		return setupIconFiles{}, err
	}
	return setupIconFiles{
		FileName:   filepath.Base(pngPath),
		Directory:  dir,
		PNGPath:    pngPath,
		PNGFileURL: fileURL(pngPath),
	}, nil
}

func setupManifestDir(pathSet paths.Set) string {
	return filepath.Join(filepath.Dir(pathSet.CacheDB.Path), "manifests")
}

func fileURL(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func waitForEnter(reader *bufio.Reader, prompt string) error {
	if _, err := fmt.Fprintln(os.Stdout, prompt); err != nil {
		return err
	}
	_, err := reader.ReadString('\n')
	return err
}

func promptRequired(reader *bufio.Reader, label string) (string, error) {
	if _, err := fmt.Fprintf(os.Stdout, "%s: ", label); err != nil {
		return "", err
	}
	value, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", appError("missing_prompt_value", fmt.Sprintf("%s is required", label))
	}
	return value, nil
}

func (cmd *SetupManifestCmd) Run(globals *Globals) error {
	manifest := newManifest(cmd.AppName, cmd.RedirectURI)
	rendered, err := renderManifest(manifest, cmd.Format)
	if err != nil {
		return err
	}
	if cmd.Output != "" {
		if err := os.WriteFile(cmd.Output, []byte(rendered), 0o600); err != nil {
			return err
		}
	}
	if cmd.Copy {
		if err := copyToClipboard(rendered); err != nil {
			return err
		}
	}

	text := rendered
	if cmd.Output != "" || cmd.Copy {
		lines := []string{"Manifest generated"}
		if cmd.Output != "" {
			lines = append(lines, fmt.Sprintf("Wrote: %s", cmd.Output))
		}
		if cmd.Copy {
			lines = append(lines, "Copied: true")
		}
		text = strings.Join(lines, "\n")
	}

	return writeEnvelope(globals, Envelope{
		OK:   true,
		Text: text,
		Results: map[string]any{
			"format":   cmd.Format,
			"manifest": manifest,
			"output":   cmd.Output,
			"copied":   cmd.Copy,
		},
	})
}

func newManifest(appName, redirectURI string) slackManifest {
	redirects := []string{
		redirectURI,
		"http://127.0.0.1:8889/callback",
	}
	return slackManifest{
		Metadata: map[string]int{
			"major_version": 2,
			"minor_version": 1,
		},
		DisplayInformation: map[string]string{
			"name":             appName,
			"description":      setupShortDescription,
			"long_description": setupLongDescription,
			"background_color": setupBackgroundColor,
		},
		OAuthConfig: manifestOAuthConfig{
			RedirectURLs: redirects,
			PKCEEnabled:  true,
			Scopes: map[string][]string{
				"user": api.UserScopes,
			},
		},
		Settings: map[string]any{
			"org_deploy_enabled":     false,
			"socket_mode_enabled":    false,
			"is_hosted":              false,
			"token_rotation_enabled": false,
		},
	}
}

func renderManifest(manifest slackManifest, format string) (string, error) {
	if format == "json" {
		data, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data) + "\n", nil
	}
	var builder strings.Builder
	builder.WriteString("_metadata:\n")
	builder.WriteString("  major_version: 2\n")
	builder.WriteString("  minor_version: 1\n")
	builder.WriteString("display_information:\n")
	builder.WriteString("  name: " + yamlString(manifest.DisplayInformation["name"]) + "\n")
	builder.WriteString("  description: " + yamlString(manifest.DisplayInformation["description"]) + "\n")
	builder.WriteString("  long_description: " + yamlString(manifest.DisplayInformation["long_description"]) + "\n")
	builder.WriteString("  background_color: " + yamlString(manifest.DisplayInformation["background_color"]) + "\n")
	builder.WriteString("oauth_config:\n")
	builder.WriteString("  redirect_urls:\n")
	for _, redirect := range manifest.OAuthConfig.RedirectURLs {
		builder.WriteString("    - " + yamlString(redirect) + "\n")
	}
	builder.WriteString("  scopes:\n")
	builder.WriteString("    user:\n")
	for _, scope := range api.UserScopes {
		builder.WriteString("      - " + scope + "\n")
	}
	builder.WriteString("  pkce_enabled: true\n")
	builder.WriteString("settings:\n")
	builder.WriteString("  org_deploy_enabled: false\n")
	builder.WriteString("  socket_mode_enabled: false\n")
	builder.WriteString("  is_hosted: false\n")
	builder.WriteString("  token_rotation_enabled: false\n")
	return builder.String(), nil
}

func yamlString(value string) string {
	data, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(data)
}

func copyToClipboard(text string) error {
	if runtime.GOOS != "darwin" {
		return appError("unsupported_clipboard", "--copy currently requires macOS pbcopy")
	}
	command := exec.Command("pbcopy")
	command.Stdin = strings.NewReader(text)
	return command.Run()
}
