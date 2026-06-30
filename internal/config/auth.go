package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	AuthKindOAuthUser         = "oauth_user"
	AuthKindImportedUserToken = "imported_user_token"
	AuthKindBrowserSession    = "browser_session"
	TokenTypeBrowserSession   = "browser_session"
	DefaultRefreshWindow      = 20 * time.Minute
)

type Auth struct {
	ProfileName      string    `json:"profile_name,omitempty"`
	AuthKind         string    `json:"auth_kind,omitempty"`
	ClientID         string    `json:"client_id,omitempty"`
	ClientSecret     string    `json:"client_secret,omitempty"`
	RedirectURI      string    `json:"redirect_uri,omitempty"`
	UserToken        string    `json:"user_token,omitempty"`
	SessionCookieD   string    `json:"session_cookie_d,omitempty"`
	BrowserUserAgent string    `json:"browser_user_agent,omitempty"`
	UserID           string    `json:"user_id,omitempty"`
	UserName         string    `json:"user_name,omitempty"`
	TeamID           string    `json:"team_id,omitempty"`
	TeamName         string    `json:"team_name,omitempty"`
	Scopes           []string  `json:"scopes,omitempty"`
	TokenType        string    `json:"token_type,omitempty"`
	ExpiresAt        time.Time `json:"expires_at,omitempty"`
	RefreshToken     string    `json:"refresh_token,omitempty"`
}

type AuthStorageStatus struct {
	Kind                 string `json:"kind"`
	Path                 string `json:"path,omitempty"`
	Mode                 string `json:"mode,omitempty"`
	DirMode              string `json:"dir_mode,omitempty"`
	SecretValuesRedacted bool   `json:"secret_values_redacted"`
}

type AuthTokenStatus struct {
	Present              bool   `json:"present"`
	State                string `json:"state"`
	ExpiresAt            string `json:"expires_at,omitempty"`
	ExpiresInSeconds     int64  `json:"expires_in_seconds,omitempty"`
	ExpiredAgoSeconds    int64  `json:"expired_ago_seconds,omitempty"`
	Expired              bool   `json:"expired"`
	RefreshDue           bool   `json:"refresh_due"`
	RefreshPossible      bool   `json:"refresh_possible"`
	RefreshWindowSeconds int64  `json:"refresh_window_seconds,omitempty"`
	NeedsReauth          bool   `json:"needs_reauth"`
}

type AuthStatus struct {
	Path                 string            `json:"path"`
	Exists               bool              `json:"exists"`
	Readable             bool              `json:"readable"`
	ReadyForSlack        bool              `json:"ready_for_slack"`
	Active               bool              `json:"active"`
	ActiveOnly           bool              `json:"active_only,omitempty"`
	ActiveProfileName    string            `json:"active_profile_name,omitempty"`
	Source               string            `json:"source"`
	SelectedBy           string            `json:"selected_by"`
	CredentialSource     string            `json:"credential_source"`
	Storage              AuthStorageStatus `json:"storage"`
	Token                AuthTokenStatus   `json:"token"`
	FileMode             string            `json:"file_mode,omitempty"`
	DirMode              string            `json:"dir_mode,omitempty"`
	PresentFields        []string          `json:"present_fields,omitempty"`
	MissingFields        []string          `json:"missing_fields,omitempty"`
	HasClientID          bool              `json:"has_client_id"`
	HasClientSecret      bool              `json:"has_client_secret"`
	HasRedirectURI       bool              `json:"has_redirect_uri"`
	HasUserToken         bool              `json:"has_user_token"`
	HasSessionCookie     bool              `json:"has_session_cookie"`
	HasBrowserUA         bool              `json:"has_browser_user_agent"`
	HasRefreshToken      bool              `json:"has_refresh_token"`
	ProfileName          string            `json:"profile_name,omitempty"`
	AuthKind             string            `json:"auth_kind,omitempty"`
	CacheDBPath          string            `json:"cache_db_path,omitempty"`
	TokenType            string            `json:"token_type,omitempty"`
	TeamID               string            `json:"team_id,omitempty"`
	TeamName             string            `json:"team_name,omitempty"`
	UserID               string            `json:"user_id,omitempty"`
	UserName             string            `json:"user_name,omitempty"`
	Scopes               []string          `json:"scopes,omitempty"`
	ExpiresAt            string            `json:"expires_at,omitempty"`
	ExpiresAtTime        time.Time         `json:"-"`
	ExpiresInSeconds     int64             `json:"expires_in_seconds,omitempty"`
	ExpiredAgoSeconds    int64             `json:"expired_ago_seconds,omitempty"`
	Expired              bool              `json:"expired"`
	RefreshDue           bool              `json:"refresh_due"`
	RefreshPossible      bool              `json:"refresh_possible"`
	RefreshWindowSeconds int64             `json:"refresh_window_seconds,omitempty"`
	MixedAuthFields      []string          `json:"mixed_auth_fields,omitempty"`
	RecoveryCommands     []string          `json:"recovery_commands,omitempty"`
	Error                string            `json:"error,omitempty"`
}

