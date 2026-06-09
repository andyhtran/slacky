# Claude Code - slacky

Read-only Slack search for fast context, built for people and agents.

## Development

- **Build**: `just build`
- **Test**: `just test`
- **Full CI gate locally**: `just ci` (format check + vet + lint + test + smoke)
- **Format**: `just fmt` - uses `gofumpt`, not `gofmt`. CI enforces this and will fail on `gofmt`-only formatted code.

## Code style

- Formatter is **gofumpt** (`just fmt` before committing). The CI lint step runs `gofumpt -l .` and fails if any file differs.
- Linter is **golangci-lint v2** (`just lint`).
- Keep terminal output compact and copyable. Prefer short `Next:` and `More:` blocks with commands on their own lines.
- Keep `--json` output stable and agent-friendly. JSON responses should use the envelope shape with `ok`, rendered `text`, `source`, optional `cache_notice`, and command-specific structured fields.

## Product constraints

- `slacky` is read-only against Slack. Do not add commands or scopes that send messages, edit messages, mutate channels, mutate users, manage files, create webhooks, or change workspace settings.
- Use Slack user tokens, not bot tokens.
- Keep token handling out of command arguments where practical. Prefer interactive prompt, stdin, env var, or token file import paths.
- Redact secrets in status, doctor, schema, agent-context, logs, docs, and tests.

## PR and merge flow

1. Push feature branch, open PR against `main`
2. CI runs lint and tests on Linux and macOS
3. Squash merge into `main`

## Release flow

Releases are automated by GoReleaser. Pushing a version tag triggers the release workflow, which builds binaries for linux/darwin (amd64/arm64), creates a GitHub Release, and updates the Homebrew formula in `andyhtran/homebrew-tap` automatically.

1. Update `CHANGELOG.md` with the new version and date
2. Commit the changelog update to `main`
3. Tag: `git tag vX.Y.Z && git push origin vX.Y.Z`
4. The `Release` GitHub Action handles everything else

After release, users get it via `brew upgrade slacky`.
