package paths

import (
	"os"
	"path/filepath"
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
	Home      Entry `json:"home"`
	AuthFile  Entry `json:"auth_file"`
	CacheDB   Entry `json:"cache_db"`
	StateDir  Entry `json:"state_dir"`
	LogsDir   Entry `json:"logs_dir"`
	SkillsDir Entry `json:"skills_dir"`
}

func Resolve() (Set, error) {
	home, source, err := resolveHome()
	if err != nil {
		return Set{}, err
	}
	return Set{
		Home:      Entry{Path: home, Source: source},
		AuthFile:  Entry{Path: filepath.Join(home, "slack.json"), Source: source},
		CacheDB:   Entry{Path: filepath.Join(home, "cache", "index.db"), Source: source},
		StateDir:  Entry{Path: filepath.Join(home, "state"), Source: source},
		LogsDir:   Entry{Path: filepath.Join(home, "logs"), Source: source},
		SkillsDir: Entry{Path: filepath.Join(home, "skills"), Source: source},
	}, nil
}

func (set Set) EnsureDirs() error {
	for _, path := range []string{
		set.Home.Path,
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
