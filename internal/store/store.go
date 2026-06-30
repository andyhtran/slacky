package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/andyhtran/slacky/internal/api"

	_ "modernc.org/sqlite"
)

const SchemaVersion = 3

var cacheTables = []string{
	"users",
	"channels",
	"messages",
	"message_mentions",
	"searches",
	"threads",
	"thread_messages",
	"api_cooldowns",
	"fetch_state",
}

type DB struct {
	db *sql.DB
}

type Status struct {
	CachePath     string         `json:"cache_path"`
	Exists        bool           `json:"exists"`
	Openable      bool           `json:"openable"`
	SizeBytes     int64          `json:"size_bytes"`
	SchemaVersion int            `json:"schema_version"`
	Counts        map[string]int `json:"counts"`
	Cooldowns     []Cooldown     `json:"cooldowns"`
	LatestFetchTS map[string]any `json:"latest_fetch_ts"`
	FTSHealthy    bool           `json:"fts_healthy"`
	Notice        string         `json:"notice,omitempty"`
	Error         string         `json:"error,omitempty"`
}

type Cooldown struct {
	Method     string `json:"method"`
	RetryAt    string `json:"retry_at"`
	RetryAfter string `json:"retry_after"`
}

type LocalSearchResult struct {
	Messages    []api.MessageResult `json:"messages"`
	Suggestions []string            `json:"suggestions,omitempty"`
}

type MessageMention struct {
	ChannelID   string `json:"channel_id"`
	TS          string `json:"ts"`
	Type        string `json:"type"`
	TargetID    string `json:"target_id"`
	DisplayText string `json:"display_text,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

type FetchState struct {
	Kind           string `json:"kind"`
	Scope          string `json:"scope"`
	CursorOrWindow string `json:"cursor_or_window"`
	Value          any    `json:"value,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
}

type PruneResult struct {
	Cutoff          string `json:"cutoff"`
	DryRun          bool   `json:"dry_run"`
	Messages        int    `json:"messages"`
	MessageMentions int    `json:"message_mentions"`
	MessagesFTS     int    `json:"messages_fts"`
	ThreadMessages  int    `json:"thread_messages"`
	Threads         int    `json:"threads"`
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	store := &DB{db: db}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func OpenReadOnly(path string) (*DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", readOnlyDSN(path))
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	store := &DB{db: db}
	if err := store.applyReadPragmas(); err != nil {
		_ = db.Close()
		return nil, err
	}
	version := store.schemaVersion()
	if version > SchemaVersion {
		_ = db.Close()
		return nil, fmt.Errorf("cache schema version %d is newer than supported version %d", version, SchemaVersion)
	}
	return store, nil
}

func (store *DB) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	return store.db.Close()
}

func Inspect(path string) Status {
	status := emptyStatus(path)

	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			status.Notice = "cache database has not been created yet"
			return status
		}
		status.Error = err.Error()
		return status
	}
	status.Exists = true
	status.SizeBytes = info.Size()

	db, err := OpenReadOnly(path)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	defer func() {
		_ = db.Close()
	}()

	status.Openable = true
	status.SchemaVersion = db.schemaVersion()
	status.Counts = db.counts()
	status.Cooldowns = db.cooldowns()
	status.LatestFetchTS = db.latestFetches()
	status.FTSHealthy = db.ftsHealthy()
	if status.SchemaVersion == 0 {
		status.Notice = "cache schema version is missing; rebuild the cache if queries fail"
	} else if status.SchemaVersion < SchemaVersion {
		status.Notice = fmt.Sprintf("cache schema is v%d; the next cache write will migrate it to v%d", status.SchemaVersion, SchemaVersion)
	}
	return status
}

