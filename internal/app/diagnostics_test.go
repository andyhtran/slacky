package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyhtran/slacky/internal/api"
	"github.com/andyhtran/slacky/internal/config"
	"github.com/andyhtran/slacky/internal/output"
	"github.com/andyhtran/slacky/internal/paths"
)

func TestAuthLogoutDryRunKeepsAuthFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SLACKY_HOME", home)
	authPath := filepath.Join(home, "slack.json")
	if err := config.WriteAuth(authPath, config.Auth{UserToken: "xoxp-test", UserID: "U123"}); err != nil {
		t.Fatalf("write auth: %v", err)
	}

	text := captureStdout(t, func() {
		cmd := AuthLogoutCmd{DryRun: true}
		if err := cmd.Run(&Globals{}); err != nil {
			t.Fatalf("auth logout dry run: %v", err)
		}
	})
	if _, err := os.Stat(authPath); err != nil {
		t.Fatalf("dry run should keep auth file: %v", err)
	}
	for _, want := range []string{
		"Auth logout dry run",
		"Would remove: " + authPath,
		"slacky auth logout",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("dry run output missing %q\n%s", want, text)
		}
	}
}

func TestAuthLogoutRemovesAuthFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SLACKY_HOME", home)
	authPath := filepath.Join(home, "slack.json")
	if err := config.WriteAuth(authPath, config.Auth{UserToken: "xoxp-test", UserID: "U123"}); err != nil {
		t.Fatalf("write auth: %v", err)
	}

	text := captureStdout(t, func() {
		cmd := AuthLogoutCmd{}
		if err := cmd.Run(&Globals{}); err != nil {
			t.Fatalf("auth logout: %v", err)
		}
	})
	if _, err := os.Stat(authPath); !os.IsNotExist(err) {
		t.Fatalf("auth file should be removed, stat err = %v", err)
	}
	for _, want := range []string{
		"Auth logout complete",
		"Removed local auth: true",
		"Slack token revoked: false",
		"slacky auth import",
		"slacky setup wizard",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("logout output missing %q\n%s", want, text)
		}
	}
}

func TestCacheStatusProfileShowsScopedDB(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SLACKY_HOME", home)
	profilesDir := filepath.Join(home, "auth")
	if err := config.WriteAuthProfile(profilesDir, "work", config.Auth{
		UserToken: "xoxp-test",
		TeamID:    "T123456",
		TeamName:  "Sample Workspace",
		UserID:    "U999999",
		UserName:  "sampleuser",
	}); err != nil {
		t.Fatalf("write auth profile: %v", err)
	}
	cachePath := filepath.Join(home, "cache", "profiles", "work--T123456--U999999", "index.db")

	text := captureStdout(t, func() {
		cmd := CacheStatusCmd{Profile: "work"}
		if err := cmd.Run(&Globals{}); err != nil {
			t.Fatalf("cache status: %v", err)
		}
	})
	for _, want := range []string{
		"Profile: work",
		"Team: Sample Workspace (T123456)",
		"User: @sampleuser (U999999)",
		"DB: " + cachePath,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("cache status output missing %q\n%s", want, text)
		}
	}
}

func TestAuthStatusShowsProfileCacheDB(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SLACKY_HOME", home)
	authPath := filepath.Join(home, "slack.json")
	if err := config.WriteAuth(authPath, config.Auth{
		ProfileName: "work-browser",
		UserToken:   "xoxp-test",
		TeamID:      "T123456",
		TeamName:    "Sample Workspace",
		UserID:      "U999999",
		UserName:    "sampleuser",
	}); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	cachePath := filepath.Join(home, "cache", "profiles", "work-browser--T123456--U999999", "index.db")

	text := captureStdout(t, func() {
		cmd := AuthStatusCmd{}
		if err := cmd.Run(&Globals{}); err != nil {
			t.Fatalf("auth status: %v", err)
		}
	})
	for _, want := range []string{
		"Profile: work-browser",
		"Team: Sample Workspace (T123456)",
		"User: @sampleuser (U999999)",
		"Cache DB: " + cachePath,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("auth status output missing %q\n%s", want, text)
		}
	}
}

