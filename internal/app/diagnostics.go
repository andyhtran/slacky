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
	"github.com/andyhtran/slacky/internal/output"
	"github.com/andyhtran/slacky/internal/paths"
	"github.com/andyhtran/slacky/internal/store"
)

type DoctorCmd struct{}

type PathsCmd struct{}

type CacheCmd struct {
	Default CacheStatusCmd `cmd:"" default:"noargs" hidden:""`
	Status  CacheStatusCmd `cmd:"" help:"Show cache status"`
	Prune   CachePruneCmd  `cmd:"" help:"Preview or remove old cached messages"`
	Clear   CacheClearCmd  `cmd:"" help:"Clear cache database files"`
}

type AuthCmd struct {
	Default       AuthSummaryCmd       `cmd:"" default:"noargs" hidden:""`
	Login         AuthLoginCmd         `cmd:"" help:"Start Slack user-token login"`
	Import        AuthImportCmd        `cmd:"" help:"Import an existing Slack user token"`
	ImportSession AuthImportSessionCmd `cmd:"" name:"import-session" help:"Import a Slack browser session token and d cookie"`
	List          AuthListCmd          `cmd:"" help:"List saved auth profiles"`
	Switch        AuthSwitchCmd        `cmd:"" help:"Switch the active auth profile"`
	Refresh       AuthRefreshCmd       `cmd:"" help:"Refresh rotating OAuth credentials"`
	Status        AuthStatusCmd        `cmd:"" help:"Show auth status"`
	Logout        AuthLogoutCmd        `cmd:"" help:"Log out by removing local Slack auth"`
	Clear         AuthClearCmd         `cmd:"" help:"Remove local Slack auth file"`
}

type AuthSummaryCmd struct{}

type AuthListCmd struct{}

type AuthSwitchCmd struct {
	Name string `arg:"" help:"Auth profile name"`
}

type AuthStatusCmd struct {
	Active bool `help:"Show only the active auth profile (default status scope)" name:"active"`
}

type AuthRefreshCmd struct {
	Profile string `help:"Refresh a named auth profile instead of the active auth" name:"profile"`
	Force   bool   `help:"Refresh even when the token is not near expiry" name:"force"`
}

type AuthLogoutCmd struct {
	DryRun bool   `help:"Preview logout without removing auth" name:"dry-run"`
	Name   string `help:"Remove a named auth profile instead of the active local auth" name:"name"`
}

type AuthClearCmd struct {
	Apply bool `help:"Actually remove the auth file" name:"apply"`
}

type AuthLoginCmd struct {
	Name          string        `help:"Store auth under a named profile and make it active" name:"name"`
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
	Name       string `help:"Store auth under a named profile and make it active" name:"name"`
	TokenStdin bool   `help:"Read Slack user token from stdin" name:"token-stdin"`
	TokenFile  string `help:"Read Slack user token from a file" name:"token-file"`
	TokenEnv   string `help:"Read Slack user token from an environment variable" name:"token-env"`
	NoValidate bool   `help:"Store token without calling Slack auth.test" name:"no-validate"`
}

type AuthImportSessionCmd struct {
	Name       string `help:"Store auth under a named profile and make it active" name:"name"`
	XOXCEnv    string `help:"Read xoxc browser token from an environment variable" name:"xoxc-env"`
	XOXDEnv    string `help:"Read Slack d cookie value from an environment variable" name:"xoxd-env"`
	UserAgent  string `help:"Browser User-Agent to send with session requests" name:"user-agent"`
	NoValidate bool   `help:"Store session without calling Slack auth.test" name:"no-validate"`
	Wizard     bool   `help:"Walk through manually collecting the xoxc token and d cookie before hidden prompts" name:"wizard"`
	Headless   bool   `help:"Print browser-session collection instructions without prompting or storing auth" name:"headless"`
}

type CacheStatusCmd struct {
	Profile string `help:"Show cache status for a named auth profile instead of the active auth" name:"profile"`
}

type CacheClearCmd struct {
	Profile string `help:"Clear cache for a named auth profile instead of the active auth" name:"profile"`
	DryRun  bool   `help:"Preview cache files without removing them" name:"dry-run"`
	Apply   bool   `help:"Actually remove cache files" name:"apply"`
}

