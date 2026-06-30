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
)

const defaultBrowserSessionUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"

const browserSessionUserAgentEnv = "SLACKY_BROWSER_USER_AGENT"

var newAPIClient = api.NewClient

func activeProfileName(pathSet paths.Set) string {
	if auth, err := config.LoadAuth(pathSet.AuthFile.Path); err == nil && auth.ProfileName != "" {
		return auth.ProfileName
	}
	name, err := config.ReadActiveProfile(pathSet.ActiveProfileFile.Path)
	if err != nil {
		return ""
	}
	return name
}

func inspectAuthStatus(pathSet paths.Set) config.AuthStatus {
	status := config.InspectAuth(pathSet.AuthFile.Path)
	status.CacheDBPath = pathSet.CacheDB.Path
	activeName := activeProfileName(pathSet)
	status.ActiveProfileName = activeName
	if status.Exists {
		status.Active = true
		if status.ProfileName != "" {
			status.Source = "profile"
			status.SelectedBy = "active_profile"
			status.ActiveProfileName = status.ProfileName
		}
	}
	return status
}

func loadExistingAuthForProfile(pathSet paths.Set, name string) config.Auth {
	name = strings.TrimSpace(name)
	if name != "" {
		if auth, err := config.LoadAuthProfile(pathSet.AuthProfilesDir.Path, name); err == nil {
			return auth
		}
		return config.Auth{}
	}
	auth, _ := config.LoadAuth(pathSet.AuthFile.Path)
	return auth
}

func writeSelectedAuth(pathSet paths.Set, name string, auth config.Auth) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		auth.ProfileName = ""
		if err := config.WriteAuth(pathSet.AuthFile.Path, auth); err != nil {
			return "", err
		}
		if err := config.ClearActiveProfile(pathSet.ActiveProfileFile.Path); err != nil {
			return "", err
		}
		return pathSet.AuthFile.Path, nil
	}
	if err := config.ValidateAuthProfileName(name); err != nil {
		return "", appError("invalid_auth_profile", err.Error())
	}
	auth.ProfileName = name
	if err := config.WriteAuthProfile(pathSet.AuthProfilesDir.Path, name, auth); err != nil {
		return "", err
	}
	if err := config.WriteAuth(pathSet.AuthFile.Path, auth); err != nil {
		return "", err
	}
	if err := config.WriteActiveProfile(pathSet.ActiveProfileFile.Path, name); err != nil {
		return "", err
	}
	return config.AuthProfilePath(pathSet.AuthProfilesDir.Path, name), nil
}

func oauthUserAuth(clientID string, clientSecret string, redirectURI string, token api.OAuthToken) config.Auth {
	return config.Auth{
		AuthKind:     config.AuthKindOAuthUser,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURI:  redirectURI,
		UserToken:    token.AccessToken,
		TokenType:    token.TokenType,
		UserID:       token.UserID,
		TeamID:       token.TeamID,
		TeamName:     token.TeamName,
		Scopes:       append([]string(nil), token.Scopes...),
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}
}

func importedUserTokenAuth(token string) config.Auth {
	return config.Auth{
		AuthKind:  config.AuthKindImportedUserToken,
		UserToken: token,
		Scopes:    []string{},
		TokenType: importedTokenType(token),
	}
}

func browserSessionAuth(xoxc string, xoxd string, userAgent string) config.Auth {
	return config.Auth{
		AuthKind:         config.AuthKindBrowserSession,
		UserToken:        xoxc,
		SessionCookieD:   xoxd,
		BrowserUserAgent: userAgent,
		TokenType:        config.TokenTypeBrowserSession,
		Scopes:           []string{},
	}
}

func browserSessionUserAgent(flagValue string) (string, string) {
	if value := strings.TrimSpace(flagValue); value != "" {
		return value, "flag"
	}
	if value := strings.TrimSpace(os.Getenv(browserSessionUserAgentEnv)); value != "" {
		return value, "env:" + browserSessionUserAgentEnv
	}
	return defaultBrowserSessionUserAgent, "default"
}

func apiClientFromAuth(globals *Globals, auth config.Auth) *api.Client {
	client := newAPIClient(auth.UserToken, appVersion, globals.Timeout, globals.MaxRateLimitWait)
	if auth.SessionCookieD != "" {
		client.SessionCookieD = auth.SessionCookieD
	}
	if auth.BrowserUserAgent != "" {
		client.UserAgent = auth.BrowserUserAgent
	}
	return client
}

func authTest(globals *Globals, auth config.Auth) (api.AuthTestResult, error) {
	ctx, cancel := contextWithTimeout(globals)
	defer cancel()
	result, err := apiClientFromAuth(globals, auth).AuthTest(ctx)
	if err != nil {
		return api.AuthTestResult{}, tokenValidationError(err)
	}
	if result.BotID != "" {
		return api.AuthTestResult{}, appError("invalid_token_type", "provided token is a Slack bot token; slacky requires a user token")
	}
	return result, nil
}

func authProfileSummary(profileName string, storedPath string, auth config.Auth) map[string]any {
	summary := map[string]any{
		"path":               storedPath,
		"profile":            profileName,
		"auth_kind":          auth.EffectiveAuthKind(),
		"team_id":            auth.TeamID,
		"team_name":          auth.TeamName,
		"user_id":            auth.UserID,
		"user_name":          auth.UserName,
		"token_type":         auth.TokenType,
		"scopes":             auth.Scopes,
		"has_user_token":     auth.UserToken != "",
		"has_session_cookie": auth.SessionCookieD != "",
		"has_refresh_token":  auth.RefreshToken != "",
		"refresh_possible":   auth.RefreshPossible(),
		"refresh_due":        auth.RefreshDue(time.Now(), config.DefaultRefreshWindow),
	}
	if !auth.ExpiresAt.IsZero() {
		summary["expires_at"] = auth.ExpiresAt.Format(time.RFC3339)
	}
	return summary
}

func profileLinePrefix(active bool) string {
	if active {
		return "*"
	}
	return " "
}

func contextWithTimeout(globals *Globals) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), globals.Timeout)
}

func profileNotFound(name string) error {
	err := appError("auth_profile_not_found", fmt.Sprintf("auth profile %q was not found", name))
	err.SuggestedCommands = []string{"slacky auth list", "slacky auth import --name <name>", "slacky auth import-session --wizard --name <name>"}
	return err
}

func isMissingAuth(err error) bool {
	return errors.Is(err, config.ErrMissingAuth)
}
