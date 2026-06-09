package app

import (
	"strings"
	"testing"

	"github.com/alecthomas/kong"
)

func TestRootHelpTextSections(t *testing.T) {
	text := rootHelpText("/tmp/slacky/cache/index.db")

	for _, want := range []string{
		"slacky - Read-only Slack Search for Fast Context",
		"Usage:",
		"Primary commands:",
		"Discovery & context:",
		"Setup & auth:",
		"Maintenance:",
		"AI agents & integrations:",
		"Global options:",
		"Search options:",
		"Slack search syntax:",
		"More help:",
		"slacky help --full",
		"slacky skill status",
		"slacky skill uninstall",
		"Index: /tmp/slacky/cache/index.db",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("root help missing %q\n%s", want, text)
		}
	}

	if strings.Contains(text, "Commands:") {
		t.Fatalf("root help should use curated sections, got generic Commands section:\n%s", text)
	}
}

func TestLimitAliasesParse(t *testing.T) {
	tests := []struct {
		args     []string
		expected int
		count    func(CLI) int
	}{
		{args: []string{"search", "--limit", "7", "release"}, expected: 7, count: func(cli CLI) int { return cli.Search.Count }},
		{args: []string{"find", "--limit", "8", "release"}, expected: 8, count: func(cli CLI) int { return cli.Find.Count }},
		{args: []string{"history", "--channel", "general", "--limit", "9"}, expected: 9, count: func(cli CLI) int { return cli.History.Count }},
		{args: []string{"channels", "--limit", "10"}, expected: 10, count: func(cli CLI) int { return cli.Channels.Count }},
	}
	for _, test := range tests {
		var cli CLI
		parser, err := kong.New(
			&cli,
			kong.Name("slacky"),
			kong.Description(appDescription),
			kong.Vars{"version": "slacky dev"},
			kong.Help(helpPrinter),
			kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
		)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parser.Parse(test.args); err != nil {
			t.Fatalf("parse %v: %v", test.args, err)
		}
		if got := test.count(cli); got != test.expected {
			t.Fatalf("parse %v count = %d", test.args, got)
		}
	}
}

func TestRootHelpTextUsesCanonicalUserCommand(t *testing.T) {
	text := rootHelpText("/tmp/slacky/cache/index.db")

	if !strings.Contains(text, "slacky user <email|@handle|user-id>") {
		t.Fatalf("root help missing canonical user command:\n%s", text)
	}
	if strings.Contains(text, "slacky resolve user <target>") || strings.Contains(text, "slacky resolve user <email|@handle|user-id>") {
		t.Fatalf("root help should not advertise resolve user compatibility alias:\n%s", text)
	}
}

func TestRootFullHelpTextIncludesDenseReference(t *testing.T) {
	parser := newTestParser(t)
	text := rootFullHelpText(parser, "/tmp/slacky/cache/index.db")

	for _, want := range []string{
		"Usage: slacky <command> [flags]",
		"Flags:",
		"-V, --version",
		"Commands:",
		"help",
		"search",
		"message",
		"Index: /tmp/slacky/cache/index.db",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("full help missing %q\n%s", want, text)
		}
	}

	if strings.Contains(text, "Primary commands:") {
		t.Fatalf("full help should use dense generated sections, got curated section:\n%s", text)
	}
	for _, removed := range []string{"message (get-message,get)", "thread (replies)", "history (fetch)"} {
		if strings.Contains(text, removed) {
			t.Fatalf("full help should not include removed alias %q\n%s", removed, text)
		}
	}
}

func newTestParser(t *testing.T) *kong.Kong {
	t.Helper()

	var cli CLI
	parser, err := kong.New(
		&cli,
		kong.Name("slacky"),
		kong.Description(appDescription),
		kong.Vars{"version": "slacky dev"},
		kong.Help(helpPrinter),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return parser
}