var ErrMissingAuth = errors.New("missing Slack auth file")

func LoadAuth(path string) (Auth, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Auth{}, ErrMissingAuth
		}
		return Auth{}, err
	}
	var auth Auth
	if err := json.Unmarshal(data, &auth); err != nil {
		return Auth{}, err
	}
	return auth, nil
}

func (auth Auth) IsBrowserSession() bool {
	return auth.AuthKind == AuthKindBrowserSession || auth.TokenType == TokenTypeBrowserSession || strings.HasPrefix(strings.ToLower(auth.UserToken), "xoxc-")
}

func (auth Auth) ReadyForSlack() bool {
	if auth.UserToken == "" {
		return false
	}
	if auth.IsBrowserSession() {
		return auth.SessionCookieD != ""
	}
	return true
}

func (auth Auth) EffectiveAuthKind() string {
	if strings.TrimSpace(auth.AuthKind) != "" {
		return strings.TrimSpace(auth.AuthKind)
	}
	if auth.IsBrowserSession() {
		return AuthKindBrowserSession
	}
	if auth.RefreshToken != "" || !auth.ExpiresAt.IsZero() {
		return AuthKindOAuthUser
	}
	if auth.UserToken != "" {
		return AuthKindImportedUserToken
	}
	return ""
}

func (auth Auth) RefreshPossible() bool {
	return !auth.IsBrowserSession() && auth.UserToken != "" && auth.ClientID != "" && auth.RefreshToken != ""
}

func (auth Auth) RefreshDue(now time.Time, window time.Duration) bool {
	if !auth.RefreshPossible() || auth.ExpiresAt.IsZero() {
		return false
	}
	return !now.Add(window).Before(auth.ExpiresAt)
}

func (auth Auth) Expired(now time.Time) bool {
	return !auth.ExpiresAt.IsZero() && !now.Before(auth.ExpiresAt)
}

