package app

import (
	"fmt"
	"strings"

	"github.com/andyhtran/slacky/internal/api"
	"github.com/andyhtran/slacky/internal/paths"
)

type SchemaCmd struct{}

type AgentContextCmd struct{}

type commandSchema struct {
	Name        string   `json:"name"`
	Summary     string   `json:"summary"`
	Aliases     []string `json:"aliases,omitempty"`
	Examples    []string `json:"examples,omitempty"`
	ResultShape string   `json:"result_shape,omitempty"`
}

func (cmd *SchemaCmd) Run(globals *Globals) error {
	commands := []commandSchema{
		{Name: "search", Summary: "Search Slack messages; --json --compact returns agent-ready IDs, timestamps, datetime/date, identity, excerpts, and follow-up commands; live search expands @handles, from:handles, raw user IDs, and in:channels when resolvable", Examples: []string{"slacky search \"from:@someone has:link\" --json --compact", "slacky search \"from:someone\"", "slacky search \"in:#general release\"", "slacky search --evidence \"release notes\"", "slacky search --local \"release notes\" --json --compact"}, ResultShape: "Envelope{results,search,cache_notice}"},
		{Name: "find", Summary: "Rank conversations for consensus and recommendations; start here for what people think/recommend/say about something", Examples: []string{"slacky find \"paddle recommendations\" --json --compact"}, ResultShape: "Envelope{threads,search}"},
		{Name: "message", Summary: "Fetch one message", Examples: []string{"slacky message --channel C123 --ts 1717440000.000000", "slacky message --channel C123 --ts 1717440000.000000 --json --compact"}, ResultShape: "Envelope{message}"},
		{Name: "thread", Summary: "Fetch a thread", Examples: []string{"slacky thread --channel C123 --ts 1717440000.000000", "slacky thread --channel C123 --ts 1717440000.000000 --json --compact"}, ResultShape: "Envelope{thread}"},
		{Name: "context", Summary: "Fetch surrounding context", Examples: []string{"slacky context --channel C123 --ts 1717440000.000000", "slacky context --channel C123 --ts 1717440000.000000 --json --compact"}, ResultShape: "Envelope{results}"},
		{Name: "open", Summary: "Open a Slack archive URL as message, thread, or context", Examples: []string{"slacky open https://workspace.slack.com/archives/C123/p1717440000000000 --mode thread", "slacky open https://workspace.slack.com/archives/C123/p1717440000000000 --mode thread --json --compact"}, ResultShape: "Envelope{message|thread|results}"},
		{Name: "history", Summary: "Fetch bounded channel history", Examples: []string{"slacky history --channel general --count 25"}, ResultShape: "Envelope{results}"},
		{Name: "channels", Summary: "List or resolve channels", Aliases: []string{"channel"}, Examples: []string{"slacky channels", "slacky channel general"}, ResultShape: "Envelope{channels}"},
		{Name: "user", Summary: "Resolve a user by email, @handle/name, fuzzy handle, or raw Slack user ID", Examples: []string{"slacky user person@example.com", "slacky user @someone", "slacky user U123"}, ResultShape: "Envelope{user}"},
		{Name: "cache status", Summary: "Show active or named profile cache status", Examples: []string{"slacky cache status --json", "slacky cache status --profile work --json"}, ResultShape: "Envelope{cache,results}"},
		{Name: "cache clear", Summary: "Preview or remove active or named profile cache database files", Examples: []string{"slacky cache clear --profile work --dry-run", "slacky cache clear --profile work --apply"}, ResultShape: "Envelope{cache,dry_run}"},
		{Name: "setup wizard", Summary: "Guided Slack app setup wizard that writes a manifest and icon, opens Slack app setup, then starts OAuth login", Examples: []string{"slacky setup wizard", "slacky setup wizard --headless --json", "slacky setup wizard --no-login"}, ResultShape: "Envelope{results}"},
		{Name: "auth import", Summary: "Import an existing Slack user token", Examples: []string{"slacky auth import", "slacky auth import --name work-token --token-env SLACKY_USER_TOKEN"}, ResultShape: "Envelope{auth}"},
		{Name: "auth import-session", Summary: "Import an advanced Slack browser session using an xoxc token and d cookie value", Examples: []string{"slacky auth import-session --wizard --name work-browser", "slacky auth import-session --headless", "slacky auth import-session --xoxc-env SLACKY_XOXC --xoxd-env SLACKY_XOXD"}, ResultShape: "Envelope{auth|results}"},
		{Name: "auth list", Summary: "List saved auth profiles", Examples: []string{"slacky auth list --json"}, ResultShape: "Envelope{auth}"},
		{Name: "auth switch", Summary: "Switch the active auth profile", Examples: []string{"slacky auth switch work-browser"}, ResultShape: "Envelope{auth}"},
		{Name: "auth refresh", Summary: "Refresh rotating OAuth credentials for the active or named profile", Examples: []string{"slacky auth refresh", "slacky auth refresh --profile work --force --json"}, ResultShape: "Envelope{auth}"},
		{Name: "auth status", Summary: "Show redacted auth status including active profile, cache DB, expiry, refresh state, and mixed auth fields", Examples: []string{"slacky auth status --json"}, ResultShape: "Envelope{auth}"},
		{Name: "auth logout", Summary: "Remove local Slack auth so the user can re-authenticate", Examples: []string{"slacky auth logout", "slacky auth logout --name work-browser", "slacky auth logout --dry-run"}, ResultShape: "Envelope{auth,dry_run}"},
		{Name: "paths", Summary: "Show storage paths", Examples: []string{"slacky paths --json"}, ResultShape: "Envelope{paths}"},
		{Name: "agent-context", Summary: "Show agent workflow context", Examples: []string{"slacky agent-context --json"}, ResultShape: "Envelope{agent_context}"},
		{Name: "skill", Summary: "Install and manage the bundled agent skill discovery stub", Examples: []string{"slacky skill install", "slacky skill install --codex", "slacky skill status --json"}, ResultShape: "Envelope{skill}"},
		{Name: "skills list", Summary: "List bundled runtime skills", Examples: []string{"slacky skills list"}, ResultShape: "Envelope{skills}"},
		{Name: "skills get", Summary: "Print bundled runtime guidance as Markdown unless --json is explicit", Examples: []string{"slacky skills get core", "slacky skills get setup", "slacky skills get auth", "slacky skills get --all"}, ResultShape: "Markdown or Envelope{skills}"},
	}
	schema := map[string]any{
		"name":        "slacky",
		"version":     appVersion,
		"description": "Read-only Slack search for fast context, built for people and agents.",
		"globals": []map[string]string{
			{"name": "--json", "summary": "Output as JSON"},
			{"name": "--raw", "summary": "Output raw/plain response where supported"},
			{"name": "--timeout", "summary": "Request timeout"},
			{"name": "--no-cache", "summary": "Disable read-through cache usage"},
			{"name": "--max-rate-limit-wait", "summary": "Maximum Retry-After wait"},
		},
		"commands":      commands,
		"json_envelope": []string{"ok", "text", "source", "cache_notice", "results"},
		"search_syntax": map[string]any{
			"live_channel_expansion": "in:#channel, in:channel, and in:CHANNELID are rewritten to Slack search channel names when one exact or close fuzzy match resolves.",
			"live_handle_expansion":  "@handle, from:handle, from:@handle, raw USERID, and from:USERID are rewritten to Slack <@USERID> syntax when one exact or close fuzzy user match resolves.",
			"agent_metadata":         "Search JSON includes search.meta.query, search.meta.slack_query, search.meta.user_expansions, and search.meta.channel_expansions when expansion occurs. Compact search results include commands.message, commands.thread, commands.context, commands.root_context, and commands.open when available.",
			"compact_json":           "Use --json --compact for search/find/message/thread/context/open agent workflows; it omits rendered human text and cache detail while preserving IDs, timestamps, datetime/date, identity, permalinks, excerpts, and follow-up commands.",
			"recommendation_queries": "For what people think/recommend/say about a topic, start with find, then try grouped searches such as \"<topic> recommend\" and \"<topic> recs\".",
			"local_search":           "--local searches the SQLite cache and does not call Slack or expand handles/channels through Slack APIs.",
		},
	}
	text := "Schema is intended for JSON consumers.\n\nTry:\n  slacky schema --json"
	return writeEnvelope(globals, Envelope{
		OK:     true,
		Text:   text,
		Schema: schema,
	})
}