func (store *DB) UpsertMessages(messages []api.MessageResult) error {
	if len(messages) == 0 {
		return nil
	}
	tx, err := store.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	for index := range messages {
		if err := upsertMessage(tx, messages[index]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (store *DB) UpsertThread(thread api.ThreadResult) error {
	tx, err := store.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	if _, err := tx.Exec(
		`INSERT INTO threads(channel_id, root_ts, permalink, message_count, updated_at)
		 VALUES(?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(channel_id, root_ts) DO UPDATE SET
		   permalink=excluded.permalink,
		   message_count=excluded.message_count,
		   updated_at=CURRENT_TIMESTAMP`,
		thread.ChannelID, thread.RootTS, thread.Permalink, len(thread.Messages),
	); err != nil {
		return err
	}
	for index := range thread.Messages {
		message := thread.Messages[index]
		if message.ChannelID == "" {
			message.ChannelID = thread.ChannelID
		}
		if message.RootTS == "" {
			message.RootTS = thread.RootTS
		}
		if err := upsertMessage(tx, message); err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO thread_messages(channel_id, root_ts, ts, updated_at)
			 VALUES(?, ?, ?, CURRENT_TIMESTAMP)
			 ON CONFLICT(channel_id, root_ts, ts) DO UPDATE SET updated_at=CURRENT_TIMESTAMP`,
			message.ChannelID, message.RootTS, message.TS,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (store *DB) UpsertChannels(channels []api.ChannelResult) error {
	tx, err := store.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	for index := range channels {
		channel := channels[index]
		if _, err := tx.Exec(
			`INSERT INTO channels(id, name, is_channel, is_group, is_im, is_mpim, is_private, is_archived, num_members, updated_at)
			 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			 ON CONFLICT(id) DO UPDATE SET
			   name=excluded.name,
			   is_channel=excluded.is_channel,
			   is_group=excluded.is_group,
			   is_im=excluded.is_im,
			   is_mpim=excluded.is_mpim,
			   is_private=excluded.is_private,
			   is_archived=excluded.is_archived,
			   num_members=excluded.num_members,
			   updated_at=CURRENT_TIMESTAMP`,
			channel.ID, channel.Name, channel.IsChannel, channel.IsGroup, channel.IsIM, channel.IsMPIM, channel.IsPrivate, channel.IsArchived, channel.NumMembers,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (store *DB) UpsertUser(user api.UserResult) error {
	_, err := store.db.Exec(
		`INSERT INTO users(id, team_id, name, real_name, email, updated_at)
		 VALUES(?, ?, ?, ?, NULLIF(?, ''), CURRENT_TIMESTAMP)
		 ON CONFLICT(id) DO UPDATE SET
		   team_id=excluded.team_id,
		   name=excluded.name,
		   real_name=excluded.real_name,
		   email=excluded.email,
		   updated_at=CURRENT_TIMESTAMP`,
		user.ID, user.TeamID, user.Name, user.RealName, user.Email,
	)
	return err
}

func (store *DB) Channels(target string, limit int) ([]api.ChannelResult, error) {
	if limit <= 0 {
		limit = 50
	}
	args := []any{}
	where := ""
	if target != "" {
		cleanTarget := strings.TrimPrefix(strings.ToLower(target), "#")
		where = "WHERE lower(id) = ? OR lower(name) = ?"
		args = append(args, strings.ToLower(target), cleanTarget)
	}
	args = append(args, limit)
	rows, err := store.db.Query(
		`SELECT id, name, is_channel, is_group, is_im, is_mpim, is_private, is_archived, num_members
		 FROM channels `+where+`
		 ORDER BY name
		 LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()
	var channels []api.ChannelResult
	for rows.Next() {
		var channel api.ChannelResult
		if err := rows.Scan(&channel.ID, &channel.Name, &channel.IsChannel, &channel.IsGroup, &channel.IsIM, &channel.IsMPIM, &channel.IsPrivate, &channel.IsArchived, &channel.NumMembers); err != nil {
			return nil, err
		}
		channels = append(channels, channel)
	}
	return channels, rows.Err()
}

func (store *DB) UserByEmail(email string) (api.UserResult, bool, error) {
	row := store.db.QueryRow(
		`SELECT id, team_id, name, real_name, COALESCE(email, '')
		 FROM users
		 WHERE lower(email) = lower(?)
		 LIMIT 1`,
		email,
	)
	var user api.UserResult
	if err := row.Scan(&user.ID, &user.TeamID, &user.Name, &user.RealName, &user.Email); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return api.UserResult{}, false, nil
		}
		return api.UserResult{}, false, err
	}
	return user, true, nil
}

func (store *DB) UserByID(id string) (api.UserResult, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return api.UserResult{}, false, nil
	}
	row := store.db.QueryRow(
		`SELECT id, team_id, name, real_name, COALESCE(email, '')
		 FROM users
		 WHERE id = ?
		 LIMIT 1`,
		id,
	)
	var user api.UserResult
	if err := row.Scan(&user.ID, &user.TeamID, &user.Name, &user.RealName, &user.Email); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return api.UserResult{}, false, nil
		}
		return api.UserResult{}, false, err
	}
	return user, true, nil
}

