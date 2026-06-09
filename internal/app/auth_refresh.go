package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/andyhtran/slacky/internal/api"
	"github.com/andyhtran/slacky/internal/config"
	"github.com/andyhtran/slacky/internal/paths"
)

var refreshOAuthToken = api.RefreshUserToken

type authRefreshTarget struct {
	ProfileName string
	Auth        config.Auth
	Active      bool
	Path        string
}

type authRefreshResult struct {
	Profile              string    `json:"profile,omitempty"`
	Path                 string    `json:"path"`
	Active               bool      `json:"active"`
	AuthKind             string    `json:"auth_kind,omitempty"`
	Refreshed            bool      `json:"refreshed"`
	SkippedReason        string    `json:"skipped_reason,omitempty"`
	ExpiresAt            time.Time `json:"expires_at,omitempty"`
	ExpiresInSeconds     int64     `json:"expires_in_seconds,omitempty"`
	ExpiredAgoSeconds    int64     `json:"expired_ago_seconds,omitempty"`
	Expired              bool      `json:"expired"`
	RefreshDue           bool      `json:"refresh_due"`
	RefreshPossible      bool      `json:"refresh_possible"`
	RefreshWindowSeconds int64     `json:"refresh_window_seconds"`
	TeamID               string    `json:"team_id,omitempty"`
	TeamName             string    `json:"team_name,omitempty"`
	UserID               string    `json:"user_id,omitempty"`
	UserName             string    `json:"user_name,omitempty"`
}

type authRefreshLock struct {
	file *os.File
}

func (cmd *AuthRefreshCmd) Run(globals *Globals) error {
	pathSet, err := paths.Resolve()
	if err != nil {
		return err
	}
	auth, result, err := refreshAuth(globals, pathSet, cmd.Profile, cmd.Force, true)
	if err != nil {
		return err
	}
	result = authRefreshResultFromAuth(result.Profile, result.Path, result.Active, auth, result.Refreshed, result.SkippedReason)

	state := "skipped"
	if result.Refreshed {
		state = "refreshed"
	}
	lines := []string{
		"Auth refresh",
		"",
		fmt.Sprintf("Profile: %s", blank(result.Profile)),
		fmt.Sprintf("State: %s", state),
		fmt.Sprintf("Refresh possible: %t", result.RefreshPossible),
		fmt.Sprintf("Refresh due: %t", result.RefreshDue),
		fmt.Sprintf("Expires at: %s", authExpiryLabel(result.ExpiresAt)),
	}
	if result.SkippedReason != "" {
		lines = append(lines, fmt.Sprintf("Skipped: %s", result.SkippedReason))
	}
	lines = append(lines, "", "Next:", "  slacky auth status", "  slacky doctor")
	return writeEnvelope(globals, Envelope{
		OK:   true,
		Text: strings.Join(lines, "\n"),
		Auth: result,
	})
}

func refreshActiveAuthIfDue(globals *Globals, pathSet paths.Set) (config.Auth, error) {
	auth, _, err := refreshAuth(globals, pathSet, "", false, false)
	return auth, err
}

func refreshAuth(globals *Globals, pathSet paths.Set, profileName string, force bool, explicit bool) (config.Auth, authRefreshResult, error) {
	target, err := loadAuthRefreshTarget(pathSet, profileName)
	if err != nil {
		return config.Auth{}, authRefreshResult{}, err
	}
	now := time.Now()
	result := authRefreshResultFromAuth(target.ProfileName, target.Path, target.Active, target.Auth, false, "")
	if !shouldRefreshAuth(target.Auth, now, force, explicit) {
		result.SkippedReason = authRefreshSkipReason(target.Auth, now, force)
		if refreshUnavailableMustError(target.Auth, now, explicit) {
			return config.Auth{}, result, refreshUnavailableError(target.ProfileName, target.Auth)
		}
		return target.Auth, result, nil
	}
	if !target.Auth.RefreshPossible() {
		return config.Auth{}, result, refreshUnavailableError(target.ProfileName, target.Auth)
	}

	lock, err := acquireAuthRefreshLock(authRefreshLockPath(pathSet, target.ProfileName))
	if err != nil {
		return config.Auth{}, result, err
	}
	defer func() {
		_ = lock.Close()
	}()

	target, err = loadAuthRefreshTarget(pathSet, target.ProfileName)
	if err != nil {
		return config.Auth{}, result, err
	}
	now = time.Now()
	result = authRefreshResultFromAuth(target.ProfileName, target.Path, target.Active, target.Auth, false, "")
	if !shouldRefreshAuth(target.Auth, now, force, explicit) {
		result.SkippedReason = authRefreshSkipReason(target.Auth, now, force)
		return target.Auth, result, nil
	}
	if !target.Auth.RefreshPossible() {
		return config.Auth{}, result, refreshUnavailableError(target.ProfileName, target.Auth)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globals.Timeout)
	defer cancel()
	token, err := refreshOAuthToken(ctx, &http.Client{Timeout: globals.Timeout}, api.OAuthRefreshRequest{
		ClientID:     target.Auth.ClientID,
		ClientSecret: target.Auth.ClientSecret,
		RefreshToken: target.Auth.RefreshToken,
	}, "slacky/"+appVersion)
	if err != nil {
		return config.Auth{}, result, authRefreshAPIError(target.ProfileName, target.Auth, err)
	}
	if token.AccessToken == "" || token.RefreshToken == "" {
		return config.Auth{}, result, appError("oauth_refresh_failed", "Slack OAuth refresh response did not include a new access token and refresh token")
	}
	refreshedAuth := applyRefreshedOAuthToken(target.Auth, token)
	storedPath, active, err := writeRefreshedAuth(pathSet, target.ProfileName, refreshedAuth)
	if err != nil {
		return config.Auth{}, result, err
	}
	result = authRefreshResultFromAuth(target.ProfileName, storedPath, active, refreshedAuth, true, "")
	return refreshedAuth, result, nil
}

