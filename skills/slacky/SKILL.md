---
name: slacky
description: Use when searching Slack for fast context or setting up Slack workspace access with the read-only slacky CLI.
---

# Slacky

Use `slacky` for read-only Slack search for fast context, built for people and agents.

## Core Loop

Follow this workflow:

```text
discover -> search -> context -> permalink -> reuse
```

Start with:

```sh
slacky
slacky auth status
slacky cache status
```

Use local-only search when credentials are missing or when you only need cached data:

```sh
slacky search --local "topic words"
```

Use live search after auth is configured:

```sh
slacky search "from:@person has:link"
slacky search "from:person"
slacky search "in:#general release"
slacky search "@person"
slacky find "incident review"
```

Live search expands `@handle`, `from:handle`, `from:@handle`, raw user IDs, and `in:<channel>` terms when one exact or close fuzzy match can be resolved. Prefer `--json` for agent workflows; check `search.meta.slack_query`, `search.meta.user_expansions`, and `search.meta.channel_expansions` to see the exact query sent to Slack.

Human search output is compact by default and collects top-result follow-up commands at the bottom. Use `--evidence` for detailed per-result text plus individual thread/context commands.

Fetch precise Slack targets with channel IDs/names and timestamps:

```sh
slacky message --channel C123456 --ts 1717440000.000000
slacky thread --channel C123456 --ts 1717440000.000000
slacky context --channel C123456 --ts 1717440000.000000
slacky open https://workspace.slack.com/archives/C123456/p1717440000000000 --mode thread
```

Resolve users by email, handle/name, fuzzy handle, or raw Slack user ID:

```sh
slacky user person@example.com
slacky user @person
slacky user U123456
```

## Safety Policy

`slacky` is read-only against Slack. Do not expect commands that send messages, edit content, change channels, modify users, or mutate workspace state.

Local writes are limited to explicit setup/auth/cache/skill commands under `~/.slacky` or the selected agent skill directory. Normal search and context commands must not rewrite auth files.

Secrets are redacted. Client secrets, access tokens, refresh tokens, OAuth codes, and callback URLs should never be printed, logged, committed, or echoed into task notes.

## Output Rules

Use `--json` for stable structured output:

```sh
slacky search "topic words" --evidence
slacky search --local "topic words" --json
slacky paths --json
slacky schema --json
slacky agent-context --json
```

JSON responses include `ok`, rendered `text`, `source`, `cache_notice`, and command-specific structured fields. Warnings and diagnostics belong on stderr.

`slacky skills get core` and `slacky skills get setup` are Markdown transport commands. They print Markdown even in non-TTY shells unless `--json` is explicit.

## Setup

Use the manifest helpers before login:

```sh
slacky skills get setup
slacky setup wizard
slacky setup steps
slacky auth login
```

Use `slacky skills get setup` when the user is first installing Slacky, creating a Slack app, importing a token, or troubleshooting auth. Use this core guide once auth is ready and the task is search, context gathering, or diagnostics.

`slacky setup wizard` writes the manifest and `slacky-icon.png`, opens Slack app setup, prompts for Client ID, then starts OAuth login. `slacky setup steps` prints a manifest for copy/paste into Slack, followed by numbered Slack app creation steps. Use `slacky setup manifest --format json` when only the manifest is needed.

If a valid scoped Slack user token is already available, skip OAuth and import it without putting the token in the command line:

```sh
slacky auth import
slacky auth import --token-env SLACKY_USER_TOKEN
```

Use logout before switching users, retesting setup, or replacing credentials:

```sh
slacky auth logout
```

`auth logout` removes the local auth file only. It does not revoke the Slack token server-side and it keeps the local cache.

The Slack app should request user scopes only. Do not add bot scopes, incoming webhooks, Socket Mode, or mutating permissions.

## Diagnostics

Use:

```sh
slacky doctor
slacky auth status
slacky paths
slacky cache status
```

If a live command reports missing auth, use `slacky setup wizard` and `slacky auth status` before retrying. If cached data is enough, prefer `slacky search --local <query>` or `slacky find <topic>`; both can use the local SQLite cache without calling Slack.

For authenticated end-to-end validation in a test workspace, run:

```sh
SLACKY_LIVE_SEND=1 just smoke-live
```

This uses `slacking` only as an external fixture sender and does not modify the `slacking` repo.
