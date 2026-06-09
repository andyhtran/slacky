package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/andyhtran/slacky/internal/api"
	"github.com/andyhtran/slacky/internal/config"
	"github.com/andyhtran/slacky/internal/paths"
	"github.com/andyhtran/slacky/internal/store"
)

type DoctorCmd struct{}

type PathsCmd struct{}

type CacheCmd struct {
	Default CacheStatusCmd `cmd:"" default:"noargs" hidden:""`
	Status  CacheStatusCmd `cmd:"" help:"Show cache status"`
}

type AuthCmd struct {
	Default AuthSummaryCmd `cmd:"" default:"noargs" hidden:""`
	Login   AuthLoginCmd   `cmd:"" help:"Start Slack user-token login"`
	Import  AuthImportCmd  `cmd:"" help:"Import an existing Slack user token"`
	Status  AuthStatusCmd  `cmd:"" help:"Show auth status"`
	Logout  AuthLogoutCmd  `cmd:"" help:"Log out by removing local Slack auth"`
	Clear   AuthClearCmd   `cmd:"" help:"Remove local Slack auth file"`
}

type AuthSummaryCmd struct{}

type AuthStatusCmd struct{}

type AuthLogoutCmd struct {
	DryRun bool `help:"Preview logout without removing auth" name:"dry-run"`
}

type AuthClearCmd struct {
	Apply bool `help:"Actually remove the auth file" name:"apply"`
}

type AuthLoginCmd struct {
	ClientID      string        `help:"Slack app client ID" name:"client-id"`
	ClientSecret  string        `help:"Slack app client secret" name:"client-secret"`
	RedirectURI   string        `help:"OAuth redirect URI" default:"http://127.0.0.1:8888/callback" name:"redirect-uri"`
	Port          int           `help:"Local callback port" default:"8888" name:"port"`
	FallbackPort  int           `help:"Fallback local callback port" default:"8889" name:"fallback-port"`
	Manual        bool          `help:"Print OAuth URL for manual/headless login" name:"manual"`
	PasteCallback bool          `help:"Accept a pasted final localhost callback URL" name:"paste-callback"`
	CallbackURL   string        `help:"Final localhost callback URL from Slack approval" name:"callback-url"`
	NoOpen        bool          `help:"Do not open a browser automatically" name:"no-open"`
	WaitTimeout   time.Duration `help:"How long to wait for the local OAuth callback" default:"5m" name:"wait-timeout"`
}

type AuthImportCmd struct {
	TokenStdin bool   `help:"Read Slack user token from stdin" name:"token-stdin"`
	TokenFile  string `help:"Read Slack user token from a file" name:"token-file"`
	TokenEnv   string `help:"Read Slack user token from an environment variable" name:"token-env"`
	NoValidate bool   `help:"Store token without calling Slack auth.test" name:"no-validate"`
}

type CacheStatusCmd struct{}

type SlackReachability struct {
	OK         bool   `json:"ok"`
	Skipped    bool   `json:"skipped"`
	Method     string `json:"method"`
	Label      string `json:"label"`
	Error      string `json:"error,omitempty"`
	RetryAfter string `json:"retry_after,omitempty"`
	RetryAt    string `json:"retry_at,omitempty"`
	Team       string `json:"team,omitempty"`
	User       string `json:"user,omitempty"`
	TeamID     string `json:"team_id,omitempty"`
	UserID     string `json:"user_id,omitempty"`
}

func (cmd *DoctorCmd) Run(globals *Globals) error {
	pathSet, err := paths.Resolve()
	if err != nil {
		return err
	}
	authStatus := config.InspectAuth(pathSet.AuthFile.Path)
	cacheStatus := store.Inspect(pathSet.CacheDB.Path)
	reachability := slackReachability(globals, pathSet.AuthFile.Path, authStatus)

	checks := []map[string]any{
		{"name": "home", "ok": pathExists(pathSet.Home.Path), "path": pathSet.Home.Path},
		{"name": "auth_file", "ok": authStatus.ReadyForSlack, "path": pathSet.AuthFile.Path, "recovery": authStatus.RecoveryCommands},
		{"name": "slack_reachability", "ok": reachability.OK, "skipped": reachability.Skipped, "method": "auth.test", "result": reachability},
		{"name": "cache", "ok": cacheStatus.Exists, "path": cacheStatus.CachePath, "notice": cacheStatus.Notice},
		{"name": "cooldowns", "ok": len(cacheStatus.Cooldowns) == 0, "cooldowns": cacheStatus.Cooldowns},
	}

	lines := []string{
		"Doctor",
		"",
		fmt.Sprintf("Home: %s (%s)", pathSet.Home.Path, existsLabel(pathExists(pathSet.Home.Path))),
		fmt.Sprintf("Auth: %s", readiness(authStatus.ReadyForSlack)),
		fmt.Sprintf("Slack reachability: %s", reachability.Label),
		fmt.Sprintf("Cache: %s", cacheLabel(cacheStatus)),
		"",
		"Next:",
		"  slacky auth status",
		"  slacky setup wizard",
		"  slacky cache status",
	}

	return writeEnvelope(globals, Envelope{
		OK:      true,
		Text:    strings.Join(lines, "\n"),
		Source:  "cache",
		Paths:   pathSet,
		Auth:    authStatus,
		Cache:   cacheStatus,
		Results: checks,
	})
}