func TestAuthStatusJSONIncludesActiveSourceAndStorage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SLACKY_HOME", home)
	authPath := filepath.Join(home, "slack.json")
	if err := config.WriteAuth(authPath, config.Auth{
		ProfileName: "work",
		AuthKind:    config.AuthKindImportedUserToken,
		UserToken:   "xoxp-test",
		TeamID:      "T123456",
		TeamName:    "Sample Workspace",
		UserID:      "U999999",
		UserName:    "sampleuser",
	}); err != nil {
		t.Fatalf("write auth: %v", err)
	}

	text := captureStdout(t, func() {
		cmd := AuthStatusCmd{Active: true}
		if err := cmd.Run(&Globals{JSON: true}); err != nil {
			t.Fatalf("auth status json: %v", err)
		}
	})
	if !strings.HasPrefix(text, "{") {
		t.Fatalf("auth status JSON should start at byte 1, got %q", text[:min(len(text), 20)])
	}
	var envelope struct {
		Auth struct {
			Active           bool   `json:"active"`
			ActiveOnly       bool   `json:"active_only"`
			Source           string `json:"source"`
			SelectedBy       string `json:"selected_by"`
			CredentialSource string `json:"credential_source"`
			Storage          struct {
				Kind                 string `json:"kind"`
				Path                 string `json:"path"`
				SecretValuesRedacted bool   `json:"secret_values_redacted"`
			} `json:"storage"`
			Token struct {
				Present bool   `json:"present"`
				State   string `json:"state"`
			} `json:"token"`
		} `json:"auth"`
	}
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		t.Fatalf("decode auth status JSON: %v\n%s", err, text)
	}
	if !envelope.Auth.Active || !envelope.Auth.ActiveOnly {
		t.Fatalf("auth active flags = active:%t active_only:%t", envelope.Auth.Active, envelope.Auth.ActiveOnly)
	}
	if envelope.Auth.Source != "profile" || envelope.Auth.SelectedBy != "active_profile" {
		t.Fatalf("auth source = %q selected_by = %q", envelope.Auth.Source, envelope.Auth.SelectedBy)
	}
	if envelope.Auth.CredentialSource != "local_file" || envelope.Auth.Storage.Kind != "local_file" || envelope.Auth.Storage.Path != authPath || !envelope.Auth.Storage.SecretValuesRedacted {
		t.Fatalf("auth storage/source = %#v", envelope.Auth)
	}
	if !envelope.Auth.Token.Present || envelope.Auth.Token.State != "ready" {
		t.Fatalf("auth token = %#v", envelope.Auth.Token)
	}
}

func TestAuthListJSONIncludesProfileSourceAndStorage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SLACKY_HOME", home)
	profilesDir := filepath.Join(home, "auth")
	if err := config.WriteAuthProfile(profilesDir, "work", config.Auth{
		AuthKind:       config.AuthKindBrowserSession,
		UserToken:      "xoxc-test",
		SessionCookieD: "xoxd-test",
		TeamID:         "T123456",
		TeamName:       "Sample Workspace",
		UserID:         "U999999",
		UserName:       "sampleuser",
	}); err != nil {
		t.Fatalf("write auth profile: %v", err)
	}
	if err := config.WriteActiveProfile(filepath.Join(home, "state", "active-auth-profile"), "work"); err != nil {
		t.Fatalf("write active profile: %v", err)
	}

	text := captureStdout(t, func() {
		cmd := AuthListCmd{}
		if err := cmd.Run(&Globals{JSON: true}); err != nil {
			t.Fatalf("auth list json: %v", err)
		}
	})
	var envelope struct {
		Auth struct {
			Profiles []struct {
				Name             string `json:"name"`
				Active           bool   `json:"active"`
				Source           string `json:"source"`
				CredentialSource string `json:"credential_source"`
				Storage          struct {
					Kind string `json:"kind"`
					Path string `json:"path"`
				} `json:"storage"`
				Token struct {
					Present bool   `json:"present"`
					State   string `json:"state"`
				} `json:"token"`
			} `json:"profiles"`
		} `json:"auth"`
	}
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		t.Fatalf("decode auth list JSON: %v\n%s", err, text)
	}
	if len(envelope.Auth.Profiles) != 1 {
		t.Fatalf("profiles = %#v", envelope.Auth.Profiles)
	}
	profile := envelope.Auth.Profiles[0]
	if profile.Name != "work" || !profile.Active || profile.Source != "profile" || profile.CredentialSource != "browser_session" {
		t.Fatalf("profile source = %#v", profile)
	}
	if profile.Storage.Kind != "local_file" || profile.Storage.Path == "" || !profile.Token.Present || profile.Token.State != "ready" {
		t.Fatalf("profile storage/token = %#v", profile)
	}
}