func (store *DB) UserByName(name string) (api.UserResult, bool, error) {
	name = strings.TrimPrefix(strings.TrimSpace(name), "@")
	if name == "" {
		return api.UserResult{}, false, nil
	}
	row := store.db.QueryRow(
		`SELECT id, team_id, name, real_name, COALESCE(email, '')
		 FROM users
		 WHERE lower(name) = lower(?)
		 LIMIT 1`,
		name,
	)
	var user api.UserResult
	if err := row.Scan(&user.ID, &user.TeamID, &user.Name, &user.RealName, &user.Email); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return api.UserResult{}, false, err
		}
	} else {
		return user, true, nil
	}
	rows, err := store.db.Query(
		`SELECT id, team_id, name, real_name, COALESCE(email, '')
		 FROM users
		 WHERE lower(real_name) = lower(?)
		 LIMIT 2`,
		name,
	)
	if err != nil {
		return api.UserResult{}, false, err
	}
	defer func() {
		_ = rows.Close()
	}()
	matches := []api.UserResult{}
	for rows.Next() {
		var candidate api.UserResult
		if err := rows.Scan(&candidate.ID, &candidate.TeamID, &candidate.Name, &candidate.RealName, &candidate.Email); err != nil {
			return api.UserResult{}, false, err
		}
		matches = append(matches, candidate)
	}
	if err := rows.Err(); err != nil {
		return api.UserResult{}, false, err
	}
	if len(matches) != 1 {
		return api.UserResult{}, false, nil
	}
	return matches[0], true, nil
}

func (store *DB) Message(channelID string, ts string) (api.MessageResult, bool, error) {
	row := store.db.QueryRow(
		`SELECT `+messageColumns("", "")+`
		 FROM messages
		 WHERE channel_id = ? AND ts = ?
		 LIMIT 1`,
		channelID,
		ts,
	)
	message, err := scanMessage(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return api.MessageResult{}, false, nil
		}
		return api.MessageResult{}, false, err
	}
	return message, true, nil
}

func (store *DB) Thread(channelID string, rootTS string) (api.ThreadResult, bool, error) {
	messages, err := store.MessagesByRoot(channelID, rootTS)
	if err != nil {
		return api.ThreadResult{}, false, err
	}
	if len(messages) == 0 {
		return api.ThreadResult{}, false, nil
	}
	permalink := messages[0].Permalink
	row := store.db.QueryRow(
		`SELECT COALESCE(permalink, '')
		 FROM threads
		 WHERE channel_id = ? AND root_ts = ?
		 LIMIT 1`,
		channelID, rootTS,
	)
	var threadPermalink string
	if err := row.Scan(&threadPermalink); err == nil && threadPermalink != "" {
		permalink = threadPermalink
	}
	return api.ThreadResult{
		ChannelID: channelID,
		RootTS:    rootTS,
		Permalink: permalink,
		Messages:  messages,
	}, true, nil
}

