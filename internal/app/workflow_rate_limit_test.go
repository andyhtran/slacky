package app

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyhtran/slacky/internal/api"
	"github.com/andyhtran/slacky/internal/config"
	"github.com/andyhtran/slacky/internal/store"
)

const (
	workflowChannelID = "C123"
	workflowRootTS    = "1717440000.000000"
	workflowReplyTS   = "1717440000.000100"
)

type workflowTestEnvelope struct {
	OK          bool                `json:"ok"`
	Source      string              `json:"source"`
	CacheNotice string              `json:"cache_notice"`
	Results     []api.MessageResult `json:"results"`
	Message     api.MessageResult   `json:"message"`
	Thread      api.ThreadResult    `json:"thread"`
	Cache       store.Status        `json:"cache"`
}

type workflowTestFixture struct {
	CachePath string
}

func TestLiveCommandsReturnCachedDataDuringRateLimit(t *testing.T) {
	tests := []struct {
		name           string
		cooldownMethod string
		run            func(*Globals) error
		assert         func(*testing.T, workflowTestEnvelope)
	}{
		{
			name:           "message",
			cooldownMethod: "conversations.history",
			run: func(globals *Globals) error {
				return (&MessageCmd{Channel: workflowChannelID, TS: workflowReplyTS}).Run(globals)
			},
			assert: func(t *testing.T, envelope workflowTestEnvelope) {
				t.Helper()
				if envelope.Message.TS != workflowReplyTS || len(envelope.Results) != 1 {
					t.Fatalf("expected cached reply message, got %#v", envelope)
				}
			},
		},
		{
			name:           "thread",
			cooldownMethod: "conversations.replies",
			run: func(globals *Globals) error {
				return (&ThreadCmd{Channel: workflowChannelID, TS: workflowRootTS}).Run(globals)
			},
			assert: func(t *testing.T, envelope workflowTestEnvelope) {
				t.Helper()
				if envelope.Thread.RootTS != workflowRootTS || len(envelope.Results) != 2 {
					t.Fatalf("expected cached thread, got %#v", envelope)
				}
			},
		},
		{
			name:           "history",
			cooldownMethod: "conversations.history",
			run: func(globals *Globals) error {
				return (&HistoryCmd{Channel: workflowChannelID, Count: 2}).Run(globals)
			},
			assert: func(t *testing.T, envelope workflowTestEnvelope) {
				t.Helper()
				if len(envelope.Results) != 2 {
					t.Fatalf("expected two cached history messages, got %#v", envelope.Results)
				}
			},
		},
		{
			name:           "context",
			cooldownMethod: "conversations.history",
			run: func(globals *Globals) error {
				return (&ContextCmd{Channel: workflowChannelID, TS: workflowRootTS, Before: 1, After: 1}).Run(globals)
			},
			assert: func(t *testing.T, envelope workflowTestEnvelope) {
				t.Helper()
				if len(envelope.Results) != 3 || envelope.Results[1].TS != workflowRootTS {
					t.Fatalf("expected cached context around root, got %#v", envelope.Results)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := setupWorkflowCache(t)
			recordWorkflowCooldown(t, fixture.CachePath, test.cooldownMethod)

			envelope := runWorkflowJSON(t, test.run)
			assertCacheFallback(t, envelope, test.cooldownMethod)
			test.assert(t, envelope)
		})
	}
}

func TestThreadCommandResolvesCachedReplyTimestampToRoot(t *testing.T) {
	fixture := setupWorkflowCache(t)
	recordWorkflowCooldown(t, fixture.CachePath, "conversations.replies")

	envelope := runWorkflowJSON(t, func(globals *Globals) error {
		return (&ThreadCmd{Channel: workflowChannelID, TS: workflowReplyTS}).Run(globals)
	})

	assertCacheFallback(t, envelope, "conversations.replies")
	if envelope.Thread.RootTS != workflowRootTS {
		t.Fatalf("thread root = %q, want %q", envelope.Thread.RootTS, workflowRootTS)
	}
	if len(envelope.Results) != 2 || envelope.Results[1].TS != workflowReplyTS {
		t.Fatalf("expected reply timestamp to return containing thread, got %#v", envelope.Results)
	}
}

func TestOpenReplyPermalinkRoutesToMessageAndThreadModes(t *testing.T) {
	fixture := setupWorkflowCache(t)
	recordWorkflowCooldown(t, fixture.CachePath, "conversations.replies")
	replyURL := "https://example.slack.com/archives/C123/p1717440000000100?thread_ts=1717440000.000000&cid=C123"

	messageEnvelope := runWorkflowJSON(t, func(globals *Globals) error {
		return (&OpenCmd{URL: replyURL}).Run(globals)
	})
	assertCacheFallback(t, messageEnvelope, "conversations.replies")
	if messageEnvelope.Message.TS != workflowReplyTS {
		t.Fatalf("default open should return exact reply message, got %#v", messageEnvelope.Message)
	}
	if messageEnvelope.Thread.RootTS != workflowRootTS {
		t.Fatalf("default open should include containing thread, got %#v", messageEnvelope.Thread)
	}

	threadEnvelope := runWorkflowJSON(t, func(globals *Globals) error {
		return (&OpenCmd{URL: replyURL, Mode: "thread"}).Run(globals)
	})
	assertCacheFallback(t, threadEnvelope, "conversations.replies")
	if threadEnvelope.Thread.RootTS != workflowRootTS || len(threadEnvelope.Results) != 2 {
		t.Fatalf("thread mode should return root thread, got %#v", threadEnvelope)
	}
}

func setupWorkflowCache(t *testing.T) workflowTestFixture {
	t.Helper()

	home := t.TempDir()
	t.Setenv("SLACKY_HOME", home)
	authPath := filepath.Join(home, "slack.json")
	if err := config.WriteAuth(authPath, config.Auth{UserToken: "xoxp-test", UserID: "U123"}); err != nil {
		t.Fatalf("write auth: %v", err)
	}

	cachePath := filepath.Join(home, "cache", "index.db")
	db, err := store.Open(cachePath)
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	if err := db.UpsertChannels([]api.ChannelResult{{ID: workflowChannelID, Name: "general", IsChannel: true}}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}
	if err := db.UpsertUser(api.UserResult{ID: "U123", Name: "person", RealName: "Person Example"}); err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	if err := db.UpsertThread(api.ThreadResult{
		ChannelID: workflowChannelID,
		RootTS:    workflowRootTS,
		Permalink: "https://example.slack.com/archives/C123/p1717440000000000",
		Messages: []api.MessageResult{
			workflowMessage(workflowRootTS, workflowRootTS, "root cached message"),
			workflowMessage(workflowReplyTS, workflowRootTS, "reply cached message"),
		},
	}); err != nil {
		t.Fatalf("upsert thread: %v", err)
	}
	beforeTS := "1717439999.000000"
	afterTS := "1717440001.000000"
	if err := db.UpsertMessages([]api.MessageResult{
		workflowMessage(beforeTS, beforeTS, "before cached message"),
		workflowMessage(afterTS, afterTS, "after cached message"),
	}); err != nil {
		t.Fatalf("upsert context messages: %v", err)
	}
	return workflowTestFixture{CachePath: cachePath}
}

func workflowMessage(ts string, rootTS string, text string) api.MessageResult {
	return api.MessageResult{
		ChannelID:   workflowChannelID,
		ChannelName: "general",
		TS:          ts,
		ThreadTS:    rootTS,
		RootTS:      rootTS,
		Permalink:   "https://example.slack.com/archives/C123/p" + strings.ReplaceAll(ts, ".", ""),
		User:        "U123",
		Username:    "person",
		Excerpt:     text,
		TextFull:    text,
	}
}

func recordWorkflowCooldown(t *testing.T, cachePath string, method string) {
	t.Helper()

	db, err := store.Open(cachePath)
	if err != nil {
		t.Fatalf("open cache for cooldown: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	if err := db.RecordCooldown(method, time.Minute, time.Now().UTC().Add(time.Minute), "test"); err != nil {
		t.Fatalf("record cooldown: %v", err)
	}
}

func runWorkflowJSON(t *testing.T, run func(*Globals) error) workflowTestEnvelope {
	t.Helper()

	globals := &Globals{JSON: true, Timeout: time.Second}
	var runErr error
	text := captureStdout(t, func() {
		runErr = run(globals)
	})
	if runErr != nil {
		t.Fatalf("run command: %v", runErr)
	}
	var envelope workflowTestEnvelope
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, text)
	}
	if !envelope.OK {
		t.Fatalf("expected ok envelope, got %#v", envelope)
	}
	return envelope
}

func assertCacheFallback(t *testing.T, envelope workflowTestEnvelope, method string) {
	t.Helper()

	if envelope.Source != "cache" {
		t.Fatalf("source = %q, want cache; envelope=%#v", envelope.Source, envelope)
	}
	if !strings.Contains(envelope.CacheNotice, "rate limited") {
		t.Fatalf("cache notice should explain rate limit, got %q", envelope.CacheNotice)
	}
	for _, cooldown := range envelope.Cache.Cooldowns {
		if cooldown.Method == method {
			return
		}
	}
	t.Fatalf("expected cooldown for %s, got %#v", method, envelope.Cache.Cooldowns)
}
