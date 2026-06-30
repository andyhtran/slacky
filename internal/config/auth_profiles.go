package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var authProfileNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type AuthProfileSummary struct {
	Name             string            `json:"name"`
	Path             string            `json:"path"`
	Active           bool              `json:"active"`
	Source           string            `json:"source"`
	CredentialSource string            `json:"credential_source"`
	Storage          AuthStorageStatus `json:"storage"`
	Token            AuthTokenStatus   `json:"token"`
	ReadyForSlack    bool              `json:"ready_for_slack"`
	AuthKind         string            `json:"auth_kind,omitempty"`
	TokenType        string            `json:"token_type,omitempty"`
	TeamID           string            `json:"team_id,omitempty"`
	TeamName         string            `json:"team_name,omitempty"`
	UserID           string            `json:"user_id,omitempty"`
	UserName         string            `json:"user_name,omitempty"`
	Scopes           []string          `json:"scopes,omitempty"`
	ExpiresAt        string            `json:"expires_at,omitempty"`
	Expired          bool              `json:"expired"`
	RefreshDue       bool              `json:"refresh_due"`
	RefreshPossible  bool              `json:"refresh_possible"`
	HasUserToken     bool              `json:"has_user_token"`
	HasSessionCookie bool              `json:"has_session_cookie"`
}

func ValidateAuthProfileName(name string) error {
	if !authProfileNamePattern.MatchString(name) {
		return fmt.Errorf("auth profile name must match %s", authProfileNamePattern.String())
	}
	return nil
}

func AuthProfilePath(profilesDir string, name string) string {
	return filepath.Join(profilesDir, name+".json")
}

func LoadAuthProfile(profilesDir string, name string) (Auth, error) {
	if err := ValidateAuthProfileName(name); err != nil {
		return Auth{}, err
	}
	return LoadAuth(AuthProfilePath(profilesDir, name))
}

func WriteAuthProfile(profilesDir string, name string, auth Auth) error {
	if err := ValidateAuthProfileName(name); err != nil {
		return err
	}
	auth.ProfileName = name
	return WriteAuth(AuthProfilePath(profilesDir, name), auth)
}

func RemoveAuthProfile(profilesDir string, name string) (bool, error) {
	if err := ValidateAuthProfileName(name); err != nil {
		return false, err
	}
	return RemoveAuth(AuthProfilePath(profilesDir, name))
}

func ListAuthProfiles(profilesDir string, activeName string) ([]AuthProfileSummary, error) {
	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	profiles := make([]AuthProfileSummary, 0, len(entries))
	dirMode := ""
	if info, err := os.Stat(profilesDir); err == nil {
		dirMode = info.Mode().Perm().String()
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		if err := ValidateAuthProfileName(name); err != nil {
			continue
		}
		path := AuthProfilePath(profilesDir, name)
		auth, err := LoadAuth(path)
		if err != nil {
			continue
		}
		now := time.Now()
		fileMode := ""
		if info, err := os.Stat(path); err == nil {
			fileMode = info.Mode().Perm().String()
		}
		status := AuthStatus{
			HasUserToken:         auth.UserToken != "",
			HasSessionCookie:     auth.SessionCookieD != "",
			ExpiresAt:            formatAuthProfileExpiry(auth),
			ExpiresInSeconds:     auth.ExpiresInSeconds(now),
			ExpiredAgoSeconds:    auth.ExpiredAgoSeconds(now),
			Expired:              auth.Expired(now),
			RefreshDue:           auth.RefreshDue(now, DefaultRefreshWindow),
			RefreshPossible:      auth.RefreshPossible(),
			RefreshWindowSeconds: int64(DefaultRefreshWindow / time.Second),
		}
		profiles = append(profiles, AuthProfileSummary{
			Name:             name,
			Path:             path,
			Active:           name == activeName,
			Source:           "profile",
			CredentialSource: authCredentialSource(auth),
			Storage:          authStorageStatus("local_file", path, fileMode, dirMode),
			Token:            authTokenStatus(auth, status, now),
			ReadyForSlack:    auth.ReadyForSlack(),
			AuthKind:         auth.EffectiveAuthKind(),
			TokenType:        auth.TokenType,
			TeamID:           auth.TeamID,
			TeamName:         auth.TeamName,
			UserID:           auth.UserID,
			UserName:         auth.UserName,
			Scopes:           append([]string(nil), auth.Scopes...),
			ExpiresAt:        status.ExpiresAt,
			Expired:          status.Expired,
			RefreshDue:       status.RefreshDue,
			RefreshPossible:  status.RefreshPossible,
			HasUserToken:     status.HasUserToken,
			HasSessionCookie: status.HasSessionCookie,
		})
	}
	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].Name < profiles[j].Name
	})
	return profiles, nil
}

func ReadActiveProfile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	name := strings.TrimSpace(string(data))
	if name == "" {
		return "", nil
	}
	if err := ValidateAuthProfileName(name); err != nil {
		return "", err
	}
	return name, nil
}

func WriteActiveProfile(path string, name string) error {
	if err := ValidateAuthProfileName(name); err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(name+"\n"), 0o600)
}

func ClearActiveProfile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func formatAuthProfileExpiry(auth Auth) string {
	if auth.ExpiresAt.IsZero() {
		return ""
	}
	return auth.ExpiresAt.Format(time.RFC3339)
}
