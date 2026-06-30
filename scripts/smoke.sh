#!/usr/bin/env bash
set -euo pipefail

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

export SLACKY_HOME="$tmp/slacky-home"

mkdir -p dist
CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=dev" -o dist/slacky ./cmd/slacky

assert_json() {
  local name="$1"
  shift
  local out
  out="$("$@")"
  case "${out:0:1}" in
    \{|\[) ;;
    *) printf 'expected JSON for %s, got: %.120s\n' "$name" "$out" >&2; return 1 ;;
  esac
}

./dist/slacky --help >/dev/null
./dist/slacky -h >/dev/null
./dist/slacky help >/dev/null
./dist/slacky help --full >/dev/null
assert_json root ./dist/slacky --json
assert_json version ./dist/slacky version --json
assert_json doctor ./dist/slacky doctor --json
assert_json paths ./dist/slacky paths --json
assert_json cache ./dist/slacky cache --json
assert_json cache-status ./dist/slacky cache status --json
assert_json cache-prune ./dist/slacky cache prune --older-than 30 --dry-run --json
./dist/slacky setup steps --json >/dev/null
assert_json setup-manifest ./dist/slacky setup manifest --format json --json
printf '%s\n' 'xoxp-fake-token-for-smoke' | ./dist/slacky auth import --token-stdin --no-validate --json >/dev/null
assert_json root-auth ./dist/slacky --json
assert_json auth-status ./dist/slacky auth status --json
assert_json auth-status-active ./dist/slacky auth status --active --json
assert_json auth-list ./dist/slacky auth list --json
assert_json auth-clear ./dist/slacky auth clear --json
assert_json search-local ./dist/slacky search --local "release notes" --json
assert_json find-local ./dist/slacky find --local "example topic" --count 3 --json
./dist/slacky skill >/dev/null
assert_json skill-status ./dist/slacky skill status --skill-dir "$tmp/agent-skills" --json
assert_json skill-install ./dist/slacky skill install --skill-dir "$tmp/agent-skills" --json
assert_json skill-status-installed ./dist/slacky skill status --skill-dir "$tmp/agent-skills" --json
assert_json skill-uninstall ./dist/slacky skill uninstall --skill-dir "$tmp/agent-skills" --json
SLACKY_CODEX_SKILL_DIR="$tmp/codex-skills" assert_json skill-install-codex ./dist/slacky skill install --codex --json
SLACKY_CODEX_SKILL_DIR="$tmp/codex-skills" assert_json skill-status-codex ./dist/slacky skill status --codex --json
./dist/slacky skills >/dev/null
./dist/slacky skills list >/dev/null
assert_json skills-list ./dist/slacky skills list --json
./dist/slacky skills get core >/dev/null
assert_json skills-get-core ./dist/slacky skills get core --json