type CachePruneCmd struct {
	Profile   string `help:"Prune cache for a named auth profile instead of the active auth" name:"profile"`
	OlderThan int    `help:"Delete messages last cached more than this many days ago" default:"30" name:"older-than"`
	DryRun    bool   `help:"Preview old cache rows without removing them" name:"dry-run"`
	Apply     bool   `help:"Actually remove old cache rows" name:"apply"`
}

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
	authStatus := inspectAuthStatus(pathSet)
	cacheStatus := store.Inspect(pathSet.CacheDB.Path)
	reachability := slackReachability(globals, pathSet, pathSet.AuthFile.Path, authStatus)
	refreshOK := authRefreshDiagnosticOK(authStatus)
	mixedFieldsOK := len(authStatus.MixedAuthFields) == 0

	checks := []map[string]any{
		{"name": "home", "ok": pathExists(pathSet.Home.Path), "path": pathSet.Home.Path},
		{"name": "auth_file", "ok": authStatus.ReadyForSlack, "path": pathSet.AuthFile.Path, "recovery": authStatus.RecoveryCommands},
		{"name": "auth_refresh", "ok": refreshOK, "auth_kind": authStatus.AuthKind, "expires_at": authStatus.ExpiresAt, "refresh_possible": authStatus.RefreshPossible, "refresh_due": authStatus.RefreshDue, "expired": authStatus.Expired},
		{"name": "auth_mixed_fields", "ok": mixedFieldsOK, "fields": authStatus.MixedAuthFields},
		{"name": "slack_reachability", "ok": reachability.OK, "skipped": reachability.Skipped, "method": "auth.test", "result": reachability},
		{"name": "cache", "ok": cacheStatus.Exists, "path": cacheStatus.CachePath, "notice": cacheStatus.Notice},
		{"name": "cooldowns", "ok": len(cacheStatus.Cooldowns) == 0, "cooldowns": cacheStatus.Cooldowns},
	}

	lines := []string{
		"Doctor",
		"",
		fmt.Sprintf("Home: %s (%s)", pathSet.Home.Path, existsLabel(pathExists(pathSet.Home.Path))),
		fmt.Sprintf("Auth: %s", readiness(authStatus.ReadyForSlack)),
		fmt.Sprintf("OAuth refresh: %s", authRefreshDiagnosticLabel(authStatus)),
		fmt.Sprintf("Auth fields: %s", authMixedFieldsLabel(authStatus.MixedAuthFields)),
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
	authStatus := inspectAuthStatus(pathSet)
	text := strings.Join([]string{
		"Paths",
		"",
		fmt.Sprintf("Home:      %s (%s)", pathSet.Home.Path, pathSet.Home.Source),
		fmt.Sprintf("Auth:      %s (%s)", pathSet.AuthFile.Path, pathSet.AuthFile.Source),
		fmt.Sprintf("Profile:   %s", blank(authStatus.ProfileName)),
		fmt.Sprintf("Team:      %s", authTeamLabel(authStatus.TeamName, authStatus.TeamID)),
		fmt.Sprintf("User:      %s", authDisplayLabel(authStatus.UserID, authStatus.UserName)),
		fmt.Sprintf("Profiles:  %s (%s)", pathSet.AuthProfilesDir.Path, pathSet.AuthProfilesDir.Source),
		fmt.Sprintf("Cache DB:  %s (%s)", pathSet.CacheDB.Path, pathSet.CacheDB.Source),
		fmt.Sprintf("State:     %s (%s)", pathSet.StateDir.Path, pathSet.StateDir.Source),
		fmt.Sprintf("Logs:      %s (%s)", pathSet.LogsDir.Path, pathSet.LogsDir.Source),
		fmt.Sprintf("Skills:    %s (%s)", pathSet.SkillsDir.Path, pathSet.SkillsDir.Source),
	}, "\n")
	return writeEnvelope(globals, Envelope{
		OK:    true,
		Text:  text,
		Paths: pathSet,
		Auth:  authStatus,
	})
}

func (cmd *CacheStatusCmd) Run(globals *Globals) error {
	pathSet, err := paths.Resolve()
	if err != nil {
		return err
	}
	target, err := cacheTargetForProfile(pathSet, cmd.Profile)
	if err != nil {
		return err
	}
	cacheStatus := store.Inspect(target.Path)
	text := strings.Join([]string{
		"Cache",
		"",
		fmt.Sprintf("Profile: %s", cacheProfileLabel(target.ProfileName)),
		fmt.Sprintf("Team: %s", authTeamLabel(target.Auth.TeamName, target.Auth.TeamID)),
		fmt.Sprintf("User: %s", authDisplayLabel(target.Auth.UserID, target.Auth.UserName)),
		fmt.Sprintf("DB: %s", cacheStatus.CachePath),
		fmt.Sprintf("Exists: %t", cacheStatus.Exists),
		fmt.Sprintf("Schema version: %d", cacheStatus.SchemaVersion),
		fmt.Sprintf("Size: %s", humanByteSizeWithExact(cacheStatus.SizeBytes)),
		fmt.Sprintf("Notice: %s", cacheStatus.Notice),
		"",
		"Next:",
		"  slacky search --local \"release notes\"",
		"  slacky cache prune --older-than 90 --dry-run",
		"  slacky history --channel general --refresh",
	}, "\n")
	return writeEnvelope(globals, Envelope{
		OK:      true,
		Text:    text,
		Source:  "cache",
		Cache:   cacheStatus,
		Results: cacheTargetPayload(target),
	})
}

