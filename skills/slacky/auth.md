---
name: slacky-auth
description: Use when choosing a Slacky auth method, importing credentials, switching profiles, refreshing auth, or diagnosing the active Slack account/workspace.
---

# Slacky Auth

Use this guide for ongoing authentication: choose a method, add or switch profiles, refresh credentials, remove local auth, and verify which Slack identity is active. For first install or Slack app creation, use `slacky skills get setup`; for search/context tasks, use `slacky skills get core`.

## Choose A Method

Prefer OAuth app login when the user can create or install a Slack app. Use setup for first-run app creation, then login with the Client ID:

```sh
slacky auth login --name work --client-id <client-id>
```

Use token import when the user already has a scoped Slack user token. The interactive prompt hides input; env import is for local non-interactive setup:

```sh
slacky auth import --name work-token
slacky auth import --name work-token --token-env SLACKY_USER_TOKEN
```

Use browser-session import only when app installation is blocked and the user explicitly wants to use an existing Slack web session:

```sh
slacky auth import-session --wizard --name work-browser
```

The wizard is the safe default because this method needs both a Slack web `xoxc-...` token and the browser cookie named `d`, usually `xoxd-...`.

Never ask the user to paste `xoxc`, `xoxd`, cookie values, OAuth tokens, OAuth callback URLs, or client secrets into chat, logs, issue trackers, or notes. Use hidden prompts or local environment-variable import.

## Check And Switch

Start with redacted status and profile inventory, then switch by exact profile name:

```sh
slacky auth status --active --json
slacky auth list --json
slacky auth switch work
slacky auth status --active
slacky doctor
slacky search "has:link" --count 3
```

Use status to confirm readiness, source/storage labels, auth kind, token type, scopes, team, user, expiry, refresh state, mixed auth fields, and the active cache DB. Named profiles isolate caches by profile, Slack team, and Slack user.

If a command fails with `auth_profile_not_found`, run `slacky auth list` and switch using the exact name.

## Refresh And Cache

If `expires_at` is present, the profile uses rotating OAuth credentials. Slacky refreshes due tokens before live Slack API commands when `refresh_possible` is true.

```sh
slacky auth refresh
slacky auth refresh --profile work
slacky auth refresh --profile work --force --json
```

If refresh fails with `invalid_refresh_token`, run `slacky auth login --name work --client-id <client-id>` again for that profile.

Inspect, prune, or clear one profile's cache without switching profiles:

```sh
slacky cache status --profile work
slacky cache prune --profile work --older-than 90 --dry-run
slacky cache clear --profile work --dry-run
```

Use `--dry-run` first before deleting cache data.

## Browser Session And Logout

If Slack rejects the default browser User-Agent for a browser-session profile, import again with `slacky auth import-session --wizard --name work-browser --user-agent "$SLACKY_BROWSER_USER_AGENT"`. Set `SLACKY_BROWSER_USER_AGENT` locally; do not record browser-session secrets in notes or issue trackers.

Remove a named local profile or the active local auth file:

```sh
slacky auth logout --name work-browser
slacky auth logout
```

Logout removes only Slacky's local auth storage. It does not revoke a Slack OAuth token server-side and does not log out the user's browser session.
