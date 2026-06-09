#!/usr/bin/env bash
set -euo pipefail

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

mkdir -p dist
CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=dev" -o dist/slacky ./cmd/slacky
bin="./dist/slacky"

run_json() {
  local name="$1"
  shift
  printf 'json: %s\n' "$name"
  "$@" > "$tmp/$name.json"
  jq -e '.ok == true or .ok == false' "$tmp/$name.json" >/dev/null
}

run_text() {
  local name="$1"
  shift
  printf 'text: %s\n' "$name"
  "$@" > "$tmp/$name.txt"
  test -s "$tmp/$name.txt"
}

run_markdown() {
  local name="$1"
  shift
  printf 'markdown: %s\n' "$name"
  "$@" > "$tmp/$name.md"
  test -s "$tmp/$name.md"
}

assert_jq() {
  local name="$1"
  local file="$2"
  local filter="$3"
  shift 3
  printf 'assert: %s\n' "$name"
  jq -e "$@" "$filter" "$file" >/dev/null
}

run_json_until() {
  local name="$1"
  local filter="$2"
  shift 2
  local attempt
  for attempt in 1 2 3 4 5 6; do
    run_json "$name" "$@"
    if jq -e "$filter" "$tmp/$name.json" >/dev/null; then
      return 0
    fi
    sleep 2
  done
  jq -e "$filter" "$tmp/$name.json" >/dev/null
}

channel="${SLACKY_LIVE_CHANNEL:-general}"
channel_typo="${SLACKY_LIVE_CHANNEL_TYPO:-genral}"
live_handle="${SLACKY_LIVE_HANDLE:-}"
live_handle_typo="${SLACKY_LIVE_HANDLE_TYPO:-$live_handle}"
live_email="${SLACKY_LIVE_USER_EMAIL:-}"
if [[ -z "$live_handle" ]]; then
  printf 'set SLACKY_LIVE_HANDLE to a visible Slack handle for live smoke\n' >&2
  exit 1
fi
channel_id="$("$bin" channels "$channel" --json | jq -r '.channels[0].id // empty')"
if [[ -z "$channel_id" ]]; then
  printf 'could not resolve live channel %q\n' "$channel" >&2
  exit 1
fi

run_text root "$bin"
run_text help "$bin" --help
run_text help-full "$bin" help --full
run_json version "$bin" version --json
run_json doctor "$bin" doctor --json
run_json paths "$bin" paths --json
run_json auth-status "$bin" auth status --json
run_json cache-status "$bin" cache status --json
run_json schema "$bin" schema --json
run_json agent-context "$bin" agent-context --json
run_json setup-manifest "$bin" setup manifest --format json --json
run_json setup-steps "$bin" setup steps --json
run_json channels "$bin" channels --json
run_text channels-text "$bin" channels
run_json channels-name "$bin" channels "$channel" --json
run_json channels-id "$bin" channels "$channel_id" --json
run_json channels-token "$bin" channels "<#$channel_id|$channel>" --json
if [[ "$channel" == "general" ]]; then
  run_json channels-fuzzy "$bin" channels "$channel_typo" --json
fi

run_text skill-summary "$bin" skill
run_json skill-status "$bin" skill status --skill-dir "$tmp/skills" --json
run_json skill-install "$bin" skill install --skill-dir "$tmp/skills" --json
run_json skill-uninstall "$bin" skill uninstall --skill-dir "$tmp/skills" --json
run_json skills-list "$bin" skills list --json
run_json skills-get-json "$bin" skills get core --json
run_markdown skills-get "$bin" skills get core
run_markdown skills-get-all "$bin" skills get --all

if [[ "${SLACKY_LIVE_SEND:-0}" != "1" ]]; then
  run_json history "$bin" history --channel "$channel" --count 5 --json
  run_json search-handle "$bin" search "@$live_handle" --count 2 --json
  live_user_id="$(jq -r '.search.meta.user_expansions[0].user_id // empty' "$tmp/search-handle.json")"
  if [[ -z "$live_user_id" ]]; then
    printf 'could not resolve live handle %q\n' "$live_handle" >&2
    exit 1
  fi
  run_json search-from-handle "$bin" search "from:@$live_handle" --count 2 --json
  assert_jq search-handle-meta "$tmp/search-handle.json" '.search.meta.slack_query == ("<@" + $id + ">")' --arg id "$live_user_id"
  assert_jq search-from-handle-meta "$tmp/search-from-handle.json" '.search.meta.slack_query == ("from:<@" + $id + ">")' --arg id "$live_user_id"
  printf 'live smoke ok channel=%s channel_id=%s mode=read-only\n' "$channel" "$channel_id"
  exit 0
fi

if ! command -v slacking >/dev/null; then
  printf 'SLACKY_LIVE_SEND=1 requires slacking on PATH\n' >&2
  exit 1
fi

stamp="$(date +%m%d%H%M%S)"
query="qwertysmoke${stamp}"
root_text="slacky smoke ${stamp} root mention @${live_handle} channel #${channel} link https://example.com/${stamp} code-span"
reply_text="slacky smoke ${stamp} thread reply ${query}"
block_text="slacky smoke ${stamp} block text rich ${query}"

root_json="$(slacking --json messages send "#$channel" --text "$root_text" --apply)"
root_ts="$(printf '%s' "$root_json" | jq -r '.ts')"
reply_json="$(slacking --json messages send "$channel_id" --text "$reply_text" --thread-ts "$root_ts" --apply)"
reply_ts="$(printf '%s' "$reply_json" | jq -r '.ts')"
blocks_json="$(jq -cn --arg text "$block_text" '[{type:"section", text:{type:"mrkdwn", text:$text}}]')"
block_json="$(slacking --json messages send "$channel_id" --text "$block_text" --blocks-json "$blocks_json" --apply)"
block_ts="$(printf '%s' "$block_json" | jq -r '.ts')"
permalink="$(slacking --json messages permalink "$channel_id" "$root_ts" | jq -r '.permalink')"
reply_permalink="$(slacking --json messages permalink "$channel_id" "$reply_ts" | jq -r '.permalink')"