func (cmd *CachePruneCmd) Run(globals *Globals) error {
	if cmd.Apply && cmd.DryRun {
		return appError("invalid_cache_prune_flags", "use either --dry-run or --apply, not both")
	}
	if cmd.OlderThan <= 0 {
		return appError("invalid_cache_prune_age", "--older-than must be greater than 0 days")
	}
	pathSet, err := paths.Resolve()
	if err != nil {
		return err
	}
	target, err := cacheTargetForProfile(pathSet, cmd.Profile)
	if err != nil {
		return err
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -cmd.OlderThan)
	cacheStatus := store.Inspect(target.Path)
	if !cacheStatus.Exists {
		result := store.PruneResult{Cutoff: cutoff.UTC().Format("2006-01-02 15:04:05"), DryRun: !cmd.Apply}
		return writeEnvelope(globals, cachePruneEnvelope(target, result, cacheStatus, cmd.OlderThan, !cmd.Apply))
	}
	var cacheDB *store.DB
	if cmd.Apply {
		cacheDB, err = store.Open(target.Path)
	} else {
		cacheDB, err = store.OpenReadOnly(target.Path)
	}
	if err != nil {
		return err
	}
	result, err := cacheDB.PruneMessagesBefore(cutoff, !cmd.Apply)
	closeErr := cacheDB.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	cacheStatus = store.Inspect(target.Path)
	return writeEnvelope(globals, cachePruneEnvelope(target, result, cacheStatus, cmd.OlderThan, !cmd.Apply))
}

func (cmd *CacheClearCmd) Run(globals *Globals) error {
	if cmd.Apply && cmd.DryRun {
		return appError("invalid_cache_clear_flags", "use either --dry-run or --apply, not both")
	}
	pathSet, err := paths.Resolve()
	if err != nil {
		return err
	}
	target, err := cacheTargetForProfile(pathSet, cmd.Profile)
	if err != nil {
		return err
	}
	files := inspectCacheFiles(target.Path)
	if !cmd.Apply {
		lines := []string{
			"Cache clear dry run",
			"",
			fmt.Sprintf("Profile: %s", cacheProfileLabel(target.ProfileName)),
			fmt.Sprintf("Team: %s", authTeamLabel(target.Auth.TeamName, target.Auth.TeamID)),
			fmt.Sprintf("User: %s", authDisplayLabel(target.Auth.UserID, target.Auth.UserName)),
			fmt.Sprintf("DB: %s", target.Path),
			"",
			"Would remove:",
		}
		for _, file := range files {
			lines = append(lines, fmt.Sprintf("  %s (%s)", file.Path, existsLabel(file.Exists)))
		}
		lines = append(lines, "", "Apply:", cacheClearApplyCommand(target.ProfileName))
		return writeEnvelope(globals, Envelope{
			OK:     true,
			Text:   strings.Join(lines, "\n"),
			Source: "cache",
			Cache:  cacheClearPayload(target, files),
			DryRun: true,
		})
	}
	for index := range files {
		if !files[index].Exists {
			continue
		}
		if err := os.Remove(files[index].Path); err != nil {
			files[index].Error = err.Error()
			continue
		}
		files[index].Removed = true
	}
	text := strings.Join([]string{
		"Cache clear complete",
		"",
		fmt.Sprintf("Profile: %s", cacheProfileLabel(target.ProfileName)),
		fmt.Sprintf("DB: %s", target.Path),
		fmt.Sprintf("Removed files: %d", countRemovedCacheFiles(files)),
		"",
		"Next:",
		"  slacky cache status" + cacheProfileFlag(target.ProfileName),
	}, "\n")
	return writeEnvelope(globals, Envelope{
		OK:     true,
		Text:   text,
		Source: "cache",
		Cache:  cacheClearPayload(target, files),
	})
}

func (cmd *AuthSummaryCmd) Run(globals *Globals) error {
	status := AuthStatusCmd{}
	return status.Run(globals)
}