func TestAuthListTextUsesBoundedTable(t *testing.T) {
	profiles := []config.AuthProfileSummary{
		{
			Name:             "very-long-profile-name",
			Active:           true,
			ReadyForSlack:    true,
			CredentialSource: "browser_session",
			Storage:          config.AuthStorageStatus{Kind: "local_file"},
			Token:            config.AuthTokenStatus{State: "refresh_due"},
			TeamID:           "T123456789",
			TeamName:         "Very Long Sample Workspace Name",
			UserID:           "U999999999",
			UserName:         "sample.person.with.long.name",
		},
	}
	text := ""
	for _, width := range []int{80, 100} {
		text = authListText("very-long-profile-name", profiles, config.AuthStatus{}, false, width)
		for _, line := range strings.Split(text, "\n") {
			if got := output.VisibleWidth(line); got > width {
				t.Fatalf("auth list line width = %d, want <= %d\n%s", got, width, line)
			}
		}
	}
	for _, want := range []string{"ACTIVE", "NAME", "STATUS", "TEAM", "USER", "SOURCE", "STORAGE", "------", "refresh", "browser", "file", "Next:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("auth list table missing %q\n%s", want, text)
		}
	}
	if strings.Contains(text, "Very Long Sample Workspace Name") || strings.Contains(text, "sample.person.with.long.name") {
		t.Fatalf("auth list should compact long human cells:\n%s", text)
	}
}

func TestAuthListTextStylesTableAndNext(t *testing.T) {
	output.SetColor(true)
	defer output.SetColor(false)

	profiles := []config.AuthProfileSummary{{
		Name:             "work",
		Active:           true,
		ReadyForSlack:    true,
		CredentialSource: "local_file",
		Storage:          config.AuthStorageStatus{Kind: "local_file"},
		Token:            config.AuthTokenStatus{State: "ready"},
		TeamName:         "Sample Workspace",
		UserName:         "sampleuser",
	}}
	text := authListText("work", profiles, config.AuthStatus{}, true, 80)
	for _, want := range []string{
		"\x1b[2m  ACTIVE",
		"\x1b[2m  ------",
		"\x1b[32mready\x1b[0m",
		"\x1b[2mNext:\x1b[0m",
		"\x1b[36m  slacky auth switch <name>\x1b[0m",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("styled auth list missing %q\n%s", want, text)
		}
	}
}

func TestAuthListJSONStartsAtByteOneAndOmitsANSI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SLACKY_HOME", home)
	profilesDir := filepath.Join(home, "auth")
	if err := config.WriteAuthProfile(profilesDir, "work", config.Auth{
		AuthKind:  config.AuthKindImportedUserToken,
		UserToken: "xoxp-test",
		TeamID:    "T123456",
		TeamName:  "Sample Workspace",
		UserID:    "U999999",
		UserName:  "sampleuser",
	}); err != nil {
		t.Fatalf("write auth profile: %v", err)
	}
	output.SetColor(true)
	defer output.SetColor(false)

	text := captureStdout(t, func() {
		cmd := AuthListCmd{}
		if err := cmd.Run(&Globals{JSON: true}); err != nil {
			t.Fatalf("auth list json: %v", err)
		}
	})
	if !strings.HasPrefix(text, "{") {
		t.Fatalf("auth list JSON should start at byte 1, got %q", text[:min(len(text), 20)])
	}
	if strings.Contains(text, "\x1b[") {
		t.Fatalf("auth list JSON should not contain ANSI:\n%s", text)
	}
}

