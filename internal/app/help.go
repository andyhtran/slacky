package app

import (
	"fmt"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/andyhtran/slacky/internal/paths"
)

const appDescription = "Read-only Slack search for fast context, built for people and agents."

type HelpCmd struct {
	Full bool `help:"Show dense generated command and flag reference" name:"full"`
}

func (cmd *HelpCmd) Run(globals *Globals, parser *kong.Kong) error {
	text := rootHelpText(rootHelpIndexPath())
	if cmd.Full {
		text = rootFullHelpText(parser, rootHelpIndexPath())
	}
	if globals.JSON {
		return writeEnvelope(globals, Envelope{
			OK:      true,
			Text:    text,
			Results: []any{},
		})
	}
	_, err := fmt.Fprint(parser.Stdout, text)
	return err
}

func helpPrinter(options kong.HelpOptions, ctx *kong.Context) error {
	if ctx.Selected() != nil {
		return kong.DefaultHelpPrinter(options, ctx)
	}
	_, err := fmt.Fprint(ctx.Stdout, rootHelpText(rootHelpIndexPath()))
	return err
}

func rootHelpIndexPath() string {
	pathSet, err := paths.Resolve()
	if err != nil {
		return "~/.slacky/cache/index.db"
	}
	return pathSet.CacheDB.Path
}

func rootHelpText(indexPath string) string {
	return fmt.Sprintf(`slacky - Read-only Slack Search for Fast Context

Usage:
  slacky <command> [options]

Primary commands:
  slacky search <query>                    - Search Slack messages
  slacky search --local <query>            - Search only the local cache
  slacky find <topic>                      - Rank whole conversations for a topic
  slacky open <slack-url>                  - Open a Slack archive permalink
  slacky message --channel <c> --ts <ts>   - Fetch a single message
  slacky thread --channel <c> --ts <ts>    - Fetch a thread
  slacky context --channel <c> --ts <ts>   - Show surrounding channel context

Discovery & context:
  slacky channels [channel]                - List or resolve channels
  slacky history --channel <channel>       - Fetch bounded channel history into cache
  slacky user <email|@handle|user-id>      - Resolve a Slack user

Setup & auth:
  slacky setup wizard                      - Guided Slack app setup wizard
  slacky setup steps                       - Print manifest and Slack app setup steps
  slacky setup manifest                    - Generate a read-only Slack app manifest
  slacky auth login                        - Start Slack user-token login
  slacky auth import                       - Import an existing Slack user token
  slacky auth status                       - Show auth status
  slacky auth logout                       - Log out and remove local Slack auth

Maintenance:
  slacky doctor                            - Check auth, Slack reachability, cache, and cooldowns
  slacky paths                             - Show home/auth/cache/state/log paths
  slacky cache status                      - View local cache health
  slacky version                           - Show version information

AI agents & integrations:
  slacky schema                            - Show command/result schema as JSON
  slacky agent-context                     - Show runtime context as JSON
  slacky skills list                       - List bundled runtime skills
  slacky skills get core                   - Print version-matched runtime guidance
  slacky skills get setup                  - Print first-run setup guidance
  slacky skill status                      - Show installed agent skill state
  slacky skill install [--codex]           - Install the bundled agent skill stub
  slacky skill uninstall                   - Remove the managed agent skill stub

Global options:
  -h, --help                               - Show context-sensitive help
  -V, --version                            - Show version
      --json                               - Output as JSON
      --raw                                - Output raw/plain response where supported
      --no-color                           - Disable ANSI color (also: NO_COLOR env)
      --no-cache                           - Disable read-through cache usage for this run
      --timeout <duration>                 - Request timeout (default 30s)
      --max-rate-limit-wait <duration>     - Maximum Slack Retry-After wait (default 5s)

Search options:
  --count, --limit <n>                     - Maximum results
  --sort <score|timestamp>                 - Slack search sort
  --local                                  - Search only the local SQLite cache
  --group-by-thread                        - Return ranked threads instead of individual hits
  --evidence                               - Show detailed per-result evidence and commands
  --verbose                                - Return full message text
  --include-rich-content                   - Include Slack blocks, attachments, and files where available

Slack search syntax:
  slacky search passes queries to Slack search unless --local is set.
  Use Slack modifiers such as in:#channel, from:@user, has:link,
  before:YYYY-MM-DD, after:YYYY-MM-DD, and quoted phrases.
  @handle, from:handle, from:@handle, raw user IDs, and in:<channel>
  terms expand to Slack IDs/names when one exact or close fuzzy match resolves.
  Use slacky find for natural-language topic discovery across conversations.

More help:
  slacky <command> --help
  slacky help --full
  slacky schema

Index: %s
`, indexPath)
}

func rootFullHelpText(parser *kong.Kong, indexPath string) string {
	var b strings.Builder
	b.WriteString("Usage: slacky <command> [flags]\n\n")
	b.WriteString(appDescription)
	b.WriteString("\n\nFlags:\n")
	b.WriteString(formatHelpRows(rootFlagRows(parser.Model.Flags), 2))
	b.WriteString("\nCommands:\n")
	b.WriteString(formatHelpRows(rootCommandRows(parser.Model.Leaves(true)), 2))
	b.WriteString("\nIndex: ")
	b.WriteString(indexPath)
	b.WriteString("\n")
	return b.String()
}

func rootFlagRows(flags []*kong.Flag) [][2]string {
	rows := make([][2]string, 0, len(flags))
	for _, flag := range flags {
		if flag.Hidden {
			continue
		}
		rows = append(rows, [2]string{flag.String(), flag.Help})
	}
	return rows
}

func rootCommandRows(commands []*kong.Node) [][2]string {
	rows := make([][2]string, 0, len(commands))
	for _, command := range commands {
		if command.Hidden {
			continue
		}
		rows = append(rows, [2]string{command.Path(), command.Help})
	}
	return rows
}

func formatHelpRows(rows [][2]string, indent int) string {
	if len(rows) == 0 {
		return ""
	}
	width := 0
	for _, row := range rows {
		if len(row[0]) > width {
			width = len(row[0])
		}
	}
	prefix := strings.Repeat(" ", indent)
	var b strings.Builder
	for _, row := range rows {
		fmt.Fprintf(&b, "%s%-*s  %s\n", prefix, width, row[0], row[1])
	}
	return b.String()
}