func (cmd *AuthListCmd) Run(globals *Globals) error {
	pathSet, err := paths.Resolve()
	if err != nil {
		return err
	}
	activeName := activeProfileName(pathSet)
	profiles, err := config.ListAuthProfiles(pathSet.AuthProfilesDir.Path, activeName)
	if err != nil {
		return err
	}
	if profiles == nil {
		profiles = []config.AuthProfileSummary{}
	}
	localStatus := inspectAuthStatus(pathSet)

	styled := !globals.JSON && !globals.Raw && !globals.NoColor
	text := authListText(activeName, profiles, localStatus, styled, output.TerminalWidth())
	return writeEnvelope(globals, Envelope{
		OK:   true,
		Text: text,
		Auth: map[string]any{
			"active_profile": activeName,
			"local":          authListLocalPayload(localStatus),
			"profiles":       profiles,
		},
	})
}

func authListText(activeName string, profiles []config.AuthProfileSummary, localStatus config.AuthStatus, styled bool, width int) string {
	lines := []string{
		"Auth profiles",
		"",
		fmt.Sprintf("Active: %s", blank(firstNonEmpty(activeName, "local"))),
	}
	rows := authListRows(profiles, localStatus, styled)
	if len(rows) == 0 {
		lines = append(lines, "  (no auth profiles found)")
	} else {
		lines = append(lines, output.TableLines(output.Table{
			Width:  width,
			Indent: "  ",
			Styled: styled,
			Columns: []output.TableColumn{
				{Header: "ACTIVE", Width: 6},
				{Header: "NAME", MinWidth: 8, MaxWidth: 12},
				{Header: "STATUS", Width: 7},
				{Header: "TEAM", MinWidth: 10, MaxWidth: 20, Flex: true},
				{Header: "USER", MinWidth: 12, MaxWidth: 20, Flex: true},
				{Header: "SOURCE", Width: 7},
				{Header: "STORAGE", Width: 7},
			},
			Rows: rows,
		})...)
	}
	lines = append(lines, "", styleIf(styled, output.Dim, "Next:"), styleIf(styled, output.Cyan, "  slacky auth switch <name>"), styleIf(styled, output.Cyan, "  slacky auth status --active"))
	return strings.Join(lines, "\n")
}

func authListRows(profiles []config.AuthProfileSummary, localStatus config.AuthStatus, styled bool) [][]string {
	if len(profiles) == 0 {
		if !localStatus.ReadyForSlack {
			return nil
		}
		return [][]string{authListLocalRow(localStatus, styled)}
	}
	rows := make([][]string, 0, len(profiles))
	for index := range profiles {
		rows = append(rows, authListProfileRow(profiles[index], styled))
	}
	return rows
}

func authListProfileRow(profile config.AuthProfileSummary, styled bool) []string {
	return []string{
		profileLinePrefix(profile.Active),
		profile.Name,
		authListStatusLabel(profile.ReadyForSlack, profile.Token.State, styled),
		authListTeamCell(profile.TeamName, profile.TeamID),
		authListUserCell(profile.UserID, profile.UserName),
		authListCredentialCell(profile.CredentialSource),
		authListStorageCell(profile.Storage.Kind),
	}
}

func authListLocalRow(status config.AuthStatus, styled bool) []string {
	return []string{
		profileLinePrefix(true),
		"local",
		authListStatusLabel(status.ReadyForSlack, status.Token.State, styled),
		authListTeamCell(status.TeamName, status.TeamID),
		authListUserCell(status.UserID, status.UserName),
		authListCredentialCell(status.CredentialSource),
		authListStorageCell(status.Storage.Kind),
	}
}

func authListStatusLabel(ready bool, tokenState string, styled bool) string {
	label := compactAuthListStatus(ready, tokenState)
	switch label {
	case "ready":
		return styleIf(styled, output.Green, label)
	case "refresh":
		return styleIf(styled, output.Yellow, label)
	case "missing", "expired", "partial":
		return styleIf(styled, output.Red, label)
	default:
		return label
	}
}

func compactAuthListStatus(ready bool, tokenState string) string {
	if !ready {
		if tokenState == "incomplete" {
			return "partial"
		}
		return "missing"
	}
	switch tokenState {
	case "", "ready":
		return "ready"
	case "refresh_due":
		return "refresh"
	case "incomplete":
		return "partial"
	default:
		return tokenState
	}
}

func authListTeamCell(teamName string, teamID string) string {
	return blank(firstNonEmpty(teamName, teamID))
}