func loadAuthRefreshTarget(pathSet paths.Set, profileName string) (authRefreshTarget, error) {
	requestedProfile := strings.TrimSpace(profileName)
	activeName := activeProfileName(pathSet)
	if requestedProfile != "" {
		if err := config.ValidateAuthProfileName(requestedProfile); err != nil {
			return authRefreshTarget{}, appError("invalid_auth_profile", err.Error())
		}
		auth, err := config.LoadAuthProfile(pathSet.AuthProfilesDir.Path, requestedProfile)
		if err != nil {
			if isMissingAuth(err) {
				return authRefreshTarget{}, profileNotFound(requestedProfile)
			}
			return authRefreshTarget{}, err
		}
		auth.ProfileName = requestedProfile
		return authRefreshTarget{
			ProfileName: requestedProfile,
			Auth:        auth,
			Active:      activeName == requestedProfile,
			Path:        config.AuthProfilePath(pathSet.AuthProfilesDir.Path, requestedProfile),
		}, nil
	}
	if activeName != "" {
		auth, err := config.LoadAuthProfile(pathSet.AuthProfilesDir.Path, activeName)
		if err != nil {
			if !isMissingAuth(err) {
				return authRefreshTarget{}, err
			}
			auth, err = config.LoadAuth(pathSet.AuthFile.Path)
			if err != nil {
				return authRefreshTarget{}, err
			}
		}
		auth.ProfileName = activeName
		return authRefreshTarget{
			ProfileName: activeName,
			Auth:        auth,
			Active:      true,
			Path:        config.AuthProfilePath(pathSet.AuthProfilesDir.Path, activeName),
		}, nil
	}
	auth, err := config.LoadAuth(pathSet.AuthFile.Path)
	if err != nil {
		if isMissingAuth(err) {
			return authRefreshTarget{}, missingAuthError(pathSet.AuthFile.Path)
		}
		return authRefreshTarget{}, err
	}
	auth.ProfileName = ""
	return authRefreshTarget{
		Auth:   auth,
		Active: true,
		Path:   pathSet.AuthFile.Path,
	}, nil
}

func shouldRefreshAuth(auth config.Auth, now time.Time, force bool, explicit bool) bool {
	if !auth.RefreshPossible() {
		return false
	}
	if force {
		return true
	}
	if auth.ExpiresAt.IsZero() {
		return explicit
	}
	return auth.RefreshDue(now, config.DefaultRefreshWindow)
}

func refreshUnavailableMustError(auth config.Auth, now time.Time, explicit bool) bool {
	if auth.RefreshPossible() {
		return false
	}
	if explicit {
		return true
	}
	return auth.EffectiveAuthKind() == config.AuthKindOAuthUser && !auth.ExpiresAt.IsZero() && auth.RefreshDue(now, config.DefaultRefreshWindow)
}

func authRefreshSkipReason(auth config.Auth, now time.Time, force bool) string {
	if force {
		return ""
	}
	if !auth.RefreshPossible() {
		return "refresh is not available for this auth profile"
	}
	if auth.ExpiresAt.IsZero() {
		return "token has no expiry"
	}
	if !auth.RefreshDue(now, config.DefaultRefreshWindow) {
		return "token is not near expiry"
	}
	return ""
}

func refreshUnavailableError(profileName string, auth config.Auth) error {
	err := appError("oauth_refresh_unavailable", "OAuth refresh is not available for this auth profile")
	if auth.EffectiveAuthKind() == config.AuthKindBrowserSession {
		err.Message = "browser-session auth cannot be refreshed with OAuth"
		err.SuggestedCommands = []string{"slacky auth import-session --wizard --name " + refreshProfilePlaceholder(profileName)}
		return err
	}
	err.Suggestions = []string{
		"Use OAuth login again if this profile should be OAuth-backed.",
		"Use browser-session import only when Slack app installation is blocked.",
	}
	err.SuggestedCommands = []string{authRefreshRecoveryCommand(profileName, auth.ClientID)}
	return err
}

