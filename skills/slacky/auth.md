---
name: slacky-auth
description: Use when switching Slacky auth profiles, choosing an auth method, importing browser-session auth, or diagnosing which Slack account/workspace is active.
---

# Slacky Auth

Use this guide when the task is about Slacky authentication, switching accounts or workspaces, importing credentials, or verifying which Slack identity is active.

For ordinary Slack search/context tasks, start with:

```sh
slacky skills get core
```

## Check Active Auth

Start with a redacted status and profile list:

```sh
slacky auth status --json
slacky auth list --json
```

Use `auth status` to confirm readiness, auth kind, token type, team, user, expiry, refresh state, mixed auth fields, and the active cache DB. Use `auth list` to see saved profile names and which one is active.

## Switch Profiles

Switch by profile name:

```sh
slacky auth switch work
slacky auth status
```

After switching, run a small read-only check before continuing the user's search:

```sh
slacky doctor
slacky search "has:link" --count 3
```

## Refresh Expiring OAuth

After OAuth login, always inspect status:

```sh
slacky auth status --json
```

If `expires_at` is present, the profile uses rotating OAuth credentials. Slacky refreshes due tokens before live Slack API commands when `refresh_possible` is true. For diagnosis or recovery, run:

```sh
slacky auth refresh
slacky auth refresh --profile work
slacky auth refresh --profile work --force --json
```

If refresh fails with `invalid_refresh_token`, run OAuth login again for that profile:

```sh
slacky auth login --name work --client-id <client-id>
```

Named profiles use separate caches by profile, Slack team, and Slack user. To inspect or clear one cache without switching:

```sh
slacky cache status --profile work
slacky cache clear --profile work --dry-run
```

If a command fails with `auth_profile_not_found`, list profiles and switch using an exact name:

```sh
slacky auth list
slacky auth switch <name>
```

## Add Profiles

OAuth app flow:

```sh
slacky auth login --name work --client-id <client-id>
```

Existing Slack user token:

```sh
slacky auth import --name work-token
slacky auth import --name work-token --token-env SLACKY_USER_TOKEN
```

Browser session fallback:

```sh
slacky auth import-session --wizard --name work-browser
```

Browser session import is for cases where the user can access Slack in a browser but cannot install a Slack app. The wizard walks the user through copying both an `xoxc-...` token from Slack web localStorage and the value of the browser cookie named `d`, usually starting with `xoxd-...`.

If a browser-session import needs to mimic a specific browser more closely, use `--user-agent <value>` or set `SLACKY_BROWSER_USER_AGENT` before running the import command.

Never ask the user to paste `xoxc`, `xoxd`, cookie values, OAuth tokens, OAuth callback URLs, or client secrets into chat, logs, issue trackers, or notes. Use hidden prompts or environment-variable import.

## Remove Profiles

Remove a named local profile:

```sh
slacky auth logout --name work-browser
```

Remove the active local auth file:

```sh
slacky auth logout
```

Logout removes local Slacky auth storage only. It does not revoke a Slack OAuth token server-side and does not log out the user's browser session.

## Choosing A Method

Prefer OAuth app login when the user can create/install a Slack app. It is scoped and easiest to reason about.

Use `auth import` when the user already has a scoped Slack user token.

Use `auth import-session --wizard` only as an advanced fallback when app installation is blocked and the user explicitly wants to use their existing browser session.
