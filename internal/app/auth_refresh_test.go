package app

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andyhtran/slacky/internal/api"
	"github.com/andyhtran/slacky/internal/config"
	"github.com/andyhtran/slacky/internal/paths"
)

func TestNamedProfileMissingDoesNotInheritActiveAuth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SLACKY_HOME", home)
	pathSet, err := paths.Resolve()
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	if err := config.WriteAuth(pathSet.AuthFile.Path, config.Auth{
		AuthKind:         config.AuthKindBrowserSession,
		UserToken:        "xoxc-active",
		SessionCookieD:   "xoxd-active",
		BrowserUserAgent: "browser",
		ClientID:         "stale-client",
		RefreshToken:     "stale-refresh",
		ExpiresAt:        time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("write active auth: %v", err)
	}

	auth := loadExistingAuthForProfile(pathSet, "new-oauth")
	if auth.UserToken != "" || auth.SessionCookieD != "" || auth.ClientID != "" || auth.RefreshToken != "" || !auth.ExpiresAt.IsZero() {
		t.Fatalf("missing named profile should start empty, got %#v", auth)
	}
}

func TestAuthConstructorsClearIncompatibleFields(t *testing.T) {
	oauth := oauthUserAuth("client-id", "", "http://127.0.0.1:8888/callback", api.OAuthToken{
		AccessToken:  "xoxp-oauth",
		TokenType:    "user",
		RefreshToken: "xoxr-oauth",
		ExpiresAt:    time.Now().Add(time.Hour),
	})
	if oauth.AuthKind != config.AuthKindOAuthUser || oauth.SessionCookieD != "" || oauth.BrowserUserAgent != "" {
		t.Fatalf("oauth auth has incompatible fields: %#v", oauth)
	}

	session := browserSessionAuth("xoxc-session", "xoxd-session", "browser")
	if session.AuthKind != config.AuthKindBrowserSession || session.ClientID != "" || session.RefreshToken != "" || !session.ExpiresAt.IsZero() {
		t.Fatalf("browser session auth has incompatible fields: %#v", session)
	}

	imported := importedUserTokenAuth("xoxp-imported")
	if imported.AuthKind != config.AuthKindImportedUserToken || imported.ClientID != "" || imported.SessionCookieD != "" || imported.RefreshToken != "" || !imported.ExpiresAt.IsZero() {
		t.Fatalf("imported token auth has incompatible fields: %#v", imported)
	}
}

func TestAuthRefreshCommandRefreshesActiveProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SLACKY_HOME", home)
	pathSet, err := paths.Resolve()
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	auth := rotatingOAuthAuth(time.Now().Add(time.Minute))
	if _, err := writeSelectedAuth(pathSet, "work", auth); err != nil {
		t.Fatalf("write selected auth: %v", err)
	}

	restore := stubRefreshOAuthToken(t, func(_ context.Context, _ *http.Client, request api.OAuthRefreshRequest, _ string) (api.OAuthToken, error) {
		if request.ClientID != "client-id" || request.RefreshToken != "xoxr-old" {
			t.Fatalf("unexpected refresh request: %#v", request)
		}
		if request.ClientSecret != "" {
			t.Fatalf("client_secret should be omitted when profile has none")
		}
		return refreshedOAuthToken(), nil
	})
	defer restore()

	text := captureStdout(t, func() {
		cmd := AuthRefreshCmd{Profile: "work"}
		if err := cmd.Run(&Globals{Timeout: time.Second}); err != nil {
			t.Fatalf("auth refresh: %v", err)
		}
	})
	for _, want := range []string{"Auth refresh", "Profile: work", "State: refreshed"} {
		if !strings.Contains(text, want) {
			t.Fatalf("refresh output missing %q\n%s", want, text)
		}
	}
	for _, forbidden := range []string{"xoxp-new", "xoxr-new", "xoxr-old"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("refresh output leaked secret %q\n%s", forbidden, text)
		}
	}
	assertStoredRefreshToken(t, pathSet.AuthFile.Path, "xoxp-new", "xoxr-new")
	assertStoredRefreshToken(t, config.AuthProfilePath(pathSet.AuthProfilesDir.Path, "work"), "xoxp-new", "xoxr-new")
}