func (store *DB) MessagesByRoot(channelID string, rootTS string) ([]api.MessageResult, error) {
	rows, err := store.db.Query(
		`SELECT `+messageColumns("", "")+`
		 FROM messages
		 WHERE channel_id = ? AND root_ts = ?
		 ORDER BY ts ASC`,
		channelID, rootTS,
	)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}

func (store *DB) ChannelMessages(channelID string, limit int) ([]api.MessageResult, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := store.db.Query(
		`SELECT `+messageColumns("", "")+`
		 FROM messages
		 WHERE channel_id = ?
		 ORDER BY ts DESC
		 LIMIT ?`,
		channelID, limit,
	)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}

func (store *DB) ContextMessages(channelID string, ts string, before int, after int) ([]api.MessageResult, error) {
	messages := []api.MessageResult{}
	if before > 0 {
		beforeMessages, err := store.messagesAround(channelID, ts, before, true)
		if err != nil {
			return nil, err
		}
		messages = append(messages, reverseMessageResults(beforeMessages)...)
	}
	if target, found, err := store.Message(channelID, ts); err != nil {
		return nil, err
	} else if found {
		messages = append(messages, target)
	}
	if after > 0 {
		afterMessages, err := store.messagesAround(channelID, ts, after, false)
		if err != nil {
			return nil, err
		}
		messages = append(messages, afterMessages...)
	}
	return messages, nil
}

func (store *DB) messagesAround(channelID string, ts string, limit int, before bool) ([]api.MessageResult, error) {
	operator := ">"
	order := "ASC"
	if before {
		operator = "<"
		order = "DESC"
	}
	rows, err := store.db.Query(
		`SELECT `+messageColumns("", "")+`
		 FROM messages
		 WHERE channel_id = ? AND ts `+operator+` ?
		 ORDER BY ts `+order+`
		 LIMIT ?`,
		channelID, ts, limit,
	)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows)
}

func (store *DB) RecordSearch(query string, source string, resultCount int) error {
	_, err := store.db.Exec(
		`INSERT INTO searches(query, source, result_count, updated_at)
		 VALUES(?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(query) DO UPDATE SET
		   source=excluded.source,
		   result_count=excluded.result_count,
		   updated_at=CURRENT_TIMESTAMP`,
		query, source, resultCount,
	)
	return err
}