func authListUserCell(userID string, userName string) string {
	return blank(authMentionLabel(userID, userName))
}

func authListCredentialCell(source string) string {
	switch source {
	case "browser_session":
		return "browser"
	case "local_file":
		return "file"
	case "":
		return blank("")
	default:
		return source
	}
}

func authListStorageCell(kind string) string {
	switch kind {
	case "local_file":
		return "file"
	case "":
		return blank("")
	default:
		return kind
	}
}

func authListLocalPayload(status config.AuthStatus) map[string]any {
	return map[string]any{
		"name":              "local",
		"active":            status.Exists && status.ProfileName == "",
		"ready_for_slack":   status.ReadyForSlack,
		"source":            status.Source,
		"credential_source": status.CredentialSource,
		"storage":           status.Storage,
		"token":             status.Token,
		"team_id":           status.TeamID,
		"team_name":         status.TeamName,
		"user_id":           status.UserID,
		"user_name":         status.UserName,
	}
}

func (cmd *AuthSwitchCmd) Run(globals *Globals) error {
	pathSet, err := paths.Resolve()
	if err != nil {
		return err
	}
	name := strings.TrimSpace(cmd.Name)
	if name == "" {
		return missingUsage("missing auth profile name", "slacky auth switch <name>", "slacky auth list")
	}
	auth, err := config.LoadAuthProfile(pathSet.AuthProfilesDir.Path, name)
	if err != nil {
		if isMissingAuth(err) {
			return profileNotFound(name)
		}
		return err
	}
	auth.ProfileName = name
	if err := config.WriteAuth(pathSet.AuthFile.Path, auth); err != nil {
		return err
	}
	if err := config.WriteActiveProfile(pathSet.ActiveProfileFile.Path, name); err != nil {
		return err
	}
	text := strings.Join([]string{
		"Auth profile switched",
		"",
		fmt.Sprintf("Active: %s", name),
		fmt.Sprintf("Team: %s", blank(auth.TeamName)),
		fmt.Sprintf("User: %s", authDisplayLabel(auth.UserID, auth.UserName)),
		"",
		"Next:",
		"  slacky auth status",
		"  " + defaultSearchCommand(authMentionLabel(auth.UserID, auth.UserName)),
	}, "\n")
	return writeEnvelope(globals, Envelope{
		OK:   true,
		Text: text,
		Auth: authProfileSummary(name, config.AuthProfilePath(pathSet.AuthProfilesDir.Path, name), auth),
	})
}

func (cmd *AuthStatusCmd) Run(globals *Globals) error {
	pathSet, err := paths.Resolve()
	if err != nil {
		return err
	}
	authStatus := inspectAuthStatus(pathSet)
	authStatus.ActiveOnly = cmd.Active
	authUser := authenticatedUserLabel(authStatus, pathSet.CacheDB.Path)
	return writeEnvelope(globals, Envelope{
		OK:   true,
		Text: authStatusText(authStatus, authUser),
		Auth: authStatus,
	})
}

func authStatusText(status config.AuthStatus, authUser string) string {
	lines := make([]string, 0, 24)
	lines = append(
		lines,
		output.Bold("Auth"),
		"",
		authStatusRow("Status", authStatusHealthLabel(status)),
		authStatusRow("Source", authSelectedSourceLabel(status)),
		authStatusRow("Profile", authProfileStatusLabel(status)),
		authStatusRow("Team", authTeamLabel(status.TeamName, status.TeamID)),
		authStatusRow("User", authDisplayLabel(status.UserID, strings.TrimPrefix(authUser, "@"))),
		authStatusRow("Credential", blank(status.CredentialSource)),
		authStatusRow("Storage", authStorageLabel(status.Storage)),
		authStatusRow("Auth kind", blank(status.AuthKind)),
		authStatusRow("Token type", blank(status.TokenType)),
		authStatusRow("Scopes", authScopesLabel(status.Scopes)),
		authStatusRow("Token", authTokenLabel(status)),
		authStatusRow("Expires at", authExpiryLabel(status.ExpiresAtTime)),
		authStatusRow("Expires in", authExpiresInLabel(status)),
		authStatusRow("Refresh", authRefreshStatusLabel(status)),
		authStatusRow("Mixed auth fields", authMixedFieldsLabel(status.MixedAuthFields)),
		authStatusRow("File", status.Path),
		authStatusRow("Cache DB", status.CacheDBPath),
		"",
	)
	lines = append(lines, authStatusNextLines(status, authUser)...)
	return strings.Join(lines, "\n")
}