func TestAuthRefreshInvalidRefreshTokenSuggestsLogin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SLACKY_HOME", home)
	pathSet, err := paths.Resolve()
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	if err := config.WriteAuthProfile(pathSet.AuthProfilesDir.Path, "work", rotatingOAuthAuth(time.Now().Add(time.Minute))); err != nil {
		t.Fatalf("write auth profile: %v", err)
	}

	restore := stubRefreshOAuthToken(t, func(context.Context, *http.Client, api.OAuthRefreshRequest, string) (api.OAuthToken, error) {
		return api.OAuthToken{}, api.OAuthError{SlackError: "invalid_refresh_token"}
	})
	defer restore()

	_, _, err = refreshAuth(&Globals{Timeout: time.Second}, pathSet, "work", true, true)
	var appErr *AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError, got %T %v", err, err)
	}
	if appErr.Kind != "invalid_refresh_token" {
		t.Fatalf("kind = %q", appErr.Kind)
	}
	want := "slacky auth login --name work --client-id client-id"
	if len(appErr.SuggestedCommands) != 1 || appErr.SuggestedCommands[0] != want {
		t.Fatalf("suggested commands = %#v, want %q", appErr.SuggestedCommands, want)
	}
}

func TestConcurrentRefreshUsesOneRefreshToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SLACKY_HOME", home)
	pathSet, err := paths.Resolve()
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	if _, err := writeSelectedAuth(pathSet, "work", rotatingOAuthAuth(time.Now().Add(time.Minute))); err != nil {
		t.Fatalf("write selected auth: %v", err)
	}

	var mu sync.Mutex
	calls := 0
	restore := stubRefreshOAuthToken(t, func(context.Context, *http.Client, api.OAuthRefreshRequest, string) (api.OAuthToken, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		return refreshedOAuthToken(), nil
	})
	defer restore()

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := refreshAuth(&Globals{Timeout: time.Second}, pathSet, "work", false, false)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent refresh: %v", err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", calls)
	}
	assertStoredRefreshToken(t, filepath.Join(home, "auth", "work.json"), "xoxp-new", "xoxr-new")
}

func TestAuthStatusDoesNotLeakSecrets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SLACKY_HOME", home)
	authPath := filepath.Join(home, "slack.json")
	if err := config.WriteAuth(authPath, config.Auth{
		AuthKind:     config.AuthKindOAuthUser,
		ClientID:     "client-id",
		ClientSecret: "client-secret-value",
		UserToken:    "xoxp-secret-value",
		RefreshToken: "xoxr-secret-value",
		ExpiresAt:    time.Now().Add(time.Hour),
		TeamID:       "T123456",
		UserID:       "U999999",
	}); err != nil {
		t.Fatalf("write auth: %v", err)
	}

	text := captureStdout(t, func() {
		cmd := AuthStatusCmd{}
		if err := cmd.Run(&Globals{JSON: true}); err != nil {
			t.Fatalf("auth status: %v", err)
		}
	})
	for _, forbidden := range []string{"client-secret-value", "xoxp-secret-value", "xoxr-secret-value"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("auth status leaked secret %q\n%s", forbidden, text)
		}
	}
	for _, want := range []string{"refresh_possible", "expires_at", "auth_kind"} {
		if !strings.Contains(text, want) {
			t.Fatalf("auth status JSON missing %q\n%s", want, text)
		}
	}
}

func rotatingOAuthAuth(expiresAt time.Time) config.Auth {
	return config.Auth{
		AuthKind:     config.AuthKindOAuthUser,
		ClientID:     "client-id",
		RedirectURI:  "http://127.0.0.1:8888/callback",
		UserToken:    "xoxp-old",
		TokenType:    "user",
		RefreshToken: "xoxr-old",
		ExpiresAt:    expiresAt,
		TeamID:       "T123456",
		TeamName:     "Sample Workspace",
		UserID:       "U999999",
		UserName:     "sampleuser",
	}
}

func refreshedOAuthToken() api.OAuthToken {
	return api.OAuthToken{
		AccessToken:  "xoxp-new",
		TokenType:    "user",
		RefreshToken: "xoxr-new",
		ExpiresAt:    time.Now().Add(12 * time.Hour),
		Scopes:       []string{"search:read"},
	}
}

func stubRefreshOAuthToken(t *testing.T, fn func(context.Context, *http.Client, api.OAuthRefreshRequest, string) (api.OAuthToken, error)) func() {
	t.Helper()
	old := refreshOAuthToken
	refreshOAuthToken = fn
	return func() {
		refreshOAuthToken = old
	}
}

func assertStoredRefreshToken(t *testing.T, path string, wantAccessToken string, wantRefreshToken string) {
	t.Helper()
	auth, err := config.LoadAuth(path)
	if err != nil {
		t.Fatalf("load auth %s: %v", path, err)
	}
	if auth.UserToken != wantAccessToken || auth.RefreshToken != wantRefreshToken {
		t.Fatalf("stored auth tokens = %#v, want access %q refresh %q", auth, wantAccessToken, wantRefreshToken)
	}
	if auth.SessionCookieD != "" || auth.BrowserUserAgent != "" {
		t.Fatalf("refreshed OAuth auth should not have browser session fields: %#v", auth)
	}
}
