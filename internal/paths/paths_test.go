package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveUsesLegacyCacheForUnnamedAuth(t *testing.T) {
	home := t.TempDir()
	t.Setenv(HomeEnv, home)

	pathSet, err := Resolve()
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	want := filepath.Join(home, "cache", "index.db")
	if pathSet.CacheDB.Path != want {
		t.Fatalf("cache DB = %q, want %q", pathSet.CacheDB.Path, want)
	}
	if pathSet.CacheDB.Source != "env:"+HomeEnv {
		t.Fatalf("cache DB source = %q, want env source", pathSet.CacheDB.Source)
	}
}

func TestResolveUsesProfileScopedCacheForNamedAuth(t *testing.T) {
	home := t.TempDir()
	t.Setenv(HomeEnv, home)
	authPath := filepath.Join(home, "slack.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	if err := os.WriteFile(authPath, []byte(`{
  "profile_name": "nyc-browser",
  "team_id": "T026Y41CU87",
  "user_id": "U07E7579DML"
}`), 0o600); err != nil {
		t.Fatalf("write auth: %v", err)
	}

	pathSet, err := Resolve()
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	want := filepath.Join(home, "cache", "profiles", "nyc-browser--T026Y41CU87--U07E7579DML", "index.db")
	if pathSet.CacheDB.Path != want {
		t.Fatalf("cache DB = %q, want %q", pathSet.CacheDB.Path, want)
	}
	if pathSet.CacheDB.Source != "profile:nyc-browser" {
		t.Fatalf("cache DB source = %q, want profile source", pathSet.CacheDB.Source)
	}
}

func TestProfileCacheDirNameSanitizesProfileAndPreservesSlackIDs(t *testing.T) {
	got := ProfileCacheDirName("NYC Browser", "T026Y41CU87", "U07E7579DML")
	want := "nyc-browser--T026Y41CU87--U07E7579DML"
	if got != want {
		t.Fatalf("profile cache dir = %q, want %q", got, want)
	}
}
