package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyhtran/slacky/internal/api"
	"github.com/andyhtran/slacky/internal/config"
	"github.com/andyhtran/slacky/internal/store"
)

func TestDefaultNextLinesWhenAuthMissing(t *testing.T) {
	text := strings.Join(defaultNextLines(false, ""), "\n")
	for _, want := range []string{
		"slacky setup wizard",
		"slacky auth import",
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
		"slacky search 'from:@sampleuser has:link'",
		"slacky channels",
		"slacky history --channel general --count 25",
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
	text := strings.Join(authStatusNextLines(false, ""), "\n")
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
	text := strings.Join(authStatusNextLines(true, "@sampleuser"), "\n")
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
