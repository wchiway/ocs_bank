# Repository Guidelines

## Required Development Workflow

Follow **worktree → task branch → PR → merge** for every change, including documentation.

- `main` is the integration branch. Never develop or commit directly on it.
- Fetch `origin`, then create a separate worktree and task branch: `git worktree add -b feat/short-description .worktrees/short-description origin/main`.
- Use `feat/`, `fix/`, `docs/`, or `test/` branches and focused, imperative commits.
- Push only the task branch; open a PR targeting `main` with `gh pr create`.
- Review the diff and pass applicable checks before `gh pr merge --squash`. Update local `main` only by fast-forwarding merged changes.
- Start each phase from updated `origin/main` in its own worktree. Never use `git push --all` or `--mirror`: local `legacy/python` contains historical credentials and must remain local.
- GitHub currently refuses private-repository branch protection on this account's plan (HTTP 403). This limitation does not waive the PR workflow.

## Project Structure

The Go rewrite uses chi and SQLite. `cmd/server` assembles the service; `internal/config`, `httpapi`, `question`, `provider`, and `store` separate configuration, HTTP contracts, business logic, AI protocols, and persistence. `migrations` holds versioned SQL. See `docs/go-rewrite-plan.md` for phases and acceptance criteria. Legacy Python lives on `legacy/python`, not `main`.

## Build, Style, and Tests

Use `gofmt`, idiomatic Go names, small interfaces at dependency boundaries, parameterized SQL, and context-aware HTTP/database calls. Keep provider-specific payloads out of business logic.

Run `go build ./...`, `go vet ./...`, `go test ./...`, and `go test -race ./...` before implementation PRs. Place `*_test.go` alongside code. Use `httptest` upstreams and temporary SQLite files; automated tests must not require real credentials. Documentation-only changes require diff checks.

## PR and Security Requirements

Describe behavior changes, related issues, validation results, and OCS compatibility impacts. API keys come from `AI_API_KEY` in `.env` beside the JSON configuration; never add API key settings to JSON. Commit only the empty `.env.example` template. Never commit API keys, live configuration, databases, or sensitive logs. Verify TLS, bound upstream calls, and never cache errors as answers. Keep `main` and inactive worktrees clean.

## Secret File Access Prohibition

Agents must not read `.env` or other real dotenv secret files, directly or indirectly, including through tools, scripts, searches, subprocesses, or delegated agents. Do not print, copy, upload, or request their contents. Do not run the application, `config.Load`, or diagnostic commands against real configuration if that would load a secret file. Runtime application loading of `.env` remains intentional; this restriction applies to agent access during development and troubleshooting.

Use source code, explicitly whitelisted non-secret configuration fields, sanitized diagnostics, and isolated tests with agent-created dummy credentials. Tests may use temporary synthetic dotenv fixtures, but must never access user secret files. Only the empty `.env.example` template may be inspected or committed.
