package config

import (
	"path/filepath"
	"testing"
)

func TestAuthProfileStorageAndListing(t *testing.T) {
	home := t.TempDir()
	profilesDir := filepath.Join(home, "auth")
	activePath := filepath.Join(home, "state", "active-auth-profile")

	auth := Auth{
		UserToken:      "xoxc-test",
		SessionCookieD: "xoxd-test",
		TokenType:      TokenTypeBrowserSession,
		TeamID:         "T123",
		UserID:         "U123",
	}
	if err := WriteAuthProfile(profilesDir, "work-browser", auth); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	if err := WriteActiveProfile(activePath, "work-browser"); err != nil {
		t.Fatalf("write active profile: %v", err)
	}
	activeName, err := ReadActiveProfile(activePath)
	if err != nil {
		t.Fatalf("read active profile: %v", err)
	}
	if activeName != "work-browser" {
		t.Fatalf("active profile = %q", activeName)
	}

	profiles, err := ListAuthProfiles(profilesDir, activeName)
	if err != nil {
		t.Fatalf("list profiles: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("profile count = %d", len(profiles))
	}
	profile := profiles[0]
	if profile.Name != "work-browser" || !profile.Active || !profile.ReadyForSlack {
		t.Fatalf("unexpected profile summary: %#v", profile)
	}
	if !profile.HasUserToken || !profile.HasSessionCookie {
		t.Fatalf("profile should report stored browser-session credentials: %#v", profile)
	}
}

func TestBrowserSessionNeedsCookie(t *testing.T) {
	auth := Auth{
		UserToken: "xoxc-test",
		TokenType: TokenTypeBrowserSession,
	}
	if auth.ReadyForSlack() {
		t.Fatalf("browser session without d cookie should not be ready")
	}
	auth.SessionCookieD = "xoxd-test"
	if !auth.ReadyForSlack() {
		t.Fatalf("browser session with d cookie should be ready")
	}
}
