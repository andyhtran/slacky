package app

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyhtran/slacky/internal/config"
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