func authStatusRow(label string, value string) string {
	return fmt.Sprintf("  %s: %s", output.Dim(label), value)
}

func authStatusHealthLabel(status config.AuthStatus) string {
	if !status.ReadyForSlack {
		return output.Red("missing")
	}
	if status.Expired && !status.RefreshPossible {
		return output.Red("expired")
	}
	if status.Expired {
		return output.Yellow("expired; refresh available")
	}
	if status.RefreshDue {
		return output.Yellow("refresh due")
	}
	if len(status.MixedAuthFields) > 0 {
		return output.Yellow("ready with mixed fields")
	}
	return output.Green("ready")
}

func authSelectedSourceLabel(status config.AuthStatus) string {
	selectedBy := blank(status.SelectedBy)
	if status.Source == "" {
		return selectedBy
	}
	return fmt.Sprintf("%s (%s)", status.Source, selectedBy)
}

func authProfileStatusLabel(status config.AuthStatus) string {
	profile := blank(status.ProfileName)
	if status.ProfileName != "" && status.Active {
		return profile + " (active)"
	}
	if status.ProfileName == "" && status.Active && status.Exists {
		return profile + " (active local)"
	}
	return profile
}

func authStorageLabel(storage config.AuthStorageStatus) string {
	parts := []string{blank(storage.Kind)}
	if storage.Mode != "" {
		parts = append(parts, storage.Mode)
	}
	if storage.Path != "" {
		parts = append(parts, storage.Path)
	}
	return strings.Join(parts, ", ")
}

func authScopesLabel(scopes []string) string {
	if len(scopes) == 0 {
		return "none recorded"
	}
	return strings.Join(scopes, ", ")
}

func authTokenLabel(status config.AuthStatus) string {
	state := status.Token.State
	if state == "" {
		state = "unknown"
	}
	if status.Token.NeedsReauth {
		return output.Red(state)
	}
	if status.RefreshDue || status.Expired {
		return output.Yellow(state)
	}
	if state == "ready" {
		return output.Green(state)
	}
	return state
}

func authRefreshStatusLabel(status config.AuthStatus) string {
	if !status.RefreshPossible {
		return "unavailable"
	}
	if status.Expired {
		return "available; token expired"
	}
	if status.RefreshDue {
		return "available; due now"
	}
	return "available; not due"
}

func authStatusNextLines(status config.AuthStatus, authUser string) []string {
	if !status.ReadyForSlack {
		return []string{
			output.Dim("Next:"),
			output.Cyan("  slacky setup wizard"),
			output.Cyan("  slacky auth import"),
		}
	}
	lines := []string{
		output.Dim("Next:"),
		output.Cyan("  slacky doctor"),
		output.Cyan("  slacky auth list"),
	}
	if status.RefreshDue || status.Expired {
		lines = append(lines, output.Cyan("  slacky auth refresh"))
	}
	lines = append(lines, output.Cyan("  "+defaultSearchCommand(authUser)))
	return lines
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
			Auth:   inspectAuthStatus(pathSet),
			DryRun: true,
		})
	}
	removed, err := config.RemoveAuth(pathSet.AuthFile.Path)
	if err != nil {
		return err
	}
	if err := config.ClearActiveProfile(pathSet.ActiveProfileFile.Path); err != nil {
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
	name := strings.TrimSpace(cmd.Name)
	if name != "" {
		return cmd.runProfileLogout(globals, pathSet, name)
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
			Auth:   inspectAuthStatus(pathSet),
			DryRun: true,
		})
	}
	removed, err := config.RemoveAuth(pathSet.AuthFile.Path)
	if err != nil {
		return err
	}
	if err := config.ClearActiveProfile(pathSet.ActiveProfileFile.Path); err != nil {
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

func (cmd *AuthLogoutCmd) runProfileLogout(globals *Globals, pathSet paths.Set, name string) error {
	if err := config.ValidateAuthProfileName(name); err != nil {
		return appError("invalid_auth_profile", err.Error())
	}
	profilePath := config.AuthProfilePath(pathSet.AuthProfilesDir.Path, name)
	activeName := activeProfileName(pathSet)
	if cmd.DryRun {
		lines := []string{
			"Auth logout dry run",
			"",
			fmt.Sprintf("Would remove profile: %s", profilePath),
		}
		if activeName == name {
			lines = append(lines, fmt.Sprintf("Would remove active auth: %s", pathSet.AuthFile.Path))
		}
		lines = append(lines, "", "Apply:", "  slacky auth logout --name "+name)
		return writeEnvelope(globals, Envelope{
			OK:     true,
			Text:   strings.Join(lines, "\n"),
			Auth:   map[string]any{"profile": name, "path": profilePath, "active": activeName == name},
			DryRun: true,
		})
	}
	removed, err := config.RemoveAuthProfile(pathSet.AuthProfilesDir.Path, name)
	if err != nil {
		return err
	}
	activeRemoved := false
	if activeName == name {
		if _, err := config.RemoveAuth(pathSet.AuthFile.Path); err != nil {
			return err
		}
		if err := config.ClearActiveProfile(pathSet.ActiveProfileFile.Path); err != nil {
			return err
		}
		activeRemoved = true
	}
	text := strings.Join([]string{
		"Auth profile logout complete",
		"",
		fmt.Sprintf("Profile: %s", name),
		fmt.Sprintf("Removed profile: %t", removed),
		fmt.Sprintf("Removed active auth: %t", activeRemoved),
		"Slack token revoked: false",
		"",
		"Next:",
		"  slacky auth list",
		"  slacky auth status",
	}, "\n")
	return writeEnvelope(globals, Envelope{
		OK:   true,
		Text: text,
		Auth: map[string]any{
			"profile":        name,
			"path":           profilePath,
			"removed":        removed,
			"active_removed": activeRemoved,
			"revoked":        false,
		},
	})
}

type cacheTarget struct {
	ProfileName string
	Path        string
	Source      string
	Auth        config.Auth
}

type cacheFile struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
	Removed   bool   `json:"removed,omitempty"`
	Error     string `json:"error,omitempty"`
}