func scanMessages(rows *sql.Rows) ([]api.MessageResult, error) {
	defer func() {
		_ = rows.Close()
	}()
	var messages []api.MessageResult
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMessage(scanner rowScanner) (api.MessageResult, error) {
	var message api.MessageResult
	var blocks string
	var attachments string
	var files string
	if err := scanner.Scan(&message.ChannelID, &message.TS, &message.ChannelName, &message.User, &message.Username, &message.Excerpt, &message.ThreadTS, &message.RootTS, &message.Permalink, &blocks, &attachments, &files); err != nil {
		return api.MessageResult{}, err
	}
	message.Excerpt = api.CleanSlackText(message.Excerpt)
	message.Blocks = rawMessage(blocks)
	message.Attachments = rawMessage(attachments)
	message.Files = rawFiles(files)
	return message, nil
}

func reverseMessageResults(messages []api.MessageResult) []api.MessageResult {
	reversed := make([]api.MessageResult, len(messages))
	for index := range messages {
		reversed[len(messages)-1-index] = messages[index]
	}
	return reversed
}

func (store *DB) RecordFetchState(state FetchState) error {
	kind := strings.TrimSpace(state.Kind)
	scope := strings.TrimSpace(state.Scope)
	window := strings.TrimSpace(state.CursorOrWindow)
	if kind == "" || scope == "" {
		return nil
	}
	if window == "" {
		window = "default"
	}
	value := ""
	if state.Value != nil {
		payload, err := json.Marshal(state.Value)
		if err != nil {
			return err
		}
		value = string(payload)
	}
	_, err := store.db.Exec(
		`INSERT INTO fetch_state(kind, scope, cursor_or_window, value_json, updated_at)
		 VALUES(?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(kind, scope, cursor_or_window) DO UPDATE SET
		   value_json=excluded.value_json,
		   updated_at=CURRENT_TIMESTAMP`,
		kind, scope, window, value,
	)
	return err
}

func (store *DB) Mentions(targetID string, limit int) ([]MessageMention, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := store.db.Query(
		`SELECT channel_id, ts, mention_type, target_id, display_text, updated_at
		 FROM message_mentions
		 WHERE target_id = ?
		 ORDER BY updated_at DESC
		 LIMIT ?`,
		targetID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()
	mentions := []MessageMention{}
	for rows.Next() {
		var mention MessageMention
		if err := rows.Scan(&mention.ChannelID, &mention.TS, &mention.Type, &mention.TargetID, &mention.DisplayText, &mention.UpdatedAt); err != nil {
			return nil, err
		}
		mentions = append(mentions, mention)
	}
	return mentions, rows.Err()
}

func (store *DB) SearchMessages(query string, limit int) (LocalSearchResult, error) {
	if limit <= 0 {
		limit = 15
	}
	ftsQuery := buildFTSQuery(query)
	if ftsQuery == "" {
		return LocalSearchResult{}, nil
	}
	messages, err := store.searchFTS(ftsQuery, limit)
	if err != nil {
		return LocalSearchResult{}, err
	}
	if len(messages) > 0 {
		return LocalSearchResult{Messages: messages, Suggestions: []string{}}, nil
	}
	suggestions := store.suggestions(query, 5)
	if len(suggestions) == 0 {
		return LocalSearchResult{Messages: []api.MessageResult{}, Suggestions: []string{}}, nil
	}
	fallbackQuery := buildFTSQuery(strings.Join(suggestions, " "))
	fallback, err := store.searchFTS(fallbackQuery, limit)
	if err != nil {
		return LocalSearchResult{}, err
	}
	if fallback == nil {
		fallback = []api.MessageResult{}
	}
	return LocalSearchResult{Messages: fallback, Suggestions: suggestions}, nil
}

func (store *DB) PruneMessagesBefore(cutoff time.Time, dryRun bool) (PruneResult, error) {
	cutoffText := sqliteTime(cutoff)
	result, err := store.pruneCounts(cutoffText)
	if err != nil || dryRun {
		result.DryRun = dryRun
		return result, err
	}
	tx, err := store.db.Begin()
	if err != nil {
		return result, err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	statements := []struct {
		query string
		args  []any
	}{
		{query: `CREATE TEMP TABLE IF NOT EXISTS temp_prune_messages(channel_id TEXT NOT NULL, ts TEXT NOT NULL, PRIMARY KEY(channel_id, ts))`},
		{query: `DELETE FROM temp_prune_messages`},
		{query: `INSERT INTO temp_prune_messages(channel_id, ts) SELECT channel_id, ts FROM messages WHERE updated_at < ?`, args: []any{cutoffText}},
		{query: `DELETE FROM messages_fts WHERE EXISTS (SELECT 1 FROM temp_prune_messages p WHERE p.channel_id = messages_fts.channel_id AND p.ts = messages_fts.ts)`},
		{query: `DELETE FROM thread_messages WHERE EXISTS (SELECT 1 FROM temp_prune_messages p WHERE p.channel_id = thread_messages.channel_id AND p.ts = thread_messages.ts)`},
		{query: `DELETE FROM messages WHERE EXISTS (SELECT 1 FROM temp_prune_messages p WHERE p.channel_id = messages.channel_id AND p.ts = messages.ts)`},
		{query: `DELETE FROM threads WHERE NOT EXISTS (SELECT 1 FROM messages m WHERE m.channel_id = threads.channel_id AND m.root_ts = threads.root_ts)`},
		{query: `DELETE FROM temp_prune_messages`},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement.query, statement.args...); err != nil {
			return result, err
		}
	}
	return result, tx.Commit()
}

func (store *DB) pruneCounts(cutoffText string) (PruneResult, error) {
	result := PruneResult{Cutoff: cutoffText}
	queries := []struct {
		dest  *int
		query string
	}{
		{dest: &result.Messages, query: `SELECT COUNT(*) FROM messages WHERE updated_at < ?`},
		{dest: &result.MessageMentions, query: `SELECT COUNT(*) FROM message_mentions mm WHERE EXISTS (SELECT 1 FROM messages m WHERE m.channel_id = mm.channel_id AND m.ts = mm.ts AND m.updated_at < ?)`},
		{dest: &result.MessagesFTS, query: `SELECT COUNT(*) FROM messages_fts f WHERE EXISTS (SELECT 1 FROM messages m WHERE m.channel_id = f.channel_id AND m.ts = f.ts AND m.updated_at < ?)`},
		{dest: &result.ThreadMessages, query: `SELECT COUNT(*) FROM thread_messages tm WHERE EXISTS (SELECT 1 FROM messages m WHERE m.channel_id = tm.channel_id AND m.ts = tm.ts AND m.updated_at < ?)`},
		{dest: &result.Threads, query: `SELECT COUNT(*) FROM threads t WHERE NOT EXISTS (SELECT 1 FROM messages m WHERE m.channel_id = t.channel_id AND m.root_ts = t.root_ts AND m.updated_at >= ?)`},
	}
	for _, item := range queries {
		if err := store.db.QueryRow(item.query, cutoffText).Scan(item.dest); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (store *DB) RecordCooldown(method string, retryAfter time.Duration, retryAt time.Time, source string) error {
	_, err := store.db.Exec(
		`INSERT INTO api_cooldowns(method, retry_after_seconds, retry_at, source, updated_at)
		 VALUES(?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(method) DO UPDATE SET
		   retry_after_seconds=excluded.retry_after_seconds,
		   retry_at=excluded.retry_at,
		   source=excluded.source,
		   updated_at=CURRENT_TIMESTAMP`,
		method, int(retryAfter.Seconds()), retryAt.UTC().Format(time.RFC3339), source,
	)
	return err
}

func (store *DB) ActiveCooldown(method string) (Cooldown, bool) {
	row := store.db.QueryRow(
		`SELECT method, retry_at, retry_after_seconds
		 FROM api_cooldowns
		 WHERE method = ? AND retry_at > ?
		 LIMIT 1`,
		method, time.Now().UTC().Format(time.RFC3339),
	)
	var cooldown Cooldown
	var seconds int
	if err := row.Scan(&cooldown.Method, &cooldown.RetryAt, &seconds); err != nil {
		return Cooldown{}, false
	}
	cooldown.RetryAfter = (time.Duration(seconds) * time.Second).String()
	return cooldown, true
}

func upsertMessage(tx *sql.Tx, message api.MessageResult) error {
	text := api.CleanSlackText(firstNonEmpty(message.TextFull, message.Excerpt))
	normalizedText := normalizeMessageText(message)
	blocks := rawString(message.Blocks)
	attachments := rawString(message.Attachments)
	files := filesString(message.Files)
	if _, err := tx.Exec(
		`INSERT INTO messages(channel_id, ts, channel_name, user_id, username, text, normalized_text, thread_ts, root_ts, permalink, blocks_json, attachments_json, files_json, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(channel_id, ts) DO UPDATE SET
		   channel_name=excluded.channel_name,
		   user_id=excluded.user_id,
		   username=excluded.username,
		   text=excluded.text,
		   normalized_text=excluded.normalized_text,
		   thread_ts=excluded.thread_ts,
		   root_ts=excluded.root_ts,
		   permalink=excluded.permalink,
		   blocks_json=excluded.blocks_json,
		   attachments_json=excluded.attachments_json,
		   files_json=excluded.files_json,
		   updated_at=CURRENT_TIMESTAMP`,
		message.ChannelID, message.TS, message.ChannelName, message.User, message.Username, text, normalizedText, message.ThreadTS, firstNonEmpty(message.RootTS, message.TS), message.Permalink, blocks, attachments, files,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM messages_fts WHERE channel_id = ? AND ts = ?`, message.ChannelID, message.TS); err != nil {
		return err
	}
	_, err := tx.Exec(
		`INSERT INTO messages_fts(channel_id, ts, channel_name, user_id, username, text, root_ts, permalink)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		message.ChannelID, message.TS, message.ChannelName, message.User, message.Username, normalizedText, firstNonEmpty(message.RootTS, message.TS), message.Permalink,
	)
	if err != nil {
		return err
	}
	return replaceMessageMentions(tx, message.ChannelID, message.TS, extractMentions(message))
}

func (store *DB) searchFTS(query string, limit int) ([]api.MessageResult, error) {
	textExpr := "m.text"
	if store.hasColumn("messages", "normalized_text") {
		textExpr = "COALESCE(NULLIF(m.text, ''), m.normalized_text)"
	}
	rows, err := store.db.Query(
		`SELECT `+messageColumns("m", textExpr)+`
		 FROM messages_fts
		 JOIN messages m ON m.channel_id = messages_fts.channel_id AND m.ts = messages_fts.ts
		 WHERE messages_fts MATCH ?
		 ORDER BY rank
		 LIMIT ?`,
		query, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()
	var messages []api.MessageResult
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

func messageColumns(alias string, textExpr string) string {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	if textExpr == "" {
		textExpr = prefix + "text"
	}
	return strings.Join([]string{
		prefix + "channel_id",
		prefix + "ts",
		"COALESCE(" + prefix + "channel_name, '')",
		"COALESCE(" + prefix + "user_id, '')",
		"COALESCE(" + prefix + "username, '')",
		"COALESCE(" + textExpr + ", '')",
		"COALESCE(" + prefix + "thread_ts, '')",
		"COALESCE(" + prefix + "root_ts, '')",
		"COALESCE(" + prefix + "permalink, '')",
		"COALESCE(" + prefix + "blocks_json, '')",
		"COALESCE(" + prefix + "attachments_json, '')",
		"COALESCE(" + prefix + "files_json, '')",
	}, ", ")
}

func (store *DB) schemaVersion() int {
	if version, err := store.userVersion(); err == nil && version > 0 {
		return version
	}
	version, ok := store.metaSchemaVersion()
	if !ok {
		return 0
	}
	return version
}

func (store *DB) counts() map[string]int {
	counts := map[string]int{}
	for _, table := range cacheTables {
		counts[table] = 0
		row := store.db.QueryRow("SELECT COUNT(*) FROM " + table)
		var count int
		if err := row.Scan(&count); err == nil {
			counts[table] = count
		}
	}
	return counts
}

func (store *DB) cooldowns() []Cooldown {
	rows, err := store.db.Query(
		`SELECT method, retry_at, retry_after_seconds
		 FROM api_cooldowns
		 WHERE retry_at > ?
		 ORDER BY retry_at ASC`,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return []Cooldown{}
	}
	defer func() {
		_ = rows.Close()
	}()
	cooldowns := []Cooldown{}
	for rows.Next() {
		var cooldown Cooldown
		var seconds int
		if err := rows.Scan(&cooldown.Method, &cooldown.RetryAt, &seconds); err == nil {
			cooldown.RetryAfter = (time.Duration(seconds) * time.Second).String()
			cooldowns = append(cooldowns, cooldown)
		}
	}
	return cooldowns
}

func (store *DB) latestFetches() map[string]any {
	queries := map[string]string{
		"users":       `SELECT MAX(updated_at) FROM users`,
		"channels":    `SELECT MAX(updated_at) FROM channels`,
		"messages":    `SELECT MAX(updated_at) FROM messages`,
		"threads":     `SELECT MAX(updated_at) FROM threads`,
		"searches":    `SELECT MAX(updated_at) FROM searches`,
		"fetch_state": `SELECT MAX(updated_at) FROM fetch_state`,
	}
	latest := map[string]any{}
	for key, query := range queries {
		var value sql.NullString
		if err := store.db.QueryRow(query).Scan(&value); err == nil && value.Valid {
			latest[key] = value.String
		}
	}
	return latest
}

func (store *DB) ftsHealthy() bool {
	var count int
	err := store.db.QueryRow(`SELECT COUNT(*) FROM messages_fts`).Scan(&count)
	return err == nil
}

func (store *DB) suggestions(query string, limit int) []string {
	queryTerms := tokenize(query)
	if len(queryTerms) == 0 {
		return nil
	}
	terms := store.cachedTerms()
	type candidate struct {
		term     string
		distance int
	}
	var candidates []candidate
	seen := map[string]bool{}
	for _, queryTerm := range queryTerms {
		for _, term := range terms {
			distance := levenshtein(queryTerm, term)
			if distance > 0 && distance <= 2 && !seen[term] {
				seen[term] = true
				candidates = append(candidates, candidate{term: term, distance: distance})
			}
		}
	}
	sort.Slice(candidates, func(i int, j int) bool {
		if candidates[i].distance == candidates[j].distance {
			return candidates[i].term < candidates[j].term
		}
		return candidates[i].distance < candidates[j].distance
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	suggestions := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		suggestions = append(suggestions, candidate.term)
	}
	return suggestions
}

func (store *DB) cachedTerms() []string {
	textExpr := "text"
	if store.hasColumn("messages", "normalized_text") {
		textExpr = "COALESCE(normalized_text, text)"
	}
	rows, err := store.db.Query(`SELECT COALESCE(` + textExpr + `, '') || ' ' || COALESCE(channel_name, '') || ' ' || COALESCE(username, '') FROM messages WHERE text IS NOT NULL OR ` + textExpr + ` IS NOT NULL LIMIT 1000`)
	if err != nil {
		return nil
	}
	defer func() {
		_ = rows.Close()
	}()
	seen := map[string]bool{}
	var terms []string
	for rows.Next() {
		var blob sql.NullString
		if err := rows.Scan(&blob); err != nil || !blob.Valid {
			continue
		}
		for _, term := range tokenize(blob.String) {
			if len(term) < 3 || seen[term] {
				continue
			}
			seen[term] = true
			terms = append(terms, term)
		}
	}
	return terms
}

func emptyStatus(path string) Status {
	status := Status{
		CachePath:     path,
		SchemaVersion: 0,
		Counts:        map[string]int{},
		Cooldowns:     []Cooldown{},
		LatestFetchTS: map[string]any{},
	}
	for _, table := range cacheTables {
		status.Counts[table] = 0
	}
	return status
}

var tokenPattern = regexp.MustCompile(`[A-Za-z0-9_@#.-]+`)

func buildFTSQuery(query string) string {
	terms := tokenize(query)
	quoted := make([]string, 0, len(terms))
	for _, term := range terms {
		quoted = append(quoted, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " OR ")
}

func tokenize(value string) []string {
	raw := tokenPattern.FindAllString(strings.ToLower(value), -1)
	terms := make([]string, 0, len(raw))
	for _, term := range raw {
		term = strings.Trim(term, ".-#@_")
		if term != "" {
			terms = append(terms, term)
		}
	}
	return terms
}

func levenshtein(a string, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return len(b)
	}
	if b == "" {
		return len(a)
	}
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	return string(raw)
}

func filesString(files []api.FileResult) string {
	if len(files) == 0 {
		return ""
	}
	payload, err := json.Marshal(files)
	if err != nil {
		return ""
	}
	return string(payload)
}

func rawFiles(value string) []api.FileResult {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var files []api.FileResult
	if err := json.Unmarshal([]byte(value), &files); err != nil {
		return nil
	}
	return files
}

func sqliteTime(value time.Time) string {
	return value.UTC().Format("2006-01-02 15:04:05")
}

func readOnlyDSN(path string) string {
	uri := url.URL{Scheme: "file", Path: path}
	query := uri.Query()
	query.Set("mode", "ro")
	uri.RawQuery = query.Encode()
	return uri.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
