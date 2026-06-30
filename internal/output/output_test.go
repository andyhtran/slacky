package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestHighlightTermsStylesMatchesWhenColorEnabled(t *testing.T) {
	SetColor(true)
	defer SetColor(false)

	got := HighlightTerms("Release checklist ready", []string{"checklist"})
	want := ansiDim + "Release " + ansiReset + ansiBold + "checklist" + ansiReset + ansiDim + " ready" + ansiReset
	if got != want {
		t.Fatalf("HighlightTerms = %q, want %q", got, want)
	}
}

func TestHighlightTermsReturnsPlainTextWithoutColor(t *testing.T) {
	SetColor(false)

	got := HighlightTerms("Release checklist ready", []string{"checklist"})
	if got != "Release checklist ready" {
		t.Fatalf("HighlightTerms without color = %q", got)
	}
}

func TestHealthColorsRespectColorGate(t *testing.T) {
	SetColor(true)
	if got := Green("ready"); got != ansiGreen+"ready"+ansiReset {
		t.Fatalf("Green = %q", got)
	}
	SetColor(false)
	if got := Red("missing"); got != "missing" {
		t.Fatalf("Red without color = %q", got)
	}
}

func TestTruncateNormalizesWhitespaceAndCapsRunes(t *testing.T) {
	got := Truncate("release\nchecklist\tready", 16)
	if got != "release check..." {
		t.Fatalf("Truncate = %q", got)
	}

	got = Truncate("abcdef", 3)
	if got != "abc" {
		t.Fatalf("Truncate narrow = %q", got)
	}
}

func TestWrapSplitsLongTokensAndPreservesWords(t *testing.T) {
	got := Wrap("alpha betagamma delta", 5)
	want := []string{"alpha", "betag", "amma", "delta"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("Wrap = %#v, want %#v", got, want)
	}
}

func TestWriteJSONStartsWithJSONObject(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, map[string]any{"ok": true}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	text := buf.String()
	if !strings.HasPrefix(text, "{\n") {
		t.Fatalf("JSON should start at byte 1 with object, got %q", text)
	}
	if !strings.Contains(text, "  \"ok\": true") {
		t.Fatalf("JSON should be indented, got %q", text)
	}
}
