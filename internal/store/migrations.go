package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/andyhtran/slacky/internal/api"
)

type sqlQueryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

func (store *DB) migrate() error {
	if err := store.applyWritePragmas(); err != nil {
		return err
	}
	version, err := store.userVersion()
	if err != nil {
		return err
	}
	if version > SchemaVersion {
		return fmt.Errorf("cache schema version %d is newer than supported version %d", version, SchemaVersion)
	}
	if version == 0 {
		empty, err := store.schemaEmpty()
		if err != nil {
			return err
		}
		if empty {
			return store.createFreshSchema()
		}
		metaVersion, ok := store.metaSchemaVersion()
		if !ok {
			return fmt.Errorf("cache schema version is missing; move or remove the cache database to rebuild it")
		}
		version = metaVersion
	}
	if version < SchemaVersion {
		if err := store.migrateExisting(version); err != nil {
			return err
		}
	}
	return store.validateCurrentSchema()
}

func (store *DB) applyWritePragmas() error {
	for _, statement := range []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
	} {
		if _, err := store.db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func (store *DB) applyReadPragmas() error {
	for _, statement := range []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA busy_timeout = 5000`,
	} {
		if _, err := store.db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func (store *DB) createFreshSchema() error {
	tx, err := store.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	if err := createCurrentSchema(tx); err != nil {
		return err
	}
	if err := setSchemaVersion(tx, SchemaVersion); err != nil {
		return err
	}
	if err := validateCurrentSchema(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (store *DB) migrateExisting(version int) error {
	tx, err := store.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	for version < SchemaVersion {
		switch version {
		case 1:
			if err := migrateV1ToV2(tx); err != nil {
				return err
			}
			version = 2
		case 2:
			if err := migrateV2ToV3(tx); err != nil {
				return err
			}
			version = 3
		default:
			return fmt.Errorf("unsupported cache schema version %d", version)
		}
	}
	if err := setSchemaVersion(tx, SchemaVersion); err != nil {
		return err
	}
	if err := validateCurrentSchema(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func createCurrentSchema(tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS meta(key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS users(
			id TEXT PRIMARY KEY,
			team_id TEXT,
			name TEXT,
			real_name TEXT,
			email TEXT UNIQUE,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS channels(
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
		`CREATE TABLE IF NOT EXISTS messages(
			channel_id TEXT NOT NULL,
			ts TEXT NOT NULL,
			channel_name TEXT,
			user_id TEXT,
			username TEXT,
			text TEXT,
			normalized_text TEXT,
			thread_ts TEXT,
			root_ts TEXT,
			permalink TEXT,
			blocks_json TEXT,
			attachments_json TEXT,
			files_json TEXT,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(channel_id, ts)
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
			channel_id UNINDEXED,
			ts UNINDEXED,
			channel_name,
			user_id UNINDEXED,
			username,
			text,
			root_ts UNINDEXED,
			permalink UNINDEXED
		)`,
		`CREATE TABLE IF NOT EXISTS message_mentions(
			channel_id TEXT NOT NULL,
			ts TEXT NOT NULL,
			mention_type TEXT NOT NULL,
			target_id TEXT NOT NULL,
			display_text TEXT,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(channel_id, ts, mention_type, target_id),
			FOREIGN KEY(channel_id, ts) REFERENCES messages(channel_id, ts) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS searches(
			query TEXT PRIMARY KEY,
			source TEXT,
			result_count INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS threads(
			channel_id TEXT NOT NULL,
			root_ts TEXT NOT NULL,
			permalink TEXT,
			message_count INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(channel_id, root_ts)
		)`,
		`CREATE TABLE IF NOT EXISTS thread_messages(
			channel_id TEXT NOT NULL,
			root_ts TEXT NOT NULL,
			ts TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(channel_id, root_ts, ts)
		)`,
		`CREATE TABLE IF NOT EXISTS api_cooldowns(
			method TEXT PRIMARY KEY,
			retry_after_seconds INTEGER NOT NULL,
			retry_at TEXT NOT NULL,
			source TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS fetch_state(
			kind TEXT NOT NULL,
			scope TEXT NOT NULL,
			cursor_or_window TEXT NOT NULL,
			value_json TEXT,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(kind, scope, cursor_or_window)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_root ON messages(channel_id, root_ts)`,
		`CREATE INDEX IF NOT EXISTS idx_message_mentions_target_ts ON message_mentions(mention_type, target_id, updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_message_mentions_message ON message_mentions(channel_id, ts)`,
		`CREATE INDEX IF NOT EXISTS idx_channels_name ON channels(name)`,
		`CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func migrateV1ToV2(tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS meta(key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
			channel_id UNINDEXED,
			ts UNINDEXED,
			channel_name,
			user_id UNINDEXED,
			username,
			text,
			root_ts UNINDEXED,
			permalink UNINDEXED
		)`,
		`CREATE TABLE IF NOT EXISTS message_mentions(
			channel_id TEXT NOT NULL,
			ts TEXT NOT NULL,
			mention_type TEXT NOT NULL,
			target_id TEXT NOT NULL,
			display_text TEXT,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(channel_id, ts, mention_type, target_id),
			FOREIGN KEY(channel_id, ts) REFERENCES messages(channel_id, ts) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS fetch_state(
			kind TEXT NOT NULL,
			scope TEXT NOT NULL,
			cursor_or_window TEXT NOT NULL,
			value_json TEXT,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(kind, scope, cursor_or_window)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_root ON messages(channel_id, root_ts)`,
		`CREATE INDEX IF NOT EXISTS idx_message_mentions_target_ts ON message_mentions(mention_type, target_id, updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_message_mentions_message ON message_mentions(channel_id, ts)`,
		`CREATE INDEX IF NOT EXISTS idx_channels_name ON channels(name)`,
		`CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	hasNormalizedText, err := columnExists(tx, "messages", "normalized_text")
	if err != nil {
		return err
	}
	if !hasNormalizedText {
		if _, err := tx.Exec(`ALTER TABLE messages ADD COLUMN normalized_text TEXT`); err != nil {
			return err
		}
	}
	return rebuildSearchData(tx)
}

func migrateV2ToV3(tx *sql.Tx) error {
	hasFiles, err := columnExists(tx, "messages", "files_json")
	if err != nil {
		return err
	}
	if !hasFiles {
		if _, err := tx.Exec(`ALTER TABLE messages ADD COLUMN files_json TEXT`); err != nil {
			return err
		}
	}
	return rebuildSearchData(tx)
}

func rebuildSearchData(tx *sql.Tx) error {
	type cachedMessage struct {
		ChannelID   string
		TS          string
		ChannelName string
		UserID      string
		Username    string
		Text        string
		ThreadTS    string
		RootTS      string
		Permalink   string
		Blocks      string
		Attachments string
		Files       string
	}
	hasFiles, err := columnExists(tx, "messages", "files_json")
	if err != nil {
		return err
	}
	filesExpr := "''"
	if hasFiles {
		filesExpr = "COALESCE(files_json, '')"
	}
	rows, err := tx.Query(
		`SELECT channel_id, ts, COALESCE(channel_name, ''), COALESCE(user_id, ''), COALESCE(username, ''), COALESCE(text, ''), COALESCE(thread_ts, ''), COALESCE(root_ts, ''), COALESCE(permalink, ''), COALESCE(blocks_json, ''), COALESCE(attachments_json, ''), ` + filesExpr + `
		 FROM messages`,
	)
	if err != nil {
		return err
	}
	records := []cachedMessage{}
	for rows.Next() {
		var record cachedMessage
		if err := rows.Scan(
			&record.ChannelID,
			&record.TS,
			&record.ChannelName,
			&record.UserID,
			&record.Username,
			&record.Text,
			&record.ThreadTS,
			&record.RootTS,
			&record.Permalink,
			&record.Blocks,
			&record.Attachments,
			&record.Files,
		); err != nil {
			_ = rows.Close()
			return err
		}
		records = append(records, record)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM messages_fts`); err != nil {
		return err
	}
	for index := range records {
		record := records[index]
		message := api.MessageResult{
			ChannelID:   record.ChannelID,
			ChannelName: record.ChannelName,
			TS:          record.TS,
			ThreadTS:    record.ThreadTS,
			RootTS:      record.RootTS,
			Permalink:   record.Permalink,
			User:        record.UserID,
			Username:    record.Username,
			TextFull:    record.Text,
			Excerpt:     record.Text,
			Blocks:      rawMessage(record.Blocks),
			Attachments: rawMessage(record.Attachments),
			Files:       rawFiles(record.Files),
		}
		normalizedText := normalizeMessageText(message)
		if _, err := tx.Exec(
			`UPDATE messages
			 SET normalized_text = ?
			 WHERE channel_id = ? AND ts = ?`,
			normalizedText, record.ChannelID, record.TS,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO messages_fts(channel_id, ts, channel_name, user_id, username, text, root_ts, permalink)
			 VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
			record.ChannelID, record.TS, record.ChannelName, record.UserID, record.Username, normalizedText, record.RootTS, record.Permalink,
		); err != nil {
			return err
		}
		if err := replaceMessageMentions(tx, record.ChannelID, record.TS, extractMentions(message)); err != nil {
			return err
		}
	}
	return nil
}

func (store *DB) userVersion() (int, error) {
	var version int
	if err := store.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func (store *DB) metaSchemaVersion() (int, bool) {
	row := store.db.QueryRow(`SELECT value FROM meta WHERE key = 'schema_version'`)
	var value int
	if err := row.Scan(&value); err != nil {
		return 0, false
	}
	return value, true
}

func (store *DB) schemaEmpty() (bool, error) {
	row := store.db.QueryRow(
		`SELECT COUNT(*)
		 FROM sqlite_master
		 WHERE type IN ('table', 'view', 'trigger')
		   AND name NOT LIKE 'sqlite_%'`,
	)
	var count int
	if err := row.Scan(&count); err != nil {
		return false, err
	}
	return count == 0, nil
}

func setSchemaVersion(tx *sql.Tx, version int) error {
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, version)); err != nil {
		return err
	}
	_, err := tx.Exec(
		`INSERT INTO meta(key, value)
		 VALUES('schema_version', ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		fmt.Sprint(version),
	)
	return err
}

func (store *DB) validateCurrentSchema() error {
	return validateCurrentSchema(store.db)
}

func validateCurrentSchema(queryer sqlQueryer) error {
	required := map[string][]string{
		"meta":             {"key", "value"},
		"users":            {"id", "email", "updated_at"},
		"channels":         {"id", "name", "updated_at"},
		"messages":         {"channel_id", "ts", "text", "normalized_text", "blocks_json", "attachments_json", "files_json", "updated_at"},
		"messages_fts":     {"channel_id", "ts", "text"},
		"message_mentions": {"channel_id", "ts", "mention_type", "target_id", "display_text", "updated_at"},
		"searches":         {"query", "source", "result_count", "updated_at"},
		"threads":          {"channel_id", "root_ts", "message_count", "updated_at"},
		"thread_messages":  {"channel_id", "root_ts", "ts", "updated_at"},
		"api_cooldowns":    {"method", "retry_after_seconds", "retry_at", "source", "updated_at"},
		"fetch_state":      {"kind", "scope", "cursor_or_window", "value_json", "updated_at"},
	}
	for table, columns := range required {
		for _, column := range columns {
			ok, err := columnExists(queryer, table, column)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("cache schema is missing required column %s.%s", table, column)
			}
		}
	}
	return nil
}

func (store *DB) hasColumn(table string, column string) bool {
	ok, err := columnExists(store.db, table, column)
	return err == nil && ok
}

func columnExists(queryer sqlQueryer, table string, column string) (bool, error) {
	rows, err := queryer.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer func() {
		_ = rows.Close()
	}()
	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func rawMessage(value string) json.RawMessage {
	if value == "" {
		return nil
	}
	return json.RawMessage(value)
}
