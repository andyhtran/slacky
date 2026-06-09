package paths

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	AppName = "slacky"
	HomeEnv = "SLACKY_HOME"
)

type Entry struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

type Set struct {
	Home              Entry `json:"home"`
	AuthFile          Entry `json:"auth_file"`
	AuthProfilesDir   Entry `json:"auth_profiles_dir"`
	ActiveProfileFile Entry `json:"active_profile_file"`
	CacheDB           Entry `json:"cache_db"`
	StateDir          Entry `json:"state_dir"`
	LogsDir           Entry `json:"logs_dir"`
	SkillsDir         Entry `json:"skills_dir"`
}

func Resolve() (Set, error) {
	home, source, err := resolveHome()
	if err != nil {
		return Set{}, err
	}
	authPath := filepath.Join(home, "slack.json")
	cachePath, cacheSource := activeCacheDBPath(home, authPath, source)
	return Set{
		Home:              Entry{Path: home, Source: source},
		AuthFile:          Entry{Path: authPath, Source: source},
		AuthProfilesDir:   Entry{Path: filepath.Join(home, "auth"), Source: source},
		ActiveProfileFile: Entry{Path: filepath.Join(home, "state", "active-auth-profile"), Source: source},
		CacheDB:           Entry{Path: cachePath, Source: cacheSource},
		StateDir:          Entry{Path: filepath.Join(home, "state"), Source: source},
		LogsDir:           Entry{Path: filepath.Join(home, "logs"), Source: source},
		SkillsDir:         Entry{Path: filepath.Join(home, "skills"), Source: source},
	}, nil
}

func CacheDBPath(home string, profileName string, teamID string, userID string) string {
	profileName = strings.TrimSpace(profileName)
	if profileName == "" {
		return filepath.Join(home, "cache", "index.db")
	}
	dir := ProfileCacheDirName(profileName, teamID, userID)
	return filepath.Join(home, "cache", "profiles", dir, "index.db")
}

func ProfileCacheDirName(profileName string, teamID string, userID string) string {
	return strings.Join([]string{
		cacheProfileSlug(profileName, "profile"),
		cacheIdentitySlug(teamID, "unknown-team"),
		cacheIdentitySlug(userID, "unknown-user"),
	}, "--")
}

func activeCacheDBPath(home string, authPath string, defaultSource string) (string, string) {
	auth := readCacheAuth(authPath)
	if strings.TrimSpace(auth.ProfileName) == "" {
		return CacheDBPath(home, "", "", ""), defaultSource
	}
	return CacheDBPath(home, auth.ProfileName, auth.TeamID, auth.UserID), "profile:" + auth.ProfileName
}

type cacheAuth struct {
	ProfileName string `json:"profile_name"`
	TeamID      string `json:"team_id"`
	UserID      string `json:"user_id"`
}

func readCacheAuth(path string) cacheAuth {
	data, err := os.ReadFile(path)
	if err != nil {
		return cacheAuth{}
	}
	var auth cacheAuth
	if err := json.Unmarshal(data, &auth); err != nil {
		return cacheAuth{}
	}
	return auth
}

var (
	cacheProfileSlugPattern  = regexp.MustCompile(`[^a-z0-9._-]+`)
	cacheIdentitySlugPattern = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
)

func cacheProfileSlug(value string, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = cacheProfileSlugPattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, ".-_")
	if value == "" {
		return fallback
	}
	return value
}

func cacheIdentitySlug(value string, fallback string) string {
	value = strings.TrimSpace(value)
	value = cacheIdentitySlugPattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, ".-_")
	if value == "" {
		return fallback
	}
	return value
}

func (set Set) EnsureDirs() error {
	for _, path := range []string{
		set.Home.Path,
		set.AuthProfilesDir.Path,
		filepath.Dir(set.CacheDB.Path),
		set.StateDir.Path,
		set.LogsDir.Path,
		set.SkillsDir.Path,
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func resolveHome() (string, string, error) {
	if value := os.Getenv(HomeEnv); value != "" {
		path, err := filepath.Abs(expandHome(value))
		if err != nil {
			return "", "", err
		}
		return path, "env:" + HomeEnv, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(home, ".slacky"), "default", nil
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if len(path) > 2 && path[:2] == "~/" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