func (cmd *PathsCmd) Run(globals *Globals) error {
	pathSet, err := paths.Resolve()
	if err != nil {
		return err
	}
	text := strings.Join([]string{
		"Paths",
		"",
		fmt.Sprintf("Home:      %s (%s)", pathSet.Home.Path, pathSet.Home.Source),
		fmt.Sprintf("Auth:      %s (%s)", pathSet.AuthFile.Path, pathSet.AuthFile.Source),
		fmt.Sprintf("Cache DB:  %s (%s)", pathSet.CacheDB.Path, pathSet.CacheDB.Source),
		fmt.Sprintf("State:     %s (%s)", pathSet.StateDir.Path, pathSet.StateDir.Source),
		fmt.Sprintf("Logs:      %s (%s)", pathSet.LogsDir.Path, pathSet.LogsDir.Source),
		fmt.Sprintf("Skills:    %s (%s)", pathSet.SkillsDir.Path, pathSet.SkillsDir.Source),
	}, "\n")
	return writeEnvelope(globals, Envelope{
		OK:    true,
		Text:  text,
		Paths: pathSet,
	})
}

func (cmd *CacheStatusCmd) Run(globals *Globals) error {
	pathSet, err := paths.Resolve()
	if err != nil {
		return err
	}
	cacheStatus := store.Inspect(pathSet.CacheDB.Path)
	text := strings.Join([]string{
		"Cache",
		"",
		fmt.Sprintf("DB: %s", cacheStatus.CachePath),
		fmt.Sprintf("Exists: %t", cacheStatus.Exists),
		fmt.Sprintf("Schema version: %d", cacheStatus.SchemaVersion),
		fmt.Sprintf("Size: %s", humanByteSizeWithExact(cacheStatus.SizeBytes)),
		fmt.Sprintf("Notice: %s", cacheStatus.Notice),
		"",
		"Next:",
		"  slacky search --local \"release notes\"",
		"  slacky history --channel general --refresh",
	}, "\n")
	return writeEnvelope(globals, Envelope{
		OK:     true,
		Text:   text,
		Source: "cache",
		Cache:  cacheStatus,
	})
}

func (cmd *AuthSummaryCmd) Run(globals *Globals) error {
	status := AuthStatusCmd{}
	return status.Run(globals)
}

func (cmd *AuthStatusCmd) Run(globals *Globals) error {
	pathSet, err := paths.Resolve()
	if err != nil {
		return err
	}
	authStatus := config.InspectAuth(pathSet.AuthFile.Path)
	authUser := authenticatedUserLabel(authStatus, pathSet.CacheDB.Path)
	lines := make([]string, 0, 12)
	lines = append(
		lines,
		"Auth",
		"",
		fmt.Sprintf("File: %s", authStatus.Path),
		fmt.Sprintf("Ready for Slack: %t", authStatus.ReadyForSlack),
		fmt.Sprintf("Team: %s", blank(authStatus.TeamName)),
		fmt.Sprintf("User: %s", authDisplayLabel(authStatus.UserID, strings.TrimPrefix(authUser, "@"))),
		"",
	)
	lines = append(lines, authStatusNextLines(authStatus.ReadyForSlack, authUser)...)
	text := strings.Join(lines, "\n")
	return writeEnvelope(globals, Envelope{
		OK:   true,
		Text: text,
		Auth: authStatus,
	})
}

func authStatusNextLines(authReady bool, authUser string) []string {
	if !authReady {
		return []string{
			"Next:",
			"  slacky setup wizard",
			"  slacky auth import",
		}
	}
	return []string{
		"Next:",
		"  slacky doctor",
		"  " + defaultSearchCommand(authUser),
	}
}

