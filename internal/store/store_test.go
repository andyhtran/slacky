package store

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyhtran/slacky/internal/api"
)

func TestStoreSearchAndStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "index.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	messages := []api.MessageResult{
		{
			ChannelID:   "C123",
			ChannelName: "general",
			TS:          "1717440000.000000",
			RootTS:      "1717440000.000000",
			Permalink:   "https://example.slack.com/archives/C123/p1717440000000000",
			User:        "U123",
			Username:    "person",
			Excerpt:     "Release notes mention the \uE000deploy\uE001 checklist",
		},
	}
	if err := db.UpsertMessages(messages); err != nil {
		t.Fatalf("upsert messages: %v", err)
	}
	if err := db.UpsertChannels([]api.ChannelResult{{ID: "C123", Name: "general", IsChannel: true}}); err != nil {
		t.Fatalf("upsert channels: %v", err)
	}
	if err := db.UpsertUser(api.UserResult{ID: "U123", TeamID: "T123", Name: "person", RealName: "Person Example", Email: "person@example.com"}); err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	if err := db.UpsertUser(api.UserResult{ID: "U456", TeamID: "T123", Name: "samplealias", RealName: "sample"}); err != nil {
		t.Fatalf("upsert visible-name user: %v", err)
	}

	result, err := db.SearchMessages("release checklist", 10)
	if err != nil {
		t.Fatalf("search messages: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected one message, got %d", len(result.Messages))
	}
	if result.Messages[0].ChannelID != "C123" {
		t.Fatalf("unexpected channel id: %s", result.Messages[0].ChannelID)
	}
	if strings.ContainsAny(result.Messages[0].Excerpt, "\uE000\uE001") {
		t.Fatalf("cached excerpt should not contain Slack highlight markers: %q", result.Messages[0].Excerpt)
	}
	message, found, err := db.Message("C123", "1717440000.000000")
	if err != nil {
		t.Fatalf("message: %v", err)
	}
	if !found || message.Excerpt != "Release notes mention the deploy checklist" {
		t.Fatalf("expected cached clean message, got found=%t message=%#v", found, message)
	}

	channels, err := db.Channels("#general", 10)
	if err != nil {
		t.Fatalf("channels: %v", err)
	}
	if len(channels) != 1 || channels[0].ID != "C123" {
		t.Fatalf("unexpected channels: %#v", channels)
	}

	user, found, err := db.UserByEmail("PERSON@example.com")
	if err != nil {
		t.Fatalf("user by email: %v", err)
	}
	if !found || user.ID != "U123" {
		t.Fatalf("expected cached user, got found=%t user=%#v", found, user)
	}
	user, found, err = db.UserByName("@person")
	if err != nil {
		t.Fatalf("user by name: %v", err)
	}
	if !found || user.ID != "U123" {
		t.Fatalf("expected cached user by name, got found=%t user=%#v", found, user)
	}
	user, found, err = db.UserByName("@sample")
	if err != nil {
		t.Fatalf("user by visible name: %v", err)
	}
	if !found || user.ID != "U456" {
		t.Fatalf("expected cached user by visible name, got found=%t user=%#v", found, user)
	}

	status := Inspect(path)
	if status.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %d, want %d", status.SchemaVersion, SchemaVersion)
	}
	if status.Counts["messages"] != 1 || status.Counts["channels"] != 1 || status.Counts["users"] != 2 {
		t.Fatalf("unexpected counts: %#v", status.Counts)
	}
	if !status.FTSHealthy {
		t.Fatalf("expected healthy FTS")
	}
}

func TestStoreUserByID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "index.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	if err := db.UpsertUser(api.UserResult{ID: "U456", TeamID: "T123", Name: "samplealias", RealName: "sample"}); err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	user, found, err := db.UserByID("U456")
	if err != nil {
		t.Fatalf("user by id: %v", err)
	}
	if !found || user.Name != "samplealias" {
		t.Fatalf("expected cached user by id, got found=%t user=%#v", found, user)
	}
}

