package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/andyhtran/slacky/internal/api"
	"github.com/andyhtran/slacky/internal/output"
	"github.com/andyhtran/slacky/internal/paths"
)

func (cmd *AuthImportCmd) Run(globals *Globals) error {
	pathSet, err := paths.Resolve()
	if err != nil {
		return err
	}
	token, source, err := cmd.readToken(globals)
	if err != nil {
		return err
	}
	if err := validateImportedTokenShape(token); err != nil {
		return err
	}

	auth := importedUserTokenAuth(token)

	validation := "skipped"
	if !cmd.NoValidate {
		result, err := authTestImportedToken(globals, token)
		if err != nil {
			return err
		}
		validation = "ok"
		auth.TeamID = result.TeamID
		auth.TeamName = result.Team
		auth.UserID = result.UserID
		auth.UserName = result.User
		auth.Scopes = append([]string{}, result.Scopes...)
		if auth.TokenType == "" {
			auth.TokenType = "user"
		}
	}

	profileName := strings.TrimSpace(cmd.Name)
	auth.ProfileName = profileName
	storedPath, err := writeSelectedAuth(pathSet, profileName, auth)
	if err != nil {
		return err
	}

	summary := authProfileSummary(profileName, storedPath, auth)
	summary["token_source"] = source
	summary["validation"] = validation
	text := strings.Join([]string{
		"Auth token import complete",
		"",
		fmt.Sprintf("Stored: %s", storedPath),
		fmt.Sprintf("Profile: %s", blank(auth.ProfileName)),
		fmt.Sprintf("Validation: %s", validation),
		fmt.Sprintf("Team: %s", blank(auth.TeamName)),
		fmt.Sprintf("User: %s", authDisplayLabel(auth.UserID, auth.UserName)),
		fmt.Sprintf("Scopes: %d", len(auth.Scopes)),
		"",
		"Next:",
		"  slacky auth status",
		"  " + defaultSearchCommand(authMentionLabel(auth.UserID, auth.UserName)),
	}, "\n")
	return writeEnvelope(globals, Envelope{
		OK:   true,
		Text: text,
		Auth: summary,
	})
}

func (cmd *AuthImportCmd) readToken(globals *Globals) (string, string, error) {
	sources := 0
	if cmd.TokenStdin {
		sources++
	}
	if strings.TrimSpace(cmd.TokenFile) != "" {
		sources++
	}
	if strings.TrimSpace(cmd.TokenEnv) != "" {
		sources++
	}
	if sources == 0 && !globals.JSON && !globals.Raw && output.IsTTY(os.Stdin) {
		value, err := readTokenFromStdin()
		if err != nil {
			return "", "", err
		}
		return cleanedToken(value, "prompt")
	}
	if sources != 1 {
		return "", "", missingUsage(
			"choose exactly one Slack user token source",
			"slacky auth import [--token-stdin|--token-file <path>|--token-env <name>]",
			"slacky auth import",
			"slacky auth import --token-file ~/.secret/slacky-token",
			"slacky auth import --token-env SLACKY_USER_TOKEN",
			"printf '%s\\n' \"$SLACKY_USER_TOKEN\" | slacky auth import --token-stdin",
		)
	}

	var raw string
	var source string
	switch {
	case cmd.TokenStdin:
		value, err := readTokenFromStdin()
		if err != nil {
			return "", "", err
		}
		raw = value
		source = "stdin"
	case strings.TrimSpace(cmd.TokenFile) != "":
		tokenFile := strings.TrimSpace(cmd.TokenFile)
		data, err := os.ReadFile(tokenFile)
		if err != nil {
			return "", "", err
		}
		raw = string(data)
		source = "file"
	case strings.TrimSpace(cmd.TokenEnv) != "":
		tokenEnv := strings.TrimSpace(cmd.TokenEnv)
		value, ok := os.LookupEnv(tokenEnv)
		if !ok || strings.TrimSpace(value) == "" {
			return "", "", appError("missing_token_env", fmt.Sprintf("environment variable %s is not set", tokenEnv))
		}
		raw = value
		source = "env"
	}

	return cleanedToken(raw, source)
}

func cleanedToken(raw string, source string) (string, string, error) {
	token := normalizeSlackToken(raw)
	if token == "" {
		return "", "", appError("missing_token", "token source did not contain a Slack user token")
	}
	if strings.ContainsAny(token, " \t\r\n") {
		return "", "", appError("invalid_token_format", "token source must contain a single Slack token value")
	}
	return token, source, nil
}

func readTokenFromStdin() (string, error) {
	if output.IsTTY(os.Stdin) {
		fmt.Fprintln(os.Stderr, "Expected: a Slack user token starting with xoxp- or xoxe.xoxp-. Input is hidden.")
		fmt.Fprint(os.Stderr, "Paste Slack user token: ")
		data, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		if len(data) == 0 {
			return "", appError("missing_token", "no token was provided on stdin")
		}
		return string(data), nil
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func normalizeSlackToken(raw string) string {
	token := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		token = strings.TrimSpace(token[len("bearer "):])
	}
	return token
}

func validateImportedTokenShape(token string) error {
	lower := strings.ToLower(token)
	if strings.HasPrefix(lower, "xoxb-") || strings.HasPrefix(lower, "xoxe.xoxb-") {
		return appError("invalid_token_type", "provided token looks like a Slack bot token; slacky requires a user token")
	}
	if strings.HasPrefix(lower, "xapp-") {
		return appError("invalid_token_type", "provided token looks like a Slack app-level token; slacky requires a user token")
	}
	if strings.HasPrefix(lower, "xoxc-") {
		return appError("invalid_token_type", "provided token looks like a Slack browser-session token; use slacky auth import-session --wizard")
	}
	return nil
}

func importedTokenType(token string) string {
	lower := strings.ToLower(token)
	if strings.HasPrefix(lower, "xoxp-") || strings.HasPrefix(lower, "xoxe.xoxp-") {
		return "user"
	}
	return ""
}

func authTestImportedToken(globals *Globals, token string) (api.AuthTestResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), globals.Timeout)
	defer cancel()
	result, err := api.NewClient(token, appVersion, globals.Timeout, globals.MaxRateLimitWait).AuthTest(ctx)
	if err != nil {
		return api.AuthTestResult{}, tokenValidationError(err)
	}
	if result.BotID != "" {
		return api.AuthTestResult{}, appError("invalid_token_type", "provided token is a Slack bot token; slacky requires a user token")
	}
	return result, nil
}

func tokenValidationError(err error) error {
	var slackErr api.SlackError
	if errors.As(err, &slackErr) {
		return appError("token_validation_failed", fmt.Sprintf("Slack auth.test failed: %s", slackErr.Code))
	}
	var rateLimit api.RateLimitError
	if errors.As(err, &rateLimit) {
		return appError("token_validation_rate_limited", rateLimit.Error())
	}
	return appError("token_validation_failed", err.Error())
}
