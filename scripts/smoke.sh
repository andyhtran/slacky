#!/usr/bin/env bash
set -euo pipefail

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

export SLACKY_HOME="$tmp/slacky-home"

mkdir -p dist
CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=dev" -o dist/slacky ./cmd/slacky

./dist/slacky --help >/dev/null
./dist/slacky help --full >/dev/null
./dist/slacky --json >/dev/null
./dist/slacky version --json >/dev/null
./dist/slacky doctor --json >/dev/null
./dist/slacky paths --json >/dev/null
./dist/slacky cache --json >/dev/null
./dist/slacky cache status --json >/dev/null
./dist/slacky setup steps --json >/dev/null
./dist/slacky setup manifest --format json --json >/dev/null
printf '%s\n' 'xoxp-fake-token-for-smoke' | ./dist/slacky auth import --token-stdin --no-validate --json >/dev/null
./dist/slacky --json >/dev/null
./dist/slacky auth status --json >/dev/null
./dist/slacky auth clear --json >/dev/null
./dist/slacky search --local "release notes" --json >/dev/null
./dist/slacky skill >/dev/null
./dist/slacky skill status --skill-dir "$tmp/agent-skills" --json >/dev/null
./dist/slacky skill install --skill-dir "$tmp/agent-skills" --json >/dev/null
./dist/slacky skill status --skill-dir "$tmp/agent-skills" --json >/dev/null
./dist/slacky skill uninstall --skill-dir "$tmp/agent-skills" --json >/dev/null
SLACKY_CODEX_SKILL_DIR="$tmp/codex-skills" ./dist/slacky skill install --codex --json >/dev/null
SLACKY_CODEX_SKILL_DIR="$tmp/codex-skills" ./dist/slacky skill status --codex --json >/dev/null
./dist/slacky skills >/dev/null
./dist/slacky skills list --json >/dev/null
./dist/slacky skills get core >/dev/null
./dist/slacky skills get core --full >/dev/null
./dist/slacky skills get core --json >/dev/null
./dist/slacky skills get --all >/dev/null
