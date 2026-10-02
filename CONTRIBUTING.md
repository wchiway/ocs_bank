# Contribution Workflow

- `main` is the integration branch. Never develop or commit directly on it.
- Fetch `origin` and create a task branch in a separate Git worktree: `git worktree add -b feat/short-description .worktrees/short-description origin/main`.
- Use `feat/`, `fix/`, `docs/`, or `test/` branch prefixes. Keep commits focused and use imperative messages.
- Push only the intended task branch. Never use `git push --all` or `--mirror`; local `legacy/python` includes historical credentials and must remain local.
- Open a PR targeting `main` using `gh pr create`. Describe behavior, related issues, validation, and compatibility changes.
- Merge only through the PR after review and applicable checks succeed. Use `gh pr merge --squash`; do not push implementation commits directly to `main`.
- Once Go code exists, required local checks are `gofmt`, `go vet ./...`, `go test ./...`, and `go test -race ./...`. Document-only PRs require diff and link checks.
- Provider tests use `httptest` fixtures; database tests use temporary files. CI must not require real API credentials.
- Never commit API keys, live configuration, database contents, or logs containing credentials.

`AGENTS.md` defines the mandatory workflow and Go development rules. The target Go architecture and acceptance criteria are in `docs/go-rewrite-plan.md`; this workflow applies to all new work.
