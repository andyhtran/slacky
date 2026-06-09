---
name: slacky-setup
description: Use when helping a user install slacky, create the Slack app manifest, import a Slack user token, run OAuth login, or verify first-time auth.
---

# Slacky Setup

Use this guide when the user asks to set up, install, authenticate, connect Slack, import a token, create a Slack app, or fix first-run auth. For day-to-day Slack search and context gathering, run:

```sh
slacky skills get core
```

## Setup Loop

Follow this workflow:

```text
detect -> choose auth path -> guide user action -> verify -> hand off
```

Start by checking whether the CLI exists and whether auth is already ready:

```sh
command -v slacky
slacky version
slacky
slacky auth status
```

If `slacky` is missing, install from Homebrew:

```sh
brew install andyhtran/tap/slacky
```

If Homebrew is unavailable, install from the public Go module:

```sh
go install github.com/andyhtran/slacky/cmd/slacky@latest
```

If the user is already inside a local source checkout, build and install that checkout:

```sh
just install
```

If neither path is appropriate, ask the user where they want to install Slacky from before running install commands.

## Choose The Auth Path

If the user already has a valid Slack user token, use token import. This avoids OAuth app setup:

```sh
slacky auth import
```

The interactive prompt hides input and expects a user token starting with `xoxp-` or `xoxe.xoxp-`. Do not ask the user to paste the token into chat, command arguments, logs, or notes.

For non-interactive setup, prefer an environment variable over command-line token arguments:

```sh
slacky auth import --token-env SLACKY_USER_TOKEN
```

If the user does not already have a token, create a Slack app from the bundled manifest:

```sh
slacky setup wizard
slacky setup steps
```

Prefer `slacky setup wizard` for a human first-run setup. It writes the manifest and `slacky-icon.png` under `~/.slacky/cache/manifests/`, opens `https://api.slack.com/apps`, walks through Slack's "Create New App -> From a manifest" flow, prompts for Client ID, then starts OAuth login.

Use `slacky setup wizard --headless --json` when an agent only needs a structured setup plan and should not open a browser, prompt, or start OAuth.

Use `slacky setup steps` when you only need the static copy/paste manifest and manual instructions. The generated app uses read-only user scopes, disables bot scopes, disables webhooks, disables Socket Mode, and disables token rotation.

Keep `token_rotation_enabled` set to `false` unless Slacky adds automatic refresh support. If Slack token rotation is enabled, Slack user tokens can expire and need to be refreshed before read commands keep working.

If the user needs to switch accounts, test setup from a clean auth state, or replace credentials, log out first:

```sh
slacky auth logout
```

This removes only the local auth file. It does not revoke the Slack token server-side and it keeps the local message cache.

## From Scratch Slack App

Guide the user through the exact sequence printed by `slacky setup wizard` or `slacky setup steps`:

```text
1. Open https://api.slack.com/apps
2. Click Create New App -> From a manifest
3. Pick the workspace to develop the app in
4. Paste the manifest from slacky setup wizard or slacky setup steps
5. Optionally upload slacky-icon.png in Display Information
6. Create the app and copy Client ID from Basic Information
7. Run slacky auth login --client-id <client-id>
```

Use a copyable command line on its own line:

```sh
slacky auth login --client-id <client-id>
```

If a browser cannot be opened from the current environment, use manual login:

```sh
slacky auth login --client-id <client-id> --manual
```

If the local callback port is busy or blocked, keep the redirect URI aligned with the manifest or rerun setup steps with the desired redirect URI.

## Verify

After either auth path, verify before moving to search:

```sh
slacky auth status
slacky doctor
slacky
```

A successful root dashboard should show `Auth: ready as @user`. Then run a small read-only smoke command:

```sh
slacky channels
slacky search "has:link" --count 3
```

Hand off to the day-to-day guidance once auth is ready:

```sh
slacky skills get core
```

## Troubleshooting

If `slacky auth import` rejects the token, check that it is a Slack user token, not a bot token or app-level token. Bot tokens usually start with `xoxb-`; app-level tokens usually start with `xapp-`.

If `slacky auth status` shows the wrong account, run `slacky auth logout`, then choose either `slacky auth import` or `slacky setup wizard`.

If `slacky auth login` reports a missing client ID, run `slacky setup wizard` and copy the Client ID from the app's Basic Information page.

If Slack rejects the OAuth callback, verify that the app manifest includes the same localhost redirect URI shown by `slacky auth login`.

If live search fails after auth, run:

```sh
slacky doctor
slacky auth status --json
```

Use `slacky search --local <query>` only when cached data is enough or when auth is not ready.