func cacheTargetForProfile(pathSet paths.Set, profileName string) (cacheTarget, error) {
	name := strings.TrimSpace(profileName)
	if name == "" {
		auth, _ := config.LoadAuth(pathSet.AuthFile.Path)
		return cacheTarget{
			ProfileName: strings.TrimSpace(auth.ProfileName),
			Path:        pathSet.CacheDB.Path,
			Source:      pathSet.CacheDB.Source,
			Auth:        auth,
		}, nil
	}
	if err := config.ValidateAuthProfileName(name); err != nil {
		return cacheTarget{}, appError("invalid_auth_profile", err.Error())
	}
	auth, err := config.LoadAuthProfile(pathSet.AuthProfilesDir.Path, name)
	if err != nil {
		if isMissingAuth(err) {
			return cacheTarget{}, profileNotFound(name)
		}
		return cacheTarget{}, err
	}
	auth.ProfileName = name
	return cacheTarget{
		ProfileName: name,
		Path:        paths.CacheDBPath(pathSet.Home.Path, name, auth.TeamID, auth.UserID),
		Source:      "profile:" + name,
		Auth:        auth,
	}, nil
}

func inspectCacheFiles(cachePath string) []cacheFile {
	files := make([]cacheFile, 0, 3)
	for _, path := range []string{cachePath, cachePath + "-wal", cachePath + "-shm"} {
		file := cacheFile{Path: path}
		if info, err := os.Stat(path); err == nil {
			file.Exists = true
			file.SizeBytes = info.Size()
		} else if !errors.Is(err, os.ErrNotExist) {
			file.Error = err.Error()
		}
		files = append(files, file)
	}
	return files
}

func cachePruneEnvelope(target cacheTarget, result store.PruneResult, status store.Status, olderThan int, dryRun bool) Envelope {
	title := "Cache prune dry run"
	applyLine := "  slacky cache prune" + cacheProfileFlag(target.ProfileName) + fmt.Sprintf(" --older-than %d --apply", olderThan)
	footerLabel := "Apply:"
	footerCommand := applyLine
	if !dryRun {
		title = "Cache prune complete"
		footerLabel = "Next:"
		footerCommand = "  slacky cache status" + cacheProfileFlag(target.ProfileName)
	}
	text := strings.Join([]string{
		title,
		"",
		fmt.Sprintf("Profile: %s", cacheProfileLabel(target.ProfileName)),
		fmt.Sprintf("Team: %s", authTeamLabel(target.Auth.TeamName, target.Auth.TeamID)),
		fmt.Sprintf("User: %s", authDisplayLabel(target.Auth.UserID, target.Auth.UserName)),
		fmt.Sprintf("DB: %s", target.Path),
		fmt.Sprintf("Older than: %d days", olderThan),
		fmt.Sprintf("Cutoff: %s", result.Cutoff),
		fmt.Sprintf("Messages: %d", result.Messages),
		fmt.Sprintf("Message mentions: %d", result.MessageMentions),
		fmt.Sprintf("FTS rows: %d", result.MessagesFTS),
		fmt.Sprintf("Thread links: %d", result.ThreadMessages),
		fmt.Sprintf("Threads: %d", result.Threads),
		"",
		footerLabel,
		footerCommand,
	}, "\n")
	payload := cacheTargetPayload(target)
	payload["prune"] = result
	return Envelope{
		OK:     true,
		Text:   text,
		Source: "cache",
		Cache:  map[string]any{"target": payload, "status": status},
		DryRun: dryRun,
	}
}