func TestStoreThreadHistoryAndContextQueries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "index.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	thread := api.ThreadResult{
		ChannelID: "C123",
		RootTS:    "2.000000",
		Permalink: "https://example.slack.com/archives/C123/p2000000",
		Messages: []api.MessageResult{
			{ChannelID: "C123", TS: "2.000000", RootTS: "2.000000", Excerpt: "root"},
			{ChannelID: "C123", TS: "2.000100", RootTS: "2.000000", Excerpt: "reply"},
		},
	}
	if err := db.UpsertThread(thread); err != nil {
		t.Fatalf("upsert thread: %v", err)
	}
	if err := db.UpsertMessages([]api.MessageResult{
		{ChannelID: "C123", TS: "1.000000", RootTS: "1.000000", Excerpt: "before"},
		{ChannelID: "C123", TS: "3.000000", RootTS: "3.000000", Excerpt: "after"},
	}); err != nil {
		t.Fatalf("upsert messages: %v", err)
	}

	cachedThread, found, err := db.Thread("C123", "2.000000")
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	if !found || len(cachedThread.Messages) != 2 || cachedThread.Messages[1].TS != "2.000100" {
		t.Fatalf("unexpected cached thread found=%t thread=%#v", found, cachedThread)
	}
	history, err := db.ChannelMessages("C123", 2)
	if err != nil {
		t.Fatalf("channel messages: %v", err)
	}
	if len(history) != 2 || history[0].TS != "3.000000" {
		t.Fatalf("expected newest cached history first, got %#v", history)
	}
	contextMessages, err := db.ContextMessages("C123", "2.000000", 1, 1)
	if err != nil {
		t.Fatalf("context messages: %v", err)
	}
	if len(contextMessages) != 3 || contextMessages[0].TS != "1.000000" || contextMessages[2].TS != "2.000100" {
		t.Fatalf("unexpected cached context: %#v", contextMessages)
	}
}

func TestInspectDoesNotCreateDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "index.db")
	status := Inspect(path)
	if status.Exists {
		t.Fatalf("expected missing cache, got %#v", status)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("inspect created cache database or returned unexpected stat error: %v", err)
	}
}

func TestStoreIndexesNormalizedTextAndMentions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "index.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	message := api.MessageResult{
		ChannelID:   "C123",
		ChannelName: "general",
		TS:          "2.000000",
		RootTS:      "2.000000",
		User:        "U111",
		Username:    "person",
		TextFull:    "Release plan mentions <@U999> and <#C999|team-ops>",
		Blocks: json.RawMessage(`[
			{"type":"section","text":{"type":"mrkdwn","text":"Use the rollout checklist"}},
			{"type":"actions","elements":[{"type":"button","text":{"type":"plain_text","text":"Approve launch"}}]},
			{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"user","user_id":"U777"},{"type":"text","text":" owns the next step"}]}]},
			{"type":"table","rows":[{"cells":[{"text":"Service"},{"text":"Status"}]},{"cells":[{"text":"API"},{"text":"Ready"}]}]}
		]`),
		Attachments: json.RawMessage(`[{"fallback":"Customer escalation attachment","fields":[{"title":"Risk","value":"database migration"}]}]`),
		Files:       []api.FileResult{{ID: "F123", Title: "Launch spec", Filetype: "pdf", Permalink: "https://example.slack.com/files/F123"}},
	}
	if err := db.UpsertMessages([]api.MessageResult{message}); err != nil {
		t.Fatalf("upsert messages: %v", err)
	}
	result, err := db.SearchMessages("approve launch", 10)
	if err != nil {
		t.Fatalf("search messages: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected normalized block text match, got %#v", result.Messages)
	}
	if !strings.Contains(result.Messages[0].Excerpt, "Release plan") {
		t.Fatalf("expected raw message excerpt, got %q", result.Messages[0].Excerpt)
	}
	result, err = db.SearchMessages("api ready", 10)
	if err != nil {
		t.Fatalf("search rich table text: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected normalized table text match, got %#v", result.Messages)
	}
	result, err = db.SearchMessages("launch spec", 10)
	if err != nil {
		t.Fatalf("search file text: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected normalized file text match, got %#v", result.Messages)
	}
	cached, found, err := db.Message("C123", "2.000000")
	if err != nil {
		t.Fatalf("cached message: %v", err)
	}
	if !found || len(cached.Files) != 1 || cached.Files[0].Title != "Launch spec" {
		t.Fatalf("expected cached file metadata, got found=%t message=%#v", found, cached)
	}
	userMentions, err := db.Mentions("U999", 10)
	if err != nil {
		t.Fatalf("user mentions: %v", err)
	}
	richUserMentions, err := db.Mentions("U777", 10)
	if err != nil {
		t.Fatalf("rich user mentions: %v", err)
	}
	channelMentions, err := db.Mentions("C999", 10)
	if err != nil {
		t.Fatalf("channel mentions: %v", err)
	}
	if len(userMentions) != 1 || userMentions[0].Type != "user" {
		t.Fatalf("unexpected user mentions: %#v", userMentions)
	}
	if len(richUserMentions) != 1 || richUserMentions[0].Type != "user" {
		t.Fatalf("unexpected rich user mentions: %#v", richUserMentions)
	}
	if len(channelMentions) != 1 || channelMentions[0].DisplayText != "team-ops" {
		t.Fatalf("unexpected channel mentions: %#v", channelMentions)
	}
}

func TestStoreFetchStateStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "index.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	if err := db.RecordFetchState(FetchState{
		Kind:           "channel_history",
		Scope:          "C123",
		CursorOrWindow: "latest",
		Value:          map[string]any{"requested": 10, "returned": 2},
	}); err != nil {
		t.Fatalf("record fetch state: %v", err)
	}
	status := Inspect(path)
	if status.Counts["fetch_state"] != 1 {
		t.Fatalf("expected one fetch_state row, got %#v", status.Counts)
	}
	if status.LatestFetchTS["fetch_state"] == nil {
		t.Fatalf("expected latest fetch_state timestamp, got %#v", status.LatestFetchTS)
	}
}

func TestInspectLegacyV1ReadOnlyAndOpenMigrates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "index.db")
	createLegacyV1Cache(t, path)

	status := Inspect(path)
	if status.SchemaVersion != 1 {
		t.Fatalf("inspect schema version = %d, want 1", status.SchemaVersion)
	}
	if !strings.Contains(status.Notice, "next cache write") {
		t.Fatalf("expected stale schema notice, got %q", status.Notice)
	}
	if got := rawUserVersion(t, path); got != 0 {
		t.Fatalf("inspect migrated user_version to %d", got)
	}

	db, err := Open(path)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	if got := db.schemaVersion(); got != SchemaVersion {
		t.Fatalf("schema version = %d, want %d", got, SchemaVersion)
	}
	result, err := db.SearchMessages("approve launch", 10)
	if err != nil {
		t.Fatalf("search migrated messages: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected migrated normalized FTS hit, got %#v", result.Messages)
	}
	userMentions, err := db.Mentions("U999", 10)
	if err != nil {
		t.Fatalf("migrated mentions: %v", err)
	}
	channelMentions, err := db.Mentions("C999", 10)
	if err != nil {
		t.Fatalf("migrated channel mentions: %v", err)
	}
	if len(userMentions) != 1 || len(channelMentions) != 1 {
		t.Fatalf("expected migrated mentions, got users=%#v channels=%#v", userMentions, channelMentions)
	}
}

func TestStoreRejectsNewerSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "index.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rawDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	if _, err := rawDB.Exec(`PRAGMA user_version = 999`); err != nil {
		t.Fatalf("set user_version: %v", err)
	}
	if err := rawDB.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}
	if db, err := Open(path); err == nil {
		_ = db.Close()
		t.Fatalf("expected writable open to reject newer schema")
	}
	if db, err := OpenReadOnly(path); err == nil {
		_ = db.Close()
		t.Fatalf("expected read-only open to reject newer schema")
	}
}

func TestStoreCooldownStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "index.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	retryAt := time.Now().UTC().Add(time.Minute)
	if err := db.RecordCooldown("search.messages", time.Minute, retryAt, "test"); err != nil {
		t.Fatalf("record cooldown: %v", err)
	}
	cooldown, ok := db.ActiveCooldown("search.messages")
	if !ok {
		t.Fatalf("expected active cooldown")
	}
	if cooldown.Method != "search.messages" {
		t.Fatalf("unexpected cooldown: %#v", cooldown)
	}

	status := Inspect(path)
	if len(status.Cooldowns) != 1 {
		t.Fatalf("expected one cooldown, got %#v", status.Cooldowns)
	}
}

