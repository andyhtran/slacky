package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyhtran/slacky/internal/api"
	"github.com/andyhtran/slacky/internal/config"
	"github.com/andyhtran/slacky/internal/output"
	"github.com/andyhtran/slacky/internal/store"
)

func TestDefaultNextLinesWhenAuthMissing(t *testing.T) {
	text := strings.Join(defaultNextLines(false, ""), "\n")
	for _, want := range []string{
		"slacky auth status",
		"slacky setup wizard",
		"slacky auth import",
		"slacky skills list",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing auth next lines should contain %q\n%s", want, text)
		}
	}
	if strings.Contains(text, "slacky search \"from:@someone has:link\"") {
		t.Fatalf("missing auth next lines should not suggest live search:\n%s", text)
	}
}

func TestDefaultNextLinesWhenAuthReady(t *testing.T) {
	text := strings.Join(defaultNextLines(true, "@sampleuser"), "\n")
	for _, want := range []string{
		"slacky auth status",
		"slacky search 'from:@sampleuser has:link'",
		"slacky channels",
		"slacky search --local 'topic words'",
		"slacky skills list",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("ready auth next lines should contain %q\n%s", want, text)
		}
	}
	if strings.Contains(text, "slacky setup wizard") || strings.Contains(text, "auth import") {
		t.Fatalf("ready auth next lines should not suggest auth setup:\n%s", text)
	}
}

func TestDefaultNextLinesFallsBackToPlaceholder(t *testing.T) {
	text := strings.Join(defaultNextLines(true, ""), "\n")
	if !strings.Contains(text, "slacky search 'from:@someone has:link'") {
		t.Fatalf("ready auth without user should contain placeholder search:\n%s", text)
	}
}

func TestDefaultDashboardTextContract(t *testing.T) {
	output.SetColor(false)
	defer output.SetColor(false)

	text := defaultDashboardText(config.AuthStatus{ReadyForSlack: true}, defaultDashboardCacheStatus(), "@sampleuser", false)
	for _, want := range []string{
		"slacky\n",
		appDescription,
		"Auth: ready as @sampleuser",
		"Cache: 2 messages, 1 channel, 1 user (240 KB)",
		"Usage:\n  slacky <command> [options]\nStart here (for AI agents):\n  slacky skills get core",
		"Next:",
		"slacky search 'from:@sampleuser has:link'",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("default dashboard missing %q\n%s", want, text)
		}
	}
	for _, blocked := range []string{"Home:", "More:", "Index:", "/tmp/slacky/cache/index.db", "version"} {
		if strings.Contains(text, blocked) {
			t.Fatalf("default dashboard should not contain %q\n%s", blocked, text)
		}
	}
	if count := strings.Count(text, "Next:"); count != 1 {
		t.Fatalf("default dashboard has %d Next blocks\n%s", count, text)
	}
	if lines := strings.Split(text, "\n"); len(lines) > 18 {
		t.Fatalf("default dashboard has %d lines, want <= 18\n%s", len(lines), text)
	}
}

func TestDefaultDashboardTextStylesTTYOutput(t *testing.T) {
	output.SetColor(true)
	defer output.SetColor(false)

	text := defaultDashboardText(config.AuthStatus{ReadyForSlack: true}, defaultDashboardCacheStatus(), "@sampleuser", true)
	for _, want := range []string{
		"\x1b[1mslacky\x1b[0m",
		"\x1b[2mUsage:\x1b[0m",
		"\x1b[2mStart here (for AI agents):\x1b[0m",
		"  \x1b[36mslacky skills get core\x1b[0m",
		"\x1b[2mNext:\x1b[0m",
		"  \x1b[36mslacky search 'from:@sampleuser has:link'\x1b[0m",
		"  \x1b[36mslacky channels\x1b[0m",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("styled default dashboard missing %q\n%s", want, text)
		}
	}
}

func TestDefaultDashboardTextNoANSIWhenStylingDisabled(t *testing.T) {
	output.SetColor(true)
	defer output.SetColor(false)

	text := defaultDashboardText(config.AuthStatus{ReadyForSlack: true}, defaultDashboardCacheStatus(), "@sampleuser", false)
	if strings.Contains(text, "\x1b[") {
		t.Fatalf("unstyled default dashboard contains ANSI escapes:\n%q", text)
	}
}

func TestDefaultCacheLabelOmitsPath(t *testing.T) {
	label := defaultCacheLabel(defaultDashboardCacheStatus())
	if strings.Contains(label, "/tmp/slacky/cache/index.db") {
		t.Fatalf("default cache label should omit paths, got %q", label)
	}
	if label != "2 messages, 1 channel, 1 user (240 KB)" {
		t.Fatalf("default cache label = %q", label)
	}
}

func defaultDashboardCacheStatus() store.Status {
	return store.Status{
		CachePath: "/tmp/slacky/cache/index.db",
		Exists:    true,
		Openable:  true,
		SizeBytes: 245760,
		Counts: map[string]int{
			"messages": 2,
			"channels": 1,
			"users":    1,
		},
	}
}

func TestHumanByteSize(t *testing.T) {
	tests := map[int64]string{
		512:     "512 B",
		1024:    "1 KB",
		1536:    "1.5 KB",
		245760:  "240 KB",
		1048576: "1 MB",
	}
	for size, want := range tests {
		if got := humanByteSize(size); got != want {
			t.Fatalf("humanByteSize(%d) = %q, want %q", size, got, want)
		}
	}
}

func TestHumanByteSizeWithExact(t *testing.T) {
	got := humanByteSizeWithExact(245760)
	want := "240 KB (245,760 bytes)"
	if got != want {
		t.Fatalf("humanByteSizeWithExact = %q, want %q", got, want)
	}
}

func TestAuthenticatedUserLabelUsesCacheUserName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "index.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := db.UpsertUser(api.UserResult{ID: "U123", Name: "sampleuser", RealName: "Sample User"}); err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	label := authenticatedUserLabel(config.AuthStatus{ReadyForSlack: true, UserID: "U123"}, path)
	if label != "@sampleuser" {
		t.Fatalf("auth user label = %q", label)
	}
}

func TestAuthStatusNextLinesWhenAuthMissing(t *testing.T) {
	text := strings.Join(authStatusNextLines(config.AuthStatus{}, ""), "\n")
	for _, want := range []string{
		"slacky setup wizard",
		"slacky auth import",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing auth status next lines should contain %q\n%s", want, text)
		}
	}
}

func TestAuthStatusNextLinesWhenAuthReady(t *testing.T) {
	text := strings.Join(authStatusNextLines(config.AuthStatus{ReadyForSlack: true}, "@sampleuser"), "\n")
	for _, want := range []string{
		"slacky doctor",
		"slacky search 'from:@sampleuser has:link'",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("ready auth status next lines should contain %q\n%s", want, text)
		}
	}
	if strings.Contains(text, "slacky setup wizard") || strings.Contains(text, "auth import") {
		t.Fatalf("ready auth status next lines should not suggest auth setup:\n%s", text)
	}
}
