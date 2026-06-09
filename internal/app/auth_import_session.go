package app

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/andyhtran/slacky/internal/output"
	"github.com/andyhtran/slacky/internal/paths"
)

const browserSessionTokenSnippet = `JSON.parse(localStorage.localConfig_v2).teams[document.location.pathname.match(/^\/client\/([A-Z0-9]+)/)[1]].token`

func (cmd *AuthImportSessionCmd) Run(globals *Globals) error {
	if cmd.Headless {
		return writeEnvelope(globals, Envelope{
			OK:      true,
			Text:    browserSessionWizardText(cmd.Name),
			Results: browserSessionWizardPlan(cmd.Name),
		})
	}
	pathSet, err := paths.Resolve()
	if err != nil {
		return err
	}
	if cmd.Wizard && strings.TrimSpace(cmd.XOXCEnv) == "" {
		if err := writeBrowserSessionWizardStep(globals, browserSessionXOXCInstructions()); err != nil {
			return err
		}
	}
	xoxc, xoxcSource, err := cmd.readSecret("xoxc browser token", cmd.XOXCEnv, "Paste xoxc token: ")
	if err != nil {
		return err
	}
	if cmd.Wizard && strings.TrimSpace(cmd.XOXDEnv) == "" {
		if err := writeBrowserSessionWizardStep(globals, browserSessionXOXDInstructions()); err != nil {
			return err
		}
	}
	xoxd, xoxdSource, err := cmd.readSecret("Slack d cookie value", cmd.XOXDEnv, "Paste Slack d cookie value: ")
	if err != nil {
		return err
	}
	xoxc = normalizeSlackToken(xoxc)
	xoxd = strings.TrimSpace(xoxd)
	if err := validateBrowserSessionShape(xoxc, xoxd); err != nil {
		return err
	}

	userAgent := strings.TrimSpace(cmd.UserAgent)
	if userAgent == "" {
		userAgent = defaultBrowserSessionUserAgent
	}

	profileName := strings.TrimSpace(cmd.Name)
	auth := browserSessionAuth(xoxc, xoxd, userAgent)

	validation := "skipped"
	if !cmd.NoValidate {
		result, err := authTest(globals, auth)
		if err != nil {
			return err
		}
		validation = "ok"
		auth.TeamID = result.TeamID
		auth.TeamName = result.Team
		auth.UserID = result.UserID
		auth.UserName = result.User
		auth.Scopes = append([]string{}, result.Scopes...)
	}

	storedPath, err := writeSelectedAuth(pathSet, profileName, auth)
	if err != nil {
		return err
	}

	summary := authProfileSummary(profileName, storedPath, auth)
	summary["xoxc_source"] = xoxcSource
	summary["xoxd_source"] = xoxdSource
	summary["validation"] = validation
	summary["has_browser_user_agent"] = auth.BrowserUserAgent != ""

	text := strings.Join([]string{
		"Browser session import complete",
		"",
		fmt.Sprintf("Stored: %s", storedPath),
		fmt.Sprintf("Profile: %s", blank(auth.ProfileName)),
		fmt.Sprintf("Validation: %s", validation),
		fmt.Sprintf("Team: %s", blank(auth.TeamName)),
		fmt.Sprintf("User: %s", authDisplayLabel(auth.UserID, auth.UserName)),
		"",
		"Next:",
		"  slacky auth status",
		"  slacky search \"has:link\" --count 3",
	}, "\n")
	return writeEnvelope(globals, Envelope{
		OK:   true,
		Text: text,
		Auth: summary,
	})
}

func (cmd *AuthImportSessionCmd) readSecret(label string, envName string, prompt string) (string, string, error) {
	envName = strings.TrimSpace(envName)
	if envName != "" {
		value, ok := os.LookupEnv(envName)
		if !ok || strings.TrimSpace(value) == "" {
			return "", "", appError("missing_token_env", fmt.Sprintf("environment variable %s is not set", envName))
		}
		return value, "env", nil
	}
	if !output.IsTTY(os.Stdin) {
		return "", "", missingUsage(
			"missing "+label,
			"slacky auth import-session --wizard --name work-browser",
			"slacky auth import-session --xoxc-env SLACKY_XOXC --xoxd-env SLACKY_XOXD",
		)
	}
	fmt.Fprintln(os.Stderr, "Input is hidden. Treat this value as a Slack browser-session secret.")
	fmt.Fprint(os.Stderr, prompt)
	data, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", "", err
	}
	if len(data) == 0 {
		return "", "", appError("missing_token", "no "+label+" was provided")
	}
	return string(data), "prompt", nil
}

