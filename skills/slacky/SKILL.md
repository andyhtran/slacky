---
name: slacky
description: Use when searching Slack and getting read-only context with the slacky CLI.
---

# Slacky

Use `slacky` to find Slack messages, open the right thread, and pull concise context for a task.

## Core Loop

```text
search -> narrow -> thread/context -> reuse
```

Start with the user's topic directly:

```sh
slacky search "topic words" --json --compact
slacky find "incident review" --json --compact
```

Use live Slack search syntax when useful:

```sh
slacky search "from:@person has:link" --json --compact
slacky search "in:#general release" --json --compact
slacky search "@person topic words" --json --compact
```

After a broad search finds a likely person or topic, narrow the query:

```sh
slacky search "from:@person topic words" --json --compact
slacky search "in:#channel topic words" --json --compact
```

Use local-only search when cached data is enough or live Slack is unavailable:

```sh
slacky search --local "topic words" --json --compact
```

## Follow Results

Prefer commands returned in JSON result `commands` fields. For threaded hits:

- Use `commands.thread` to read the containing thread.
- Use `commands.context` for the exact hit timestamp.
- Use `commands.root_context` when the hit is a reply and surrounding root-channel context is needed.
- Use `commands.open` when a Slack permalink is the safest target.

Manual forms:

```sh
slacky thread --channel C123456 --ts 1717440000.000000 --json
slacky context --channel C123456 --ts 1717440000.000100 --json
slacky open https://workspace.slack.com/archives/C123456/p1717440000000100 --mode thread --json
```

If `context` fails on a threaded hit, run the `thread` command using the result's `root_ts` or `thread_ts`.

## People

Resolve people when a query needs a stable handle or user ID:

```sh
slacky user person@example.com --json
slacky user @person --json
slacky user person --json
```

If a short name is ambiguous, use the suggested commands from the JSON error.

## Output

For search and find, prefer compact JSON:

```sh
slacky search "topic words" --json --compact
slacky find "topic words" --json --compact
```

Compact results omit rendered human text and include the IDs, timestamps, permalinks, excerpts, and follow-up commands agents need.

Use `--evidence` only when human-readable per-result text is needed:

```sh
slacky search "topic words" --evidence
```

`slacky` is read-only against Slack. Do not expect commands that send messages, edit content, change channels, modify users, or change workspace settings.

## When Blocked

If auth, cache, or Slack API errors block the task, use the command's suggested JSON error commands first. Useful fallback commands:

```sh
slacky auth status --json
slacky cache status --json
slacky doctor --json
slacky skills get setup
```
