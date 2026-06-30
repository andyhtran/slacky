---
name: slacky-setup
description: Use when installing slacky, creating the first Slack app, running first OAuth login, or verifying first-time auth.
---

# Slacky Setup

Use this guide for first-run setup: install the CLI, create the Slack app when needed, complete first login, verify auth, then hand off. For profile switching, token import details, browser-session fallback, refresh, or recovery, use `slacky skills get auth`. For Slack search and context gathering, use `slacky skills get core`.

## Setup Loop

```text
detect -> install -> first login -> verify -> hand off
```

Check whether `slacky` exists and whether auth is already ready:

```sh
command -v slacky
slacky version
slacky auth status
```

If auth is ready, skip setup and hand off to `slacky skills get core`.

## Install

```sh
brew install andyhtran/tap/slacky
go install github.com/andyhtran/slacky/cmd/slacky@latest
```

Use Homebrew when available. Use `go install` when Homebrew is unavailable. If the user is inside a local source checkout, build that checkout instead:

```sh
just install
```

If none of those paths fits, ask where they want to install Slacky from before running install commands.

## First Login

Prefer OAuth app login when the user can create or install a Slack app:

```sh
slacky setup wizard
```

The wizard writes the manifest/icon locally, opens Slack app setup, asks for the Client ID, then starts OAuth login. The generated app is read-only: user scopes only, no bot scopes, no webhooks, no Socket Mode, and no workspace mutation scopes.

Use `slacky setup wizard --headless --json` when an agent needs a structured plan and must not open a browser, prompt, or start login. Use `slacky setup steps` when the user needs copy/paste setup instructions without the wizard.

If setup cannot launch a browser or the user already created the app, start login with the Client ID:

```sh
slacky auth login --client-id <client-id>
slacky auth login --client-id <client-id> --manual
```

If the user already has a Slack user token or explicitly needs browser-session import, hand off to auth instead of duplicating those flows:

```sh
slacky skills get auth
slacky auth import
slacky auth import-session --wizard
```

Never ask the user to paste Slack tokens, browser cookies, OAuth callback URLs, or client secrets into chat, command arguments, logs, issue trackers, or notes. Use hidden prompts or local environment variables.

## Verify

Verify before moving to search:

```sh
slacky auth status --json
slacky doctor
slacky paths
slacky
```

A successful root dashboard should show `Auth: ready as @user`. Run a small read-only smoke command:

```sh
slacky channels
slacky search "has:link" --count 3
```

Then hand off to the day-to-day guide:

```sh
slacky skills get core
```

## First-Run Troubleshooting

If `slacky auth login` reports a missing Client ID, run `slacky setup wizard` or `slacky setup steps`, then copy the Client ID from the app's Basic Information page.

If Slack rejects the OAuth callback, verify that the app manifest includes the same localhost redirect URI shown by `slacky auth login`.

If `slacky auth import` rejects a token, confirm it is a Slack user token, not a bot token (`xoxb-`) or app-level token (`xapp-`), then use `slacky skills get auth`.

If live search fails after auth, run `slacky doctor` and `slacky auth status --json`. Use `slacky search --local <query>` only when cached data is enough.