func validateBrowserSessionShape(xoxc string, xoxd string) error {
	if !strings.HasPrefix(strings.ToLower(xoxc), "xoxc-") {
		return appError("invalid_token_type", "browser session import expects an xoxc token from Slack web localStorage")
	}
	if !strings.HasPrefix(strings.ToLower(xoxd), "xoxd-") {
		return appError("invalid_token_type", "browser session import expects the value of Slack cookie named d, usually starting with xoxd-")
	}
	if strings.ContainsAny(xoxc, " \t\r\n") || strings.ContainsAny(xoxd, " \t\r\n") {
		return appError("invalid_token_format", "browser session values must not contain whitespace")
	}
	return nil
}

func browserSessionWizardText(profileName string) string {
	lines := []string{
		"Browser session import wizard",
		"",
		"Use this only for your own logged-in Slack browser session when Slack app installation is blocked.",
		"Do not paste the copied values into chat, logs, screenshots, or issue trackers. Slacky prompts for them with hidden terminal input.",
	}
	if strings.TrimSpace(profileName) != "" {
		lines = append(lines, fmt.Sprintf("Profile: %s", strings.TrimSpace(profileName)))
	}
	lines = append(
		lines,
		"",
		browserSessionXOXCInstructions(),
		"",
		browserSessionXOXDInstructions(),
		"",
		"Interactive command:",
		"  "+browserSessionWizardCommand(profileName),
		"",
		"Non-interactive local testing:",
		"  slacky auth import-session --name "+firstNonEmpty(strings.TrimSpace(profileName), "work-browser")+" --xoxc-env SLACKY_XOXC --xoxd-env SLACKY_XOXD",
	)
	return strings.Join(lines, "\n")
}

func browserSessionXOXCInstructions() string {
	return strings.Join([]string{
		"Step 1: copy XOXC_TOKEN",
		"",
		"1. Open Slack in the browser workspace you want Slacky to use.",
		"2. Open the browser Developer Console. In Chrome, click the three dots button to the right of the URL bar, then choose More Tools -> Developer Tools.",
		"3. Switch to the Console tab.",
		"4. If Chrome blocks paste, type allow pasting and press Enter.",
		"5. Paste this snippet and press Enter:",
		"",
		"```js",
		browserSessionTokenSnippet,
		"```",
		"",
		"6. Copy the returned value. It should start with xoxc-.",
		"7. Return to this terminal and paste it at the hidden XOXC prompt.",
	}, "\n")
}

func browserSessionXOXDInstructions() string {
	return strings.Join([]string{
		"Step 2: copy XOXD_TOKEN",
		"",
		"1. In Developer Tools, switch to the Application tab.",
		"2. In the left navigation pane, open Cookies and select the Slack workspace domain.",
		"3. Find the cookie named d. The name is just the letter d.",
		"4. Double-click the Value field for that cookie.",
		"5. Press Cmd+C or Ctrl+C to copy the value. It usually starts with xoxd-.",
		"6. Return to this terminal and paste it at the hidden d cookie prompt.",
	}, "\n")
}

func browserSessionWizardPlan(profileName string) map[string]any {
	return map[string]any{
		"profile":             strings.TrimSpace(profileName),
		"manual_only":         true,
		"interactive_command": browserSessionWizardCommand(profileName),
		"token_snippet":       browserSessionTokenSnippet,
		"required_values":     []string{"xoxc browser token", "Slack cookie named d"},
	}
}

func browserSessionWizardCommand(profileName string) string {
	profileName = strings.TrimSpace(profileName)
	if profileName == "" {
		return "slacky auth import-session --wizard --name work-browser"
	}
	return "slacky auth import-session --wizard --name " + profileName
}

func writeBrowserSessionWizardStep(globals *Globals, text string) error {
	if globals.JSON {
		_, err := fmt.Fprintln(os.Stderr, text)
		return err
	}
	return output.WriteText(os.Stdout, text)
}