func createLegacyV1Cache(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir legacy cache dir: %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	statements := []string{
		`CREATE TABLE meta(key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`INSERT INTO meta(key, value) VALUES('schema_version', '1')`,
		`CREATE TABLE users(
			id TEXT PRIMARY KEY,
			team_id TEXT,
			name TEXT,
			real_name TEXT,
			email TEXT UNIQUE,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE channels(
			id TEXT PRIMARY KEY,
			name TEXT,
			is_channel INTEGER NOT NULL DEFAULT 0,
			is_group INTEGER NOT NULL DEFAULT 0,
			is_im INTEGER NOT NULL DEFAULT 0,
			is_mpim INTEGER NOT NULL DEFAULT 0,
			is_private INTEGER NOT NULL DEFAULT 0,
			is_archived INTEGER NOT NULL DEFAULT 0,
			num_members INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE messages(
			channel_id TEXT NOT NULL,
			ts TEXT NOT NULL,
			channel_name TEXT,
			user_id TEXT,
			username TEXT,
			text TEXT,
			thread_ts TEXT,
			root_ts TEXT,
			permalink TEXT,
			blocks_json TEXT,
			attachments_json TEXT,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(channel_id, ts)
		)`,
		`CREATE VIRTUAL TABLE messages_fts USING fts5(
			channel_id UNINDEXED,
			ts UNINDEXED,
			channel_name,
			user_id UNINDEXED,
			username,
			text,
			root_ts UNINDEXED,
			permalink UNINDEXED
		)`,
		`CREATE TABLE searches(
			query TEXT PRIMARY KEY,
			source TEXT,
			result_count INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE threads(
			channel_id TEXT NOT NULL,
			root_ts TEXT NOT NULL,
			permalink TEXT,
			message_count INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(channel_id, root_ts)
		)`,
		`CREATE TABLE thread_messages(
			channel_id TEXT NOT NULL,
			root_ts TEXT NOT NULL,
			ts TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(channel_id, root_ts, ts)
		)`,
		`CREATE TABLE api_cooldowns(
			method TEXT PRIMARY KEY,
			retry_after_seconds INTEGER NOT NULL,
			retry_at TEXT NOT NULL,
			source TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX idx_messages_root ON messages(channel_id, root_ts)`,
		`CREATE INDEX idx_channels_name ON channels(name)`,
		`CREATE INDEX idx_users_email ON users(email)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("legacy statement failed: %v\n%s", err, statement)
		}
	}
	blocks := `[{"type":"section","text":{"type":"mrkdwn","text":"Ask <#C999|team-ops> to approve launch"}}]`
	if _, err := db.Exec(
		`INSERT INTO messages(channel_id, ts, channel_name, user_id, username, text, thread_ts, root_ts, permalink, blocks_json, attachments_json, updated_at)
		 VALUES('C123', '3.000000', 'general', 'U111', 'person', 'Legacy deploy mentions <@U999>', '', '3.000000', 'https://example.slack.com/archives/C123/p3000000', ?, '', CURRENT_TIMESTAMP)`,
		blocks,
	); err != nil {
		t.Fatalf("insert legacy message: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO messages_fts(channel_id, ts, channel_name, user_id, username, text, root_ts, permalink)
		 VALUES('C123', '3.000000', 'general', 'U111', 'person', 'Legacy deploy mentions <@U999>', '3.000000', 'https://example.slack.com/archives/C123/p3000000')`,
	); err != nil {
		t.Fatalf("insert legacy fts: %v", err)
	}
}

func rawUserVersion(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	return version
}

func TestStoreFuzzySuggestions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "index.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	if err := db.UpsertMessages([]api.MessageResult{{ChannelID: "C123", TS: "1.000000", RootTS: "1.000000", Excerpt: "retrospective followup"}}); err != nil {
		t.Fatalf("upsert messages: %v", err)
	}

	result, err := db.SearchMessages("retrospektive", 10)
	if err != nil {
		t.Fatalf("search messages: %v", err)
	}
	if len(result.Suggestions) == 0 || result.Suggestions[0] != "retrospective" {
		t.Fatalf("expected retrospective suggestion, got %#v", result.Suggestions)
	}
}