func (cmd *AgentContextCmd) Run(globals *Globals) error {
	pathSet, err := paths.Resolve()
	if err != nil {
		return err
	}
	context := map[string]any{
		"name":             "slacky",
		"version":          appVersion,
		"primary_workflow": []string{"discover", "search", "context", "permalink", "reuse"},
		"mutation_policy":  "read-only against Slack; local auth/cache writes only through explicit auth/cache commands",
		"auth": map[string]any{
			"path":             pathSet.AuthFile.Path,
			"profiles_path":    pathSet.AuthProfilesDir.Path,
			"redaction_policy": "never print client secrets, access tokens, refresh tokens, OAuth codes, or callback URLs",
		},
		"cache": map[string]any{
			"path":            pathSet.CacheDB.Path,
			"profile_scoping": "named profiles use cache/profiles/<profile>--<team_id>--<user_id>/index.db; unnamed legacy auth uses cache/index.db",
			"source_values":   []string{"slack", "cache", "slack+cache", "slack+fuzzy-cache"},
		},
		"rate_limits": map[string]any{
			"policy":              "persist Slack Retry-After cooldowns and return cached data when available",
			"max_rate_limit_wait": globals.MaxRateLimitWait.String(),
		},
		"read_methods": api.ReadMethods,
		"recipes": []string{
			"slacky setup wizard",
			"slacky setup steps",
			"slacky auth status",
			"slacky auth import",
			"slacky auth import-session --wizard --name work-browser",
			"slacky auth list",
			"slacky auth switch <name>",
			"slacky auth refresh --profile <name>",
			"slacky auth logout",
			"slacky cache status --profile <name>",
			"slacky cache clear --profile <name> --dry-run",
			"slacky find \"topic recommendations\" --json --compact",
			"slacky search \"topic recommend\" --json --compact --group-by-thread",
			"slacky thread --channel <channel-id> --ts <root-ts> --json --compact",
			"slacky skills get core",
			"slacky skills get setup",
			"slacky skills get auth",
			"slacky skill install",
			"slacky search --local \"release notes\" --json --compact",
			"slacky search \"from:@someone has:link\" --json --compact",
			"slacky search \"@someone\" --json",
			"slacky user @someone --json",
			"slacky open <slack-url> --mode thread",
		},
		"search_syntax": map[string]any{
			"channel_expansion": "Use in:#channel, in:channel, or in:CHANNELID; close typos resolve only when one channel is the clear match.",
			"handle_expansion":  "Use @handle, from:handle, from:@handle, raw USERID, or from:USERID in live search; close typos resolve only when one user is the clear match.",
			"context_followups": "Use result commands when present. For threaded hits, use root_ts/thread_ts with thread, ts with context, and root_ts with root_context when surrounding root-channel context is needed.",
		},
	}
	text := strings.Join([]string{
		"Agent context",
		"",
		fmt.Sprintf("Workflow: %s", "discover -> search -> context -> permalink -> reuse"),
		fmt.Sprintf("Auth file: %s", pathSet.AuthFile.Path),
		fmt.Sprintf("Cache DB: %s", pathSet.CacheDB.Path),
		"",
		"Use JSON for the full context:",
		"  slacky agent-context --json",
	}, "\n")
	return writeEnvelope(globals, Envelope{
		OK:           true,
		Text:         text,
		AgentContext: context,
	})
}
