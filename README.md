# slacky - Read-only Slack search for fast context

Read-only Slack search for fast context, built for people and agents. `slacky` helps you search messages, open the right thread, and pull surrounding Slack context into JSON or copyable terminal output.

> Requires a Slack user token with read scopes. macOS and Linux.

## Install

```bash
brew install andyhtran/tap/slacky
```

To update:

```bash
brew update && brew upgrade slacky
```

<details>
<summary>Build from source</summary>

```bash
git clone https://github.com/andyhtran/slacky.git
cd slacky && go build -o slacky ./cmd/slacky
```

</details>

## First-time setup

Use the bundled setup guide when creating a Slack app or importing an existing user token:

```bash
slacky skills get setup
slacky setup wizard
slacky auth status
```

If you already have a Slack user token with the required scopes:

```bash
slacky auth import
slacky auth status
```

`auth import` prompts for the token by default. For scripts, use `--token-stdin`, `--token-file <path>`, or `--token-env <name>` instead of putting tokens in command arguments.

## Search Slack

Search live Slack results:

```bash
slacky search "from:@someone has:link"
slacky search "in:#general release"
slacky find "customer escalation"
```

Search the local cache when offline or rate-limited:

```bash
slacky search --local "release notes"
```

## Get context

Fetch messages, threads, surrounding context, or archive links:

```bash
slacky thread --channel C123 --ts 1717440000.000000
slacky context --channel C123 --ts 1717440000.000000
slacky open https://workspace.slack.com/archives/C123/p1717440000000000 --mode thread
```

Resolve channels and people:

```bash
slacky channels general
slacky user person@example.com
slacky user @someone
```

## Use with agents

`slacky skill install` ships a small discovery stub for agents. The full runtime guidance is embedded in the `slacky` binary and updates with each `brew upgrade slacky`.

```bash
slacky skill install
slacky skills get core
slacky agent-context --json
slacky schema --json
```

Then prompt naturally:

```text
use slacky to find the launch thread and summarize the decision
```

Agents should prefer `--json` for stable output and check `source` plus `cache_notice` before treating cached data as fresh.

## Safety and storage

Slack API calls are read-only. `slacky` uses user-token scopes and does not request permissions to send messages, mutate channels, change users, manage files, create webhooks, or change workspace settings.

`slacky` stores its own files under `~/.slacky/`: auth metadata, the local SQLite cache, small state files, logs, and CLI-owned skill storage. Set `SLACKY_HOME` to override this for isolated runs. Auth files are written with `0600` permissions, and secrets are redacted from status, doctor, schema, and agent-context output.

Run `slacky --help`, `slacky schema --json`, or `slacky skills get core` for the full command surface.

## Build commands

```bash
just build
just test
just ci
```

## License

MIT
