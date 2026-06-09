package app

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alecthomas/kong"

	"github.com/andyhtran/slacky/internal/output"
)

type Globals struct {
	JSON             bool          `help:"Output as JSON" name:"json"`
	Raw              bool          `help:"Output raw/plain response where supported" name:"raw"`
	NoColor          bool          `help:"Disable ANSI color (also: NO_COLOR env)" name:"no-color"`
	NoCache          bool          `help:"Disable read-through cache usage for this run" name:"no-cache"`
	Timeout          time.Duration `help:"Request timeout" default:"30s" name:"timeout"`
	MaxRateLimitWait time.Duration `help:"Maximum Slack Retry-After wait before returning a rate-limit error" default:"5s" name:"max-rate-limit-wait"`
}

type CLI struct {
	Globals

	VersionFlag kong.VersionFlag `name:"version" help:"Show version" short:"V"`

	Default      DefaultCmd      `cmd:"" default:"noargs" hidden:""`
	Help         HelpCmd         `cmd:"" help:"Show help and full command reference"`
	Version      VersionCmd      `cmd:"" help:"Show version information"`
	Doctor       DoctorCmd       `cmd:"" help:"Check auth, paths, Slack reachability, cache, and cooldowns"`
	Setup        SetupCmd        `cmd:"" help:"Generate Slack app setup helpers"`
	Auth         AuthCmd         `cmd:"" help:"Install and manage Slack user-token auth"`
	Paths        PathsCmd        `cmd:"" help:"Show effective Slacky paths"`
	Cache        CacheCmd        `cmd:"" help:"Show local cache state"`
	Schema       SchemaCmd       `cmd:"" help:"Show CLI schema as JSON"`
	AgentContext AgentContextCmd `cmd:"" name:"agent-context" help:"Show agent runtime context as JSON"`
	Skill        SkillCmd        `cmd:"" help:"Install and manage the bundled agent skill"`
	Skills       SkillsCmd       `cmd:"" help:"List and print bundled runtime guidance"`
	Search       SearchCmd       `cmd:"" help:"Search Slack messages"`
	Find         FindCmd         `cmd:"" help:"Rank whole conversations for a topic"`
	Message      MessageCmd      `cmd:"" help:"Fetch a single message"`
	Thread       ThreadCmd       `cmd:"" help:"Fetch a thread"`
	Context      ContextCmd      `cmd:"" help:"Fetch surrounding channel context"`
	Open         OpenCmd         `cmd:"" help:"Open a Slack archive permalink"`
	History      HistoryCmd      `cmd:"" help:"Fetch bounded channel history"`
	Channels     ChannelsCmd     `cmd:"" aliases:"channel" help:"List or resolve channels"`
	User         UserCmd         `cmd:"" help:"Resolve a Slack user"`
	Resolve      ResolveCmd      `cmd:"" hidden:"" help:"Resolve Slack metadata"`
}

var appVersion = "dev"

func Run(version string) int {
	appVersion = version

	var cli CLI
	parser, err := kong.New(
		&cli,
		kong.Name("slacky"),
		kong.Description(appDescription),
		kong.Vars{"version": "slacky " + appVersion},
		kong.Help(helpPrinter),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "slacky: %v\n", err)
		return 1
	}

	ctx, err := parser.Parse(os.Args[1:])
	if err != nil {
		writeStartupError(err)
		return 1
	}

	if err := applyGlobals(&cli.Globals, ctx.Command()); err != nil {
		fmt.Fprintf(os.Stderr, "slacky: %v\n", err)
		return 1
	}

	if err := ctx.Run(&cli.Globals, parser); err != nil {
		return handleRunError(&cli.Globals, err)
	}
	return 0
}

func applyGlobals(globals *Globals, command string) error {
	if globals.JSON && globals.Raw {
		return errors.New("--json and --raw are mutually exclusive")
	}
	if !globals.JSON && !globals.Raw && !isMarkdownTransport(command) && !output.IsTTY(os.Stdout) {
		globals.JSON = true
	}
	if os.Getenv("NO_COLOR") != "" {
		globals.NoColor = true
	}
	output.SetColor(!globals.NoColor && !globals.JSON && !globals.Raw && output.IsTTY(os.Stdout) && os.Getenv("TERM") != "dumb")
	return nil
}

func isMarkdownTransport(command string) bool {
	command = strings.TrimSpace(command)
	return command == "help" || strings.HasPrefix(command, "skills get")
}

func writeStartupError(err error) {
	if wantsJSON(os.Args[1:]) {
		_ = output.WriteJSON(os.Stdout, ErrorEnvelope{
			OK: false,
			Error: &AppError{
				Kind:    "parse",
				Message: err.Error(),
			},
		})
		return
	}
	fmt.Fprintf(os.Stderr, "slacky: %v\n", err)
	fmt.Fprintln(os.Stderr, "Run 'slacky --help' or 'slacky <command> --help' for usage.")
}

func wantsJSON(args []string) bool {
	for _, arg := range args {
		if arg == "--json" {
			return true
		}
	}
	return false
}

func handleRunError(globals *Globals, err error) int {
	var appErr *AppError
	if errors.As(err, &appErr) {
		if globals.JSON {
			_ = output.WriteJSON(os.Stdout, ErrorEnvelope{OK: false, Error: appErr})
		} else {
			fmt.Fprintf(os.Stderr, "Error: %v\n", appErr)
		}
		return exitCode(err)
	}

	if globals.JSON {
		_ = output.WriteJSON(os.Stdout, ErrorEnvelope{
			OK: false,
			Error: &AppError{
				Kind:    "runtime",
				Message: err.Error(),
			},
		})
	} else {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
	return 1
}