func (cmd *AuthClearCmd) Run(globals *Globals) error {
	pathSet, err := paths.Resolve()
	if err != nil {
		return err
	}
	if !cmd.Apply {
		text := strings.Join([]string{
			"Auth clear dry run",
			"",
			fmt.Sprintf("Would remove: %s", pathSet.AuthFile.Path),
			"",
			"Apply:",
			"  slacky auth clear --apply",
		}, "\n")
		return writeEnvelope(globals, Envelope{
			OK:     true,
			Text:   text,
			Auth:   config.InspectAuth(pathSet.AuthFile.Path),
			DryRun: true,
		})
	}
	removed, err := config.RemoveAuth(pathSet.AuthFile.Path)
	if err != nil {
		return err
	}
	text := fmt.Sprintf("Removed auth file: %t", removed)
	return writeEnvelope(globals, Envelope{
		OK:   true,
		Text: text,
		Auth: map[string]any{
			"path":    pathSet.AuthFile.Path,
			"removed": removed,
		},
	})
}

func (cmd *AuthLogoutCmd) Run(globals *Globals) error {
	pathSet, err := paths.Resolve()
	if err != nil {
		return err
	}
	if cmd.DryRun {
		text := strings.Join([]string{
			"Auth logout dry run",
			"",
			fmt.Sprintf("Would remove: %s", pathSet.AuthFile.Path),
			fmt.Sprintf("Cache retained: %s", pathSet.CacheDB.Path),
			"",
			"Apply:",
			"  slacky auth logout",
		}, "\n")
		return writeEnvelope(globals, Envelope{
			OK:     true,
			Text:   text,
			Auth:   config.InspectAuth(pathSet.AuthFile.Path),
			DryRun: true,
		})
	}
	removed, err := config.RemoveAuth(pathSet.AuthFile.Path)
	if err != nil {
		return err
	}
	text := strings.Join([]string{
		"Auth logout complete",
		"",
		fmt.Sprintf("Removed local auth: %t", removed),
		fmt.Sprintf("Auth file: %s", pathSet.AuthFile.Path),
		"Slack token revoked: false",
		fmt.Sprintf("Cache retained: %s", pathSet.CacheDB.Path),
		"",
		"Next:",
		"  slacky auth import",
		"  slacky setup wizard",
		"  slacky auth status",
	}, "\n")
	return writeEnvelope(globals, Envelope{
		OK:   true,
		Text: text,
		Auth: map[string]any{
			"path":           pathSet.AuthFile.Path,
			"removed":        removed,
			"revoked":        false,
			"cache_retained": pathSet.CacheDB.Path,
		},
	})
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func existsLabel(ok bool) string {
	if ok {
		return "exists"
	}
	return "missing"
}

func slackReachability(globals *Globals, authPath string, authStatus config.AuthStatus) SlackReachability {
	reachability := SlackReachability{
		Method: "auth.test",
	}
	if !authStatus.ReadyForSlack {
		reachability.Skipped = true
		reachability.Label = "skipped, missing auth"
		return reachability
	}
	auth, err := config.LoadAuth(authPath)
	if err != nil {
		reachability.Label = "auth file unreadable"
		reachability.Error = err.Error()
		return reachability
	}
	ctx, cancel := context.WithTimeout(context.Background(), globals.Timeout)
	defer cancel()
	result, err := api.NewClient(auth.UserToken, appVersion, globals.Timeout, globals.MaxRateLimitWait).AuthTest(ctx)
	if err == nil {
		reachability.OK = true
		reachability.Label = "ok"
		reachability.Team = result.Team
		reachability.User = result.User
		reachability.TeamID = result.TeamID
		reachability.UserID = result.UserID
		return reachability
	}
	var rateLimit api.RateLimitError
	if errors.As(err, &rateLimit) {
		reachability.Label = "rate limited"
		reachability.Error = rateLimit.Error()
		reachability.RetryAfter = rateLimit.RetryAfter.String()
		reachability.RetryAt = rateLimit.RetryAt.Format(time.RFC3339)
		return reachability
	}
	var slackErr api.SlackError
	if errors.As(err, &slackErr) {
		reachability.Label = "Slack API error"
		reachability.Error = slackErr.Code
		return reachability
	}
	reachability.Label = "network or runtime error"
	reachability.Error = err.Error()
	return reachability
}

func blank(value string) string {
	if value == "" {
		return "(blank)"
	}
	return value
}