run_json history-name "$bin" history --channel "$channel" --count 5 --json
run_json history-id-rich "$bin" history --channel "$channel_id" --count 5 --include-rich-content --json
run_json message-name "$bin" message --channel "$channel" --ts "$root_ts" --json
run_json message-token "$bin" message --channel "<#$channel_id|$channel>" --ts "$root_ts" --json
run_json message-id-rich "$bin" message --channel "$channel_id" --ts "$block_ts" --include-rich-content --json
run_json thread-name "$bin" thread --channel "$channel" --ts "$root_ts" --json
run_json context-root "$bin" context --channel "$channel_id" --ts "$root_ts" --before 1 --after 1 --json
run_json context-reply "$bin" context --channel "$channel_id" --ts "$reply_ts" --before 1 --after 1 --json
run_json open-message "$bin" open "$permalink" --json
run_json open-thread "$bin" open "$permalink" --mode thread --json
run_json open-reply "$bin" open "$reply_permalink" --json
run_json_until search-stamp ".results[]? | select((.excerpt // \"\") | contains(\"$stamp\"))" "$bin" search "slacky smoke ${stamp}" --count 5 --json
run_json search-local "$bin" search --local "$query" --count 5 --json
run_json find-query "$bin" find "$query" --count 5 --json
run_json search-handle "$bin" search "@$live_handle" --count 2 --json
live_user_id="$(jq -r '.search.meta.user_expansions[0].user_id // empty' "$tmp/search-handle.json")"
if [[ -z "$live_user_id" ]]; then
  printf 'could not resolve live handle %q\n' "$live_handle" >&2
  exit 1
fi
run_json search-handle-fuzzy "$bin" search "@$live_handle_typo" --count 2 --json
run_json search-from-handle "$bin" search "from:@$live_handle" --count 2 --json
run_json search-from-name "$bin" search "from:$live_handle" --count 2 --json
run_json search-from-id "$bin" search "from:$live_user_id" --count 2 --json
run_json search-channel-fuzzy "$bin" search "in:#${channel_typo} ${query}" --count 2 --json
run_json search-channel-id "$bin" search "in:${channel_id} ${query}" --count 2 --json
run_json user-handle "$bin" user "@$live_handle" --json
run_json user-handle-fuzzy "$bin" user "@$live_handle_typo" --json
run_json user-id "$bin" user "$live_user_id" --json
if [[ -n "$live_email" ]]; then
	run_json user-email "$bin" user --email "$live_email" --json
	run_json resolve-user "$bin" resolve user "$live_email" --json
fi

assert_jq search-stamp-result "$tmp/search-stamp.json" '.results[]? | select((.excerpt // "") | contains($q))' --arg q "$stamp"
assert_jq search-local-result "$tmp/search-local.json" '.results[]? | select((.excerpt // "") | contains($q))' --arg q "$query"
assert_jq search-handle-meta "$tmp/search-handle.json" '.search.meta.slack_query == ("<@" + $id + ">")' --arg id "$live_user_id"
assert_jq search-handle-fuzzy-meta "$tmp/search-handle-fuzzy.json" '.search.meta.slack_query == ("<@" + $id + ">")' --arg id "$live_user_id"
assert_jq search-from-handle-meta "$tmp/search-from-handle.json" '.search.meta.slack_query == ("from:<@" + $id + ">")' --arg id "$live_user_id"
assert_jq search-from-name-meta "$tmp/search-from-name.json" '.search.meta.slack_query == ("from:<@" + $id + ">")' --arg id "$live_user_id"
assert_jq search-from-id-meta "$tmp/search-from-id.json" '.search.meta.slack_query == ("from:<@" + $id + ">")' --arg id "$live_user_id"
assert_jq search-channel-fuzzy-meta "$tmp/search-channel-fuzzy.json" '.search.meta.channel_expansions[0].name == $channel' --arg channel "$channel"
assert_jq search-channel-id-meta "$tmp/search-channel-id.json" '.search.meta.channel_expansions[0].name == $channel' --arg channel "$channel"
assert_jq user-handle-id "$tmp/user-handle.json" '.user.id == $id' --arg id "$live_user_id"
assert_jq user-handle-fuzzy-id "$tmp/user-handle-fuzzy.json" '.user.id == $id' --arg id "$live_user_id"
assert_jq user-id-id "$tmp/user-id.json" '.user.id == $id' --arg id "$live_user_id"
assert_jq message-name-channel "$tmp/message-name.json" '.message.channel_id == $id or .results[0].channel_id == $id' --arg id "$channel_id"
assert_jq thread-root "$tmp/thread-name.json" '.thread.root_ts == $ts' --arg ts "$root_ts"
assert_jq context-root-result "$tmp/context-root.json" '.results[]? | select(.root_ts == $ts or .ts == $ts)' --arg ts "$root_ts"
assert_jq open-reply-message "$tmp/open-reply.json" '.message.ts == $ts' --arg ts "$reply_ts"
assert_jq context-reply-thread "$tmp/context-reply.json" '.thread.root_ts == $ts' --arg ts "$root_ts"

printf 'live smoke ok channel=%s channel_id=%s root_ts=%s query=%s mode=send\n' "$channel" "$channel_id" "$root_ts" "$query"
