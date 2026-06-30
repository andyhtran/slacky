package app

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestEnvelopeContract(t *testing.T) {
	tests := []struct {
		name              string
		setup             func(t *testing.T)
		run               func(*Globals) error
		wantKeys          []string
		wantCacheFallback bool
		wantText          bool
		assert            func(t *testing.T, envelope map[string]any)
	}{
		{
			name: "message cache fallback",
			setup: func(t *testing.T) {
				fixture := setupWorkflowCache(t)
				recordWorkflowCooldown(t, fixture.CachePath, "conversations.history")
			},
			run: func(globals *Globals) error {
				return (&MessageCmd{Channel: workflowChannelID, TS: workflowReplyTS}).Run(globals)
			},
			wantKeys:          []string{"cache", "cache_notice", "message", "ok", "results", "source", "text"},
			wantCacheFallback: true,
			wantText:          true,
		},
		{
			name: "thread cache fallback",
			setup: func(t *testing.T) {
				fixture := setupWorkflowCache(t)
				recordWorkflowCooldown(t, fixture.CachePath, "conversations.replies")
			},
			run: func(globals *Globals) error {
				return (&ThreadCmd{Channel: workflowChannelID, TS: workflowRootTS}).Run(globals)
			},
			wantKeys:          []string{"cache", "cache_notice", "ok", "results", "source", "text", "thread"},
			wantCacheFallback: true,
			wantText:          true,
		},
		{
			name: "context cache fallback",
			setup: func(t *testing.T) {
				fixture := setupWorkflowCache(t)
				recordWorkflowCooldown(t, fixture.CachePath, "conversations.history")
			},
			run: func(globals *Globals) error {
				return (&ContextCmd{Channel: workflowChannelID, TS: workflowRootTS, Before: 1, After: 1}).Run(globals)
			},
			wantKeys:          []string{"cache", "cache_notice", "ok", "results", "source", "text"},
			wantCacheFallback: true,
			wantText:          true,
		},
		{
			name: "history cache fallback",
			setup: func(t *testing.T) {
				fixture := setupWorkflowCache(t)
				recordWorkflowCooldown(t, fixture.CachePath, "conversations.history")
			},
			run: func(globals *Globals) error {
				return (&HistoryCmd{Channel: workflowChannelID, Count: 2}).Run(globals)
			},
			wantKeys:          []string{"cache", "cache_notice", "ok", "results", "source", "text"},
			wantCacheFallback: true,
			wantText:          true,
		},
		{
			name: "search local",
			setup: func(t *testing.T) {
				setupWorkflowCache(t)
			},
			run: func(globals *Globals) error {
				return (&SearchCmd{Query: []string{"cached"}, Local: true}).Run(globals)
			},
			wantKeys: []string{"cache", "ok", "results", "search", "source", "text"},
			wantText: true,
		},
		{
			name: "thread compact cache fallback",
			setup: func(t *testing.T) {
				fixture := setupWorkflowCache(t)
				recordWorkflowCooldown(t, fixture.CachePath, "conversations.replies")
			},
			run: func(globals *Globals) error {
				return (&ThreadCmd{Channel: workflowChannelID, TS: workflowRootTS, Compact: true}).Run(globals)
			},
			wantKeys:          []string{"cache_notice", "ok", "source", "thread"},
			wantCacheFallback: true,
			assert: func(t *testing.T, envelope map[string]any) {
				t.Helper()
				if _, exists := envelope["text"]; exists {
					t.Fatalf("compact envelope should omit text: %#v", envelope)
				}
				if _, exists := envelope["cache"]; exists {
					t.Fatalf("compact envelope should omit cache: %#v", envelope)
				}
				thread, ok := envelope["thread"].(map[string]any)
				if !ok {
					t.Fatalf("compact thread should be an object: %#v", envelope["thread"])
				}
				if _, exists := thread["commands"]; !exists {
					t.Fatalf("compact thread should include commands: %#v", thread)
				}
			},
		},
		{
			name: "version",
			setup: func(t *testing.T) {
				t.Setenv("SLACKY_HOME", t.TempDir())
			},
			run: func(globals *Globals) error {
				return (&VersionCmd{}).Run(globals)
			},
			wantKeys: []string{"ok", "text", "version"},
			wantText: true,
		},
		{
			name: "paths no auth",
			setup: func(t *testing.T) {
				t.Setenv("SLACKY_HOME", t.TempDir())
			},
			run: func(globals *Globals) error {
				return (&PathsCmd{}).Run(globals)
			},
			wantKeys: []string{"auth", "ok", "paths", "text"},
			wantText: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.setup != nil {
				test.setup(t)
			}

			envelope := runEnvelopeContractJSON(t, test.run)
			assertEnvelopeKeys(t, envelope, test.wantKeys)
			if test.wantCacheFallback {
				assertEnvelopeCacheFallback(t, envelope)
			}
			if test.wantText {
				assertEnvelopeText(t, envelope)
			}
			if test.assert != nil {
				test.assert(t, envelope)
			}
		})
	}
}

func runEnvelopeContractJSON(t *testing.T, run func(*Globals) error) map[string]any {
	t.Helper()

	globals := &Globals{JSON: true, Timeout: time.Second}
	var runErr error
	text := captureStdout(t, func() {
		runErr = run(globals)
	})
	if runErr != nil {
		t.Fatalf("run command: %v", runErr)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, text)
	}
	if ok, _ := envelope["ok"].(bool); !ok {
		t.Fatalf("expected ok envelope, got %#v", envelope)
	}
	return envelope
}

func assertEnvelopeKeys(t *testing.T, envelope map[string]any, want []string) {
	t.Helper()

	got := make([]string, 0, len(envelope))
	for key := range envelope {
		got = append(got, key)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("envelope keys = %#v, want %#v\nenvelope=%#v", got, want, envelope)
	}
}

func assertEnvelopeCacheFallback(t *testing.T, envelope map[string]any) {
	t.Helper()

	if source, _ := envelope["source"].(string); source != "cache" {
		t.Fatalf("source = %q, want cache", source)
	}
	notice, _ := envelope["cache_notice"].(string)
	if !strings.Contains(notice, "rate limited") {
		t.Fatalf("cache notice should explain rate limit, got %q", notice)
	}
}

func assertEnvelopeText(t *testing.T, envelope map[string]any) {
	t.Helper()

	text, _ := envelope["text"].(string)
	if strings.TrimSpace(text) == "" {
		t.Fatalf("expected non-empty text in envelope: %#v", envelope)
	}
}
