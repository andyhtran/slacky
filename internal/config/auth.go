package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type Auth struct {
	ClientID     string    `json:"client_id,omitempty"`
	ClientSecret string    `json:"client_secret,omitempty"`
	RedirectURI  string    `json:"redirect_uri,omitempty"`
	UserToken    string    `json:"user_token,omitempty"`
	UserID       string    `json:"user_id,omitempty"`
	UserName     string    `json:"user_name,omitempty"`
	TeamID       string    `json:"team_id,omitempty"`
	TeamName     string    `json:"team_name,omitempty"`
	Scopes       []string  `json:"scopes,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
}

type AuthStatus struct {
	Path             string   `json:"path"`
	Exists           bool     `json:"exists"`
	Readable         bool     `json:"readable"`
	ReadyForSlack    bool     `json:"ready_for_slack"`
	FileMode         string   `json:"file_mode,omitempty"`
	DirMode          string   `json:"dir_mode,omitempty"`
	PresentFields    []string `json:"present_fields,omitempty"`
	MissingFields    []string `json:"missing_fields,omitempty"`
	HasClientID      bool     `json:"has_client_id"`
	HasClientSecret  bool     `json:"has_client_secret"`
	HasRedirectURI   bool     `json:"has_redirect_uri"`
	HasUserToken     bool     `json:"has_user_token"`
	HasRefreshToken  bool     `json:"has_refresh_token"`
	TokenType        string   `json:"token_type,omitempty"`
	TeamID           string   `json:"team_id,omitempty"`
	TeamName         string   `json:"team_name,omitempty"`
	UserID           string   `json:"user_id,omitempty"`
	UserName         string   `json:"user_name,omitempty"`
	Scopes           []string `json:"scopes,omitempty"`
	RecoveryCommands []string `json:"recovery_commands,omitempty"`
	Error            string   `json:"error,omitempty"`
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

func InspectAuth(path string) AuthStatus {
	status := AuthStatus{
		Path: path,
		RecoveryCommands: []string{
			"slacky setup wizard",
			"slacky setup steps",
			"slacky setup manifest",
			"slacky auth login",
			"slacky auth import",
		},
	}

	if dirInfo, err := os.Stat(filepath.Dir(path)); err == nil {
		status.DirMode = dirInfo.Mode().Perm().String()
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
	status.HasRefreshToken = auth.RefreshToken != ""
	status.TokenType = auth.TokenType
	status.TeamID = auth.TeamID
	status.TeamName = auth.TeamName
	status.UserID = auth.UserID
	status.UserName = auth.UserName
	status.Scopes = append([]string(nil), auth.Scopes...)
	status.ReadyForSlack = status.HasUserToken

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

	return status
}

func WriteAuth(path string, auth Auth) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
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