func cacheClearPayload(target cacheTarget, files []cacheFile) map[string]any {
	payload := cacheTargetPayload(target)
	payload["files"] = files
	return payload
}

func cacheTargetPayload(target cacheTarget) map[string]any {
	return map[string]any{
		"profile":   target.ProfileName,
		"source":    target.Source,
		"path":      target.Path,
		"team_id":   target.Auth.TeamID,
		"team_name": target.Auth.TeamName,
		"user_id":   target.Auth.UserID,
		"user_name": target.Auth.UserName,
	}
}

func countRemovedCacheFiles(files []cacheFile) int {
	count := 0
	for _, file := range files {
		if file.Removed {
			count++
		}
	}
	return count
}

func cacheProfileLabel(profileName string) string {
	if strings.TrimSpace(profileName) == "" {
		return "(legacy local)"
	}
	return profileName
}

func cacheProfileFlag(profileName string) string {
	if strings.TrimSpace(profileName) == "" {
		return ""
	}
	return " --profile " + profileName
}

func cacheClearApplyCommand(profileName string) string {
	return "  slacky cache clear" + cacheProfileFlag(profileName) + " --apply"
}

func authTeamLabel(teamName string, teamID string) string {
	if teamName != "" && teamID != "" {
		return fmt.Sprintf("%s (%s)", teamName, teamID)
	}
	if teamName != "" {
		return teamName
	}
	if teamID != "" {
		return teamID
	}
	return blank("")
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

func slackReachability(globals *Globals, pathSet paths.Set, authPath string, authStatus config.AuthStatus) SlackReachability {
	reachability := SlackReachability{
		Method: "auth.test",
	}
	if !authStatus.ReadyForSlack {
		reachability.Skipped = true
		reachability.Label = "skipped, missing auth"
		return reachability
	}
	var auth config.Auth
	var err error
	if authPath == pathSet.AuthFile.Path {
		auth, err = refreshActiveAuthIfDue(globals, pathSet)
	} else {
		auth, err = config.LoadAuth(authPath)
	}
	if err != nil {
		reachability.Label = "auth file unreadable"
		reachability.Error = err.Error()
		return reachability
	}
	ctx, cancel := context.WithTimeout(context.Background(), globals.Timeout)
	defer cancel()
	result, err := apiClientFromAuth(globals, auth).AuthTest(ctx)
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

func authRefreshDiagnosticOK(status config.AuthStatus) bool {
	if !status.ReadyForSlack || status.AuthKind != config.AuthKindOAuthUser || status.ExpiresAtTime.IsZero() {
		return true
	}
	if status.Expired {
		return false
	}
	return status.RefreshPossible
}

func authRefreshDiagnosticLabel(status config.AuthStatus) string {
	if !status.ReadyForSlack {
		return "skipped, missing auth"
	}
	if status.AuthKind != config.AuthKindOAuthUser || status.ExpiresAtTime.IsZero() {
		return "not rotating"
	}
	if status.Expired {
		return "expired"
	}
	if !status.RefreshPossible {
		return "unavailable"
	}
	if status.RefreshDue {
		return "due"
	}
	return "ready"
}

func authMixedFieldsLabel(fields []string) string {
	if len(fields) == 0 {
		return "clean"
	}
	return "mixed: " + strings.Join(fields, ", ")
}

func authExpiresInLabel(status config.AuthStatus) string {
	if status.ExpiresAtTime.IsZero() {
		return "(none)"
	}
	if status.Expired {
		return "expired " + durationSecondsLabel(status.ExpiredAgoSeconds) + " ago"
	}
	return durationSecondsLabel(status.ExpiresInSeconds)
}

func durationSecondsLabel(seconds int64) string {
	const maxDurationSeconds = int64((1<<63 - 1) / 1_000_000_000)
	if seconds < 0 {
		seconds = 0
	}
	if seconds > maxDurationSeconds {
		return ">=" + (time.Duration(maxDurationSeconds) * time.Second).String()
	}
	return (time.Duration(seconds) * time.Second).String()
}
