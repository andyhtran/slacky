---
name: slacky
description: Use when searching Slack and getting read-only context with the slacky CLI.
---

# Slacky

Use `slacky` to search Slack, read the right thread/context, and reuse stable IDs or permalinks. `slacky` is read-only against Slack.

## Core Loop

```text
discover -> search -> context -> permalink -> reuse
```

Prefer compact JSON for agent work; add `--json --compact` to `find`, `search`, `thread`, and `context` unless the user asked for human-readable output.

Start from the user's topic:

```sh
slacky find "topic words" --json --compact
slacky search "topic words" --json --compact
```

For "what do people recommend/think/say about X", use `find` first, then group or narrow live search:

```sh
slacky find "topic recommendations" --json --compact
slacky search "topic recommend" --json --compact --group-by-thread
slacky search "topic recs" --json --compact --group-by-thread
```

When synthesizing consensus, separate actual recommendations, deal/marketplace availability, advice requests, and noise.

Use Slack syntax when it sharpens the query:

```sh
slacky search "from:@person topic words" --json --compact
slacky search "in:#channel topic words" --json --compact
slacky search "@person has:link" --json --compact
```

Re-find cached context first when the user likely needs something already seen. It is instant, offline-friendly, and costs no Slack API calls:

```sh
slacky search --local "topic words" --json --compact
slacky search --local "in:#channel topic words" --json --compact
slacky find --local "topic recommendations" --json --compact
```

## Follow Results

Prefer commands returned in JSON result `commands` fields; copy them exactly before constructing commands by hand.

For threaded hits:

- `commands.thread`: read the containing thread.
- `commands.context`: read around the exact hit timestamp.
- `commands.root_context`: read surrounding root-channel context for a reply.
- `commands.open`: open the safest Slack permalink target.

If `context` fails on a threaded hit, run `commands.thread` or use the result's `root_ts` / `thread_ts` with `slacky thread`.

## People

Resolve people when a query needs a stable handle or user ID:

```sh
slacky user person@example.com --json
slacky user @person --json
slacky user person --json
```

If a short name is ambiguous, use the suggested commands from the JSON error.

## Output And Safety

Compact JSON keeps the IDs, timestamps, identity fields, permalinks, excerpts, and follow-up commands agents need while omitting rendered terminal text.

Use `--evidence` for human-readable per-result snippets. Use `--verbose --include-rich-content` only when Slack blocks, attachments, files, or tables are relevant.

Do not expect commands that send messages, edit content, change channels, modify users, manage files, create webhooks, or change workspace settings.

## When Blocked

Use the command's suggested JSON error commands first. Common fallbacks:

```sh
slacky auth status --active --json
slacky auth refresh --json
slacky cache status --json
slacky doctor --json
slacky cache status --profile <name>
slacky cache prune --profile <name> --older-than 90 --dry-run
slacky cache clear --profile <name> --dry-run
```

Use setup/auth guides only for install, first-run auth, profile switching, or auth recovery:

```sh
slacky skills get setup
slacky skills get auth
```