func TestSlackReachabilityCanUseNonActiveAuthPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SLACKY_HOME", home)

	activePath := filepath.Join(home, "slack.json")
	otherPath := filepath.Join(home, "auth", "other.json")
	if err := config.WriteAuth(activePath, config.Auth{UserToken: "xoxp-active"}); err != nil {
		t.Fatalf("write active auth: %v", err)
	}
	if err := config.WriteAuth(otherPath, config.Auth{UserToken: "xoxp-other"}); err != nil {
		t.Fatalf("write other auth: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer xoxp-other" {
			t.Fatalf("Authorization = %q, want non-active token", got)
		}
		_, _ = writer.Write([]byte(`{"ok":true,"team":"Sample Workspace","user":"sampleuser","team_id":"T123","user_id":"U123"}`))
	}))
	defer server.Close()

	oldNewAPIClient := newAPIClient
	newAPIClient = func(token string, version string, timeout time.Duration, maxRateLimitWait time.Duration) *api.Client {
		client := api.NewClient(token, version, timeout, maxRateLimitWait)
		client.BaseURL = server.URL + "/"
		client.HTTPClient = server.Client()
		return client
	}
	t.Cleanup(func() {
		newAPIClient = oldNewAPIClient
	})

	pathSet, err := paths.Resolve()
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	status := config.InspectAuth(otherPath)
	reachability := slackReachability(&Globals{Timeout: time.Second}, pathSet, otherPath, status)
	if !reachability.OK {
		t.Fatalf("reachability should use non-active auth path: %#v", reachability)
	}
}

func TestAuthExpiresInLabelShowsExpiredAgo(t *testing.T) {
	status := config.AuthStatus{
		ExpiresAtTime:     time.Now().Add(-2 * time.Minute),
		Expired:           true,
		ExpiredAgoSeconds: 120,
	}
	got := authExpiresInLabel(status)
	if got != "expired 2m0s ago" {
		t.Fatalf("auth expiry label = %q", got)
	}
}

func TestAuthExpiresInLabelClampsHugeDurations(t *testing.T) {
	status := config.AuthStatus{
		ExpiresAtTime:    time.Now().Add(time.Hour),
		ExpiresInSeconds: 10_000_000_000,
	}
	got := authExpiresInLabel(status)
	if !strings.HasPrefix(got, ">=") {
		t.Fatalf("huge expiry label = %q, want clamped label", got)
	}
}

func TestPathsShowsProfileCacheIdentity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SLACKY_HOME", home)
	authPath := filepath.Join(home, "slack.json")
	if err := config.WriteAuth(authPath, config.Auth{
		ProfileName: "work-browser",
		UserToken:   "xoxp-test",
		TeamID:      "T123456",
		TeamName:    "Sample Workspace",
		UserID:      "U999999",
		UserName:    "sampleuser",
	}); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	cachePath := filepath.Join(home, "cache", "profiles", "work-browser--T123456--U999999", "index.db")

	text := captureStdout(t, func() {
		cmd := PathsCmd{}
		if err := cmd.Run(&Globals{}); err != nil {
			t.Fatalf("paths: %v", err)
		}
	})
	for _, want := range []string{
		"Profile:   work-browser",
		"Team:      Sample Workspace (T123456)",
		"User:      @sampleuser (U999999)",
		"Cache DB:  " + cachePath,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("paths output missing %q\n%s", want, text)
		}
	}
}

func TestCacheClearProfileDryRunKeepsCacheFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SLACKY_HOME", home)
	profilesDir := filepath.Join(home, "auth")
	if err := config.WriteAuthProfile(profilesDir, "work", config.Auth{
		UserToken: "xoxp-test",
		TeamID:    "T123456",
		TeamName:  "Sample Workspace",
		UserID:    "U999999",
		UserName:  "sampleuser",
	}); err != nil {
		t.Fatalf("write auth profile: %v", err)
	}
	cachePath := filepath.Join(home, "cache", "profiles", "work--T123456--U999999", "index.db")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	for _, path := range []string{cachePath, cachePath + "-wal"} {
		if err := os.WriteFile(path, []byte("cache"), 0o600); err != nil {
			t.Fatalf("write cache file: %v", err)
		}
	}

	text := captureStdout(t, func() {
		cmd := CacheClearCmd{Profile: "work", DryRun: true}
		if err := cmd.Run(&Globals{}); err != nil {
			t.Fatalf("cache clear dry run: %v", err)
		}
	})
	for _, path := range []string{cachePath, cachePath + "-wal"} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("dry run should keep cache file %s: %v", path, err)
		}
	}
	for _, want := range []string{
		"Cache clear dry run",
		"Profile: work",
		"Would remove:",
		cachePath + " (exists)",
		"slacky cache clear --profile work --apply",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("cache clear output missing %q\n%s", want, text)
		}
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = original
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	var buffer bytes.Buffer
	if _, err := io.Copy(&buffer, reader); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	return buffer.String()
}