func authRefreshAPIError(profileName string, auth config.Auth, err error) error {
	var oauthErr api.OAuthError
	if errors.As(err, &oauthErr) && oauthErr.SlackError == "invalid_refresh_token" {
		appErr := appError("invalid_refresh_token", "Slack rejected the stored refresh token. Refresh tokens are single-use; run OAuth login again for this profile.")
		appErr.SuggestedCommands = []string{authRefreshRecoveryCommand(profileName, auth.ClientID)}
		return appErr
	}
	return appError("oauth_refresh_failed", err.Error())
}

func authRefreshRecoveryCommand(profileName string, clientID string) string {
	parts := []string{"slacky auth login"}
	if strings.TrimSpace(profileName) != "" {
		parts = append(parts, "--name "+strings.TrimSpace(profileName))
	}
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = "<client-id>"
	}
	parts = append(parts, "--client-id "+clientID)
	return strings.Join(parts, " ")
}

func refreshProfilePlaceholder(profileName string) string {
	if strings.TrimSpace(profileName) == "" {
		return "<name>"
	}
	return strings.TrimSpace(profileName)
}

func applyRefreshedOAuthToken(auth config.Auth, token api.OAuthToken) config.Auth {
	auth.AuthKind = config.AuthKindOAuthUser
	auth.UserToken = token.AccessToken
	auth.RefreshToken = token.RefreshToken
	auth.SessionCookieD = ""
	auth.BrowserUserAgent = ""
	if token.TokenType != "" {
		auth.TokenType = token.TokenType
	}
	if !token.ExpiresAt.IsZero() {
		auth.ExpiresAt = token.ExpiresAt
	}
	if token.TeamID != "" {
		auth.TeamID = token.TeamID
	}
	if token.TeamName != "" {
		auth.TeamName = token.TeamName
	}
	if token.UserID != "" {
		auth.UserID = token.UserID
	}
	if len(token.Scopes) > 0 {
		auth.Scopes = append([]string(nil), token.Scopes...)
	}
	return auth
}

func writeRefreshedAuth(pathSet paths.Set, profileName string, auth config.Auth) (string, bool, error) {
	activeName := activeProfileName(pathSet)
	if strings.TrimSpace(profileName) == "" {
		auth.ProfileName = ""
		if err := config.WriteAuth(pathSet.AuthFile.Path, auth); err != nil {
			return "", false, err
		}
		if err := config.ClearActiveProfile(pathSet.ActiveProfileFile.Path); err != nil {
			return "", false, err
		}
		return pathSet.AuthFile.Path, true, nil
	}
	profileName = strings.TrimSpace(profileName)
	auth.ProfileName = profileName
	if err := config.WriteAuthProfile(pathSet.AuthProfilesDir.Path, profileName, auth); err != nil {
		return "", false, err
	}
	active := activeName == profileName
	if active {
		if err := config.WriteAuth(pathSet.AuthFile.Path, auth); err != nil {
			return "", false, err
		}
		if err := config.WriteActiveProfile(pathSet.ActiveProfileFile.Path, profileName); err != nil {
			return "", false, err
		}
	}
	return config.AuthProfilePath(pathSet.AuthProfilesDir.Path, profileName), active, nil
}

func authRefreshResultFromAuth(profileName string, path string, active bool, auth config.Auth, refreshed bool, skippedReason string) authRefreshResult {
	now := time.Now()
	return authRefreshResult{
		Profile:              profileName,
		Path:                 path,
		Active:               active,
		AuthKind:             auth.EffectiveAuthKind(),
		Refreshed:            refreshed,
		SkippedReason:        skippedReason,
		ExpiresAt:            auth.ExpiresAt,
		ExpiresInSeconds:     auth.ExpiresInSeconds(now),
		ExpiredAgoSeconds:    auth.ExpiredAgoSeconds(now),
		Expired:              auth.Expired(now),
		RefreshDue:           auth.RefreshDue(now, config.DefaultRefreshWindow),
		RefreshPossible:      auth.RefreshPossible(),
		RefreshWindowSeconds: int64(config.DefaultRefreshWindow / time.Second),
		TeamID:               auth.TeamID,
		TeamName:             auth.TeamName,
		UserID:               auth.UserID,
		UserName:             auth.UserName,
	}
}

func authExpiryLabel(value time.Time) string {
	if value.IsZero() {
		return "(none)"
	}
	return value.Format(time.RFC3339)
}

func authRefreshLockPath(pathSet paths.Set, profileName string) string {
	lockName := strings.TrimSpace(profileName)
	if lockName == "" {
		lockName = "local"
	}
	return filepath.Join(pathSet.StateDir.Path, "locks", "auth-refresh-"+lockName+".lock")
}

func acquireAuthRefreshLock(path string) (*authRefreshLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &authRefreshLock{file: file}, nil
}

func (lock *authRefreshLock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN)
	closeErr := lock.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