func (auth Auth) ExpiresInSeconds(now time.Time) int64 {
	if auth.ExpiresAt.IsZero() {
		return 0
	}
	remaining := int64(auth.ExpiresAt.Sub(now).Round(time.Second) / time.Second)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (auth Auth) ExpiredAgoSeconds(now time.Time) int64 {
	if auth.ExpiresAt.IsZero() || now.Before(auth.ExpiresAt) {
		return 0
	}
	return int64(now.Sub(auth.ExpiresAt).Round(time.Second) / time.Second)
}

func (auth Auth) MixedAuthFields() []string {
	var fields []string
	add := func(name string, present bool) {
		if present {
			fields = append(fields, name)
		}
	}
	switch auth.EffectiveAuthKind() {
	case AuthKindBrowserSession:
		add("client_id", auth.ClientID != "")
		add("client_secret", auth.ClientSecret != "")
		add("redirect_uri", auth.RedirectURI != "")
		add("refresh_token", auth.RefreshToken != "")
		add("expires_at", !auth.ExpiresAt.IsZero())
	case AuthKindOAuthUser:
		add("session_cookie_d", auth.SessionCookieD != "")
		add("browser_user_agent", auth.BrowserUserAgent != "")
	case AuthKindImportedUserToken:
		add("client_id", auth.ClientID != "")
		add("client_secret", auth.ClientSecret != "")
		add("redirect_uri", auth.RedirectURI != "")
		add("session_cookie_d", auth.SessionCookieD != "")
		add("browser_user_agent", auth.BrowserUserAgent != "")
		add("refresh_token", auth.RefreshToken != "")
		add("expires_at", !auth.ExpiresAt.IsZero())
	}
	return fields
}

func InspectAuth(path string) AuthStatus {
	status := AuthStatus{
		Path:             path,
		Source:           "missing",
		SelectedBy:       "missing",
		CredentialSource: "missing",
		Storage:          authStorageStatus("missing", path, "", ""),
		Token:            AuthTokenStatus{State: "missing", NeedsReauth: true},
		RecoveryCommands: []string{
			"slacky setup wizard",
			"slacky setup steps",
			"slacky setup manifest",
			"slacky auth login",
			"slacky auth import",
			"slacky auth import-session",
			"slacky auth list",
		},
	}

	if dirInfo, err := os.Stat(filepath.Dir(path)); err == nil {
		status.DirMode = dirInfo.Mode().Perm().String()
		status.Storage.DirMode = status.DirMode
	}

	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			status.MissingFields = []string{"user_token"}
			return status
		}
		status.Error = err.Error()
		return status
	}
	status.Exists = true
	status.FileMode = info.Mode().Perm().String()
	status.Source = "local_file"
	status.SelectedBy = "local_auth_file"
	status.Storage = authStorageStatus("local_file", path, status.FileMode, status.DirMode)

	auth, err := LoadAuth(path)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Readable = true
	status.HasClientID = auth.ClientID != ""
	status.HasClientSecret = auth.ClientSecret != ""
	status.HasRedirectURI = auth.RedirectURI != ""
	status.HasUserToken = auth.UserToken != ""
	status.HasSessionCookie = auth.SessionCookieD != ""
	status.HasBrowserUA = auth.BrowserUserAgent != ""
	status.HasRefreshToken = auth.RefreshToken != ""
	status.ProfileName = auth.ProfileName
	status.AuthKind = auth.EffectiveAuthKind()
	status.TokenType = auth.TokenType
	status.TeamID = auth.TeamID
	status.TeamName = auth.TeamName
	status.UserID = auth.UserID
	status.UserName = auth.UserName
	status.Scopes = append([]string(nil), auth.Scopes...)
	status.ReadyForSlack = auth.ReadyForSlack()
	status.CredentialSource = authCredentialSource(auth)
	if auth.ProfileName != "" {
		status.Source = "profile"
		status.SelectedBy = "active_profile"
	}
	status.ExpiresAtTime = auth.ExpiresAt
	if !auth.ExpiresAt.IsZero() {
		status.ExpiresAt = auth.ExpiresAt.Format(time.RFC3339)
	}
	now := time.Now()
	status.ExpiresInSeconds = auth.ExpiresInSeconds(now)
	status.ExpiredAgoSeconds = auth.ExpiredAgoSeconds(now)
	status.Expired = auth.Expired(now)
	status.RefreshPossible = auth.RefreshPossible()
	status.RefreshDue = auth.RefreshDue(now, DefaultRefreshWindow)
	status.RefreshWindowSeconds = int64(DefaultRefreshWindow / time.Second)
	status.MixedAuthFields = auth.MixedAuthFields()
	status.Token = authTokenStatus(auth, status, now)

	addField := func(name string, present bool) {
		if present {
			status.PresentFields = append(status.PresentFields, name)
			return
		}
		status.MissingFields = append(status.MissingFields, name)
	}
	addField("client_id", status.HasClientID)
	addField("client_secret", status.HasClientSecret)
	addField("redirect_uri", status.HasRedirectURI)
	addField("user_token", status.HasUserToken)
	if auth.IsBrowserSession() {
		addField("session_cookie_d", status.HasSessionCookie)
	}

	return status
}

func authStorageStatus(kind string, path string, mode string, dirMode string) AuthStorageStatus {
	return AuthStorageStatus{
		Kind:                 kind,
		Path:                 path,
		Mode:                 mode,
		DirMode:              dirMode,
		SecretValuesRedacted: true,
	}
}

func authCredentialSource(auth Auth) string {
	if !auth.ReadyForSlack() {
		return "missing"
	}
	if auth.IsBrowserSession() {
		return "browser_session"
	}
	return "local_file"
}

func authTokenStatus(auth Auth, status AuthStatus, now time.Time) AuthTokenStatus {
	token := AuthTokenStatus{
		Present:              status.HasUserToken,
		ExpiresAt:            status.ExpiresAt,
		ExpiresInSeconds:     status.ExpiresInSeconds,
		ExpiredAgoSeconds:    status.ExpiredAgoSeconds,
		Expired:              status.Expired,
		RefreshDue:           status.RefreshDue,
		RefreshPossible:      status.RefreshPossible,
		RefreshWindowSeconds: status.RefreshWindowSeconds,
	}
	switch {
	case !status.HasUserToken:
		token.State = "missing"
		token.NeedsReauth = true
	case auth.IsBrowserSession() && !status.HasSessionCookie:
		token.State = "incomplete"
		token.NeedsReauth = true
	case auth.Expired(now):
		token.State = "expired"
		token.NeedsReauth = !auth.RefreshPossible()
	case auth.RefreshDue(now, DefaultRefreshWindow):
		token.State = "refresh_due"
	case auth.ReadyForSlack():
		token.State = "ready"
	default:
		token.State = "incomplete"
		token.NeedsReauth = true
	}
	return token
}

func WriteAuth(path string, auth Auth) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'), 0o600)
}

func RemoveAuth(path string) (bool, error) {
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := file.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := file.Chmod(perm); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return syncDir(dir)
}

func syncDir(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()
	if err := file.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) {
		return err
	}
	return nil
}
