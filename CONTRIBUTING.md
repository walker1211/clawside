# Contributing

## Local setup

1. Install Go using the version declared in `go.mod`.
2. Copy `.example.env` to `.env` for local shell variables when needed.
3. Copy `configs/config.example.toml` only as a reference; generated local runtime config lives at `configs/config.toml` and is ignored by git.
4. Run `./scripts/config_builder.sh` after setting `SENDER_AUTH_KEY` when you need a local sender config.

## Build and test

Run the fast local checks before opening a pull request:

```bash
go test -count=1 ./...
```

Run the full local CI gate before pushing release-sensitive changes:

```bash
scripts/ci-local.sh clean
```

The clean gate runs `scripts/secret-scan.sh`, `scripts/secret-scan.sh --history`, gofmt, `go vet ./...`, `go test -count=1 ./...`, and build verification from tracked files.

## Secret handling

Never commit `.env`, `configs/config.toml`, databases, logs, generated artifacts, `.openclaw/trajectory-exports`, tokens, private prompts, or local absolute paths. Use placeholders in examples and keep sensitive values in local ignored files or environment variables.

## Pull requests

Pull requests should include a concise summary, test evidence, and any release or migration notes. Keep changes focused and avoid mixing unrelated refactors with feature or bug-fix work.

## Commit messages

Use Conventional Commits, for example `feat(mcp): add read-only status tool` or `fix(release): block dirty tag creation`.

## Releases

Only maintainers should create or push release tags. Use `scripts/tag-release.sh vX.Y.Z` only after explicit release authorization; it runs the clean gate, creates the tag, and pushes it to origin.
