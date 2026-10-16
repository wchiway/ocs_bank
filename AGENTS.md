# Repository Guidelines

## Project Overview

**OCS Bank** is a high-performance local question bank and AI answer caching proxy designed for the OCS (Online Course Script) browser extension. Originally written in Python, the project has been fully rewritten in modern Go (Go 1.27.1) using `go-chi` and pure-Go SQLite (`modernc.org/sqlite`).

The system sits between the user's browser script and upstream AI providers (OpenAI, Anthropic, or compatible APIs), providing:
1. **Low-latency Caching & Deduplication**: Fast local SQLite lookups, human-reviewed answer precedence, and single-flight inflight request deduplication.
2. **Multi-Protocol AI Integration**: Unified question answering across Chat Completions, Anthropic Messages, and OpenAI Responses protocols.
3. **Dual Execution Modes**: A standalone headless server daemon (`cmd/server`) for CLI/Docker deployments and an integrated desktop GUI (`cmd/desktop`) powered by MyGo (`github.com/egoist/mygo`) using the OS native WebView.
4. **Offline Database Maintenance & Migration**: Dedicated CLI utilities (`cmd/maintain`, `cmd/migrate`) for backup, pruning, statistics, and migration of legacy database files.

---

## Required Development Workflow

Follow **worktree → task branch → PR → merge** for every change, including documentation.

- `main` is the integration branch. Never develop or commit directly on it.
- Fetch `origin`, then create a separate worktree and task branch:
  ```bash
  git fetch origin
  git worktree add -b <type>/short-description .worktrees/short-description origin/main
  ```
- Use `feat/`, `fix/`, `docs/`, or `test/` branch prefixes and focused, imperative commit messages.
- Push only the task branch; open a PR targeting `main` with `gh pr create`.
- Review the diff and pass all applicable checks (`gofmt`, `go vet`, `go test -race`, Node tests) before `gh pr merge --squash`.
- Update local `main` only by fast-forwarding merged changes:
  ```bash
  git checkout main && git pull --ff-only origin main
  ```
- Remove the completed worktree:
  ```bash
  git worktree remove .worktrees/short-description
  ```
- Start each phase from updated `origin/main` in its own worktree. Never use `git push --all` or `--mirror`: local `legacy/python` contains historical credentials and must remain local.
- GitHub currently refuses private-repository branch protection on this account's plan (HTTP 403). This limitation does not waive the PR workflow.

---

## Architecture & Data Flow

### System Architecture

The codebase follows a modular clean/hexagonal architecture with strict dependency boundaries:

```
[ Browser / OCS Client ]
          │
          ▼
   [ httpapi (Chi) ] ◄─────── [ cmd/desktop (MyGo WebView) ]
          │
          ▼
  [ question.Service ]
     ├──► [ store.SQLite (reviewed_answers) ] ── (Priority 1: Human Curated)
     ├──► [ store.SQLite (answers cache) ]    ── (Priority 2: Valid AI Cache)
     └──► [ provider.Client ]                 ── (Priority 3: Upstream AI)
               ├── Chat Completions Adapter
               ├── Messages Adapter
               └── Responses Adapter
```

### End-to-End Query Data Flow

1. **Request Intake**: `httpapi.Router` receives HTTP GET/POST queries at `/ocs/query` or `/api/query`. Parameters are parsed, validated, and normalized in `httpapi/query.go`.
2. **Authentication & Rate Limiting**: If an access token is configured, requests are validated against `OCS_ACCESS_TOKEN`. Requests pass through an in-memory token bucket rate limiter.
3. **Singleflight Deduplication**: `question.Service` normalizes question text and options to produce a canonical hash (`question/types.go`). Identical concurrent in-flight questions share a single execution promise via `golang.org/x/sync/singleflight`.
4. **Knowledge Base Priority Lookup**:
   - **Priority 1 (`reviewed_answers`)**: Checks for human-verified answers. If found, the answer is returned immediately (cache hits never overwrite reviewed answers).
   - **Priority 2 (`answers` cache)**: Checks the SQLite cache for unexpired entries matching question hash, type, model, and reasoning parameters.
5. **AI Upstream Generation**:
   - If cache misses, the request acquires a slot in bounded semaphores (`max_concurrent` and `max_pending`).
   - `provider.Client` executes the request against the configured AI backend with jittered exponential backoff retries.
   - Raw output is parsed as JSON (with markdown-fence unwrapping and typo tolerance in `question/answer.go`), then strictly validated against question options and type constraints in `question/validate.go`.
6. **Telemetry & Audit Persistence**:
   - Upstream token usage, durations, and status codes are logged into `upstream_calls` via `store/calls.go`.
   - In-memory counters are incremented in `telemetry.Metrics`.
   - Valid AI answers are persisted to `answers` cache with the configured TTL (`cache_ttl_hours`). Conflicting stale cache entries are invalidated.
7. **Response Formatting**: Returns standard OCS format (`{"code": 1, "data": "..."}`) or API JSON format. Failures return business error codes or safe generic error messages.

### Desktop Architecture

- `cmd/desktop/main.go` initializes `github.com/egoist/mygo`, binding `desktopUI` methods (`Settings`, `SaveSettings`, `Start`, `Stop`, `Status`, `ExportOCS`, etc.) to the system WebView.
- The web interface (`cmd/desktop/assets/`) is pure vanilla HTML/CSS/JS embedded directly into the Go executable via `go:embed`.
- The desktop controller manages an in-process instance of the Go HTTP service on loopback (`127.0.0.1`), with atomic two-phase persistence for configuration (`config.json`) and credentials (`.env`) in the OS user configuration directory.

---

## Key Directories

| Directory | Purpose |
| --- | --- |
| `cmd/server/` | Standalone CLI server daemon entry point. |
| `cmd/desktop/` | MyGo desktop application entry point and embedded assets (`cmd/desktop/assets/`). |
| `cmd/maintain/` | Offline database maintenance CLI (stats, prune, backup, restore). |
| `cmd/migrate/` | Offline database migration CLI (legacy database import into `reviewed_answers`). |
| `internal/config/` | JSON configuration and `.env` secret parsing, validation, and error sanitization. |
| `internal/desktop/` | Desktop controller, lifecycle state machine, settings persistence, and OCS export. |
| `internal/httpapi/` | Chi router, query normalization, rate limiting, token auth, diagnostics endpoints. |
| `internal/provider/` | Upstream AI client, retry policies, and protocol adapters (`chat`, `message`, `response`). |
| `internal/question/` | Domain logic: question typing, validation, answer parsing, cache key derivation, error classification. |
| `internal/store/` | SQLite persistence (`modernc.org/sqlite`), WAL configuration, migrations, cache, calls audit. |
| `internal/telemetry/` | Process-lifetime metrics counters, snapshots, and call recorder interfaces. |
| `internal/legacy/` | Read-only scanner and validator for legacy SQLite databases. |
| `migrations/` | Versioned SQL schema files embedded into binary (`001_answers.sql`, `002_reviewed_calls.sql`). |
| `docs/` | Architecture specs, desktop guide, maintenance runbooks, and release documentation. |
| `tests/` | Black-box server integration tests (`server_test.go`) and Node test runner suites (`*.test.mjs`). |
| `.github/workflows/` | CI/CD pipelines (Go checks, desktop matrix builds, GHCR container releases). |

---

## Development Commands

### Building

```bash
# Build all Go binaries
go build ./...

# Build specific CLI binaries
CGO_ENABLED=0 go build -o bin/server ./cmd/server
CGO_ENABLED=0 go build -o bin/maintain ./cmd/maintain
CGO_ENABLED=0 go build -o bin/migrate ./cmd/migrate

# Build desktop application for current platform (requires no CGO)
CGO_ENABLED=0 go tool mygo build

# Cross-platform desktop builds
CGO_ENABLED=0 go tool mygo build -platform linux/amd64,linux/arm64
CGO_ENABLED=0 go tool mygo build -platform windows/amd64,windows/arm64
CGO_ENABLED=0 go tool mygo build -platform darwin/universal -skip-notarize
```

### Testing & Verification

```bash
# Verify formatting (must produce no output)
test -z "$(gofmt -l .)"

# Static analysis
go vet ./...

# Standard test suite
go test ./...

# Race condition detection (mandatory before PRs)
go test -race ./...

# Run JavaScript / Node test suites (requires Node.js 22+, standard library only)
node --test tests/*.test.mjs

# Container smoke tests
docker build -t ocs-bank:ci .
docker run --rm ocs-bank:ci -h
docker run --rm --entrypoint /maintain ocs-bank:ci -h
```

### Running Locally

```bash
# Run server with local configuration
go run ./cmd/server -config config.json

# Run desktop app with isolated test data directory
go run ./cmd/desktop -data-dir /tmp/test-ocs-desktop

# Run database maintenance inspection
go run ./cmd/maintain -db question_bank.db stats
```

---

## Code Conventions & Common Patterns

### Formatting & Style
- Standard `gofmt` is enforced in CI.
- Idiomatic Go names: short, descriptive identifiers; camelCase for unexported, PascalCase for exported.
- Dependency boundaries rely on small, cohesive interfaces defined by consumers (e.g. `question.Repository`, `question.ReviewedRepository`, `question.Generator`, `telemetry.Recorder`).
- Keep provider-specific payloads and external protocol models completely isolated inside `internal/provider`. Domain logic operates solely on `question.Question` and `question.Result`.

### Error Handling & Security
- **Typed Domain Errors**: Use errors defined in `internal/question/types.go` (`ErrQuestionInvalid`, `ErrUpstreamUnavailable`, `ErrOverloaded`, `ErrOutputTruncated`, etc.).
- **Safe Error Classification**: Use `question.ClassifyError(err)` for telemetry metrics and log categorization. Never leak upstream error messages, raw API payloads, prompts, or credentials into client HTTP responses or logs.
- **Never Cache Errors**: Failed requests, truncated responses, timeout errors, or invalid answers must never be persisted as valid answers.

### Concurrency & Lifecycle
- Every blocking or network call must accept and respect `context.Context`.
- Protect shared resources with appropriate synchronization: `sync.RWMutex` for desktop controller status and telemetry snapshots; single-writer channel/transaction patterns for SQLite.
- Desktop controller uses an atomic state machine (`stopped` -> `starting` -> `running` -> `stopping`) to ensure race-free start/stop cycles.

### Secret & Configuration Separation
- **Strict Isolation**: Application settings live in `config.json`. Secrets live in `.env` (`AI_API_KEY`, optional `OCS_ACCESS_TOKEN`).
- **Never put API keys in JSON**: The configuration parser rejects unknown fields and rejects API key properties in JSON.
- **Non-Loopback Security Guard**: If `server.host` is configured to listen beyond loopback (`127.0.0.1` / `localhost`), `OCS_ACCESS_TOKEN` is mandatory; otherwise startup fails.

---

## Important Files

- `cmd/server/main.go`: Daemon entry point assembling configuration, storage, telemetry, provider, and HTTP router.
- `cmd/desktop/main.go`: MyGo GUI entry point registering IPC methods and hosting the WebView.
- `internal/config/config.go`: Configuration and secret loader with URL normalization and boundary validation.
- `internal/desktop/controller.go`: Lifecycle coordinator for embedded server runtime and IPC state.
- `internal/desktop/settings.go`: Two-phase atomic persistence for desktop `config.json` and `.env`.
- `internal/httpapi/router.go`: Chi router wiring routes, middleware, and OCS query endpoints.
- `internal/question/service.go`: Core domain orchestration for cache, singleflight, and AI generation.
- `internal/question/validate.go`: Deterministic answer validator across single, multiple, judgement, and completion questions.
- `internal/provider/client.go`: Resilient HTTP client for AI APIs with backoff and usage extraction.
- `internal/store/sqlite.go`: SQLite initialization with WAL mode, busy timeout, and schema migrations.
- `migrations/migrations.go`: Embedded SQL migration runner.
- `mygo.json`: Desktop packaging configuration and platform bundle definitions.
- `Dockerfile`: Multi-stage, CGO-free build targeting a minimal `scratch` image.

---

## Runtime & Tooling Preferences

- **Go Toolchain**: Go 1.27.1 is the primary runtime. All production Go code compiles with `CGO_ENABLED=0`.
- **Zero-CGO SQLite**: Database storage uses `modernc.org/sqlite`, eliminating CGO toolchain dependencies while retaining full WAL-mode SQLite functionality.
- **Desktop Packaging**: Uses `egoist/mygo v0.5.5`, invoked via `go tool mygo`. It links against OS native webviews without requiring Node, Bun, npm, or CGO.
- **JavaScript & Node.js**: Node.js 22 is used exclusively for testing OCS client integration scripts (`tests/ocs.test.mjs`) and release tooling (`.github/scripts/*.mjs`). Tests run via native `node --test` without any npm dependencies, package managers (`npm`/`pnpm`/`yarn`), or `node_modules`.

---

## Testing & QA

### Testing Strategy & Frameworks
- **Pure Standard Library**: Go tests use Go's standard `testing` package with `t.Parallel()`, `t.Helper()`, and `t.Cleanup()`. No external assertion libraries.
- **Mocking Upstream AI**: Use `httptest.NewServer` to provide deterministic, isolated mock AI endpoints. Tests must never require or make calls to live external APIs.
- **Isolated SQLite Storage**: Database tests write to `filepath.Join(t.TempDir(), "test.db")` to ensure test independence.
- **Race Detection**: Every PR must pass `go test -race ./...`. Concurrency limits, singleflight deduplication, metrics collection, and desktop state transitions are rigorously exercised under concurrent goroutines.
- **Process-Level Integration Tests**: `tests/server_test.go` compiles and spawns the real server binary in a temporary sandbox, asserting signal handling, configuration reloading, and cache persistence across restarts.

---

## Secret File Access Prohibition

Agents must not read `.env` or other real dotenv secret files, directly or indirectly, including through tools, scripts, searches, subprocesses, or delegated agents. Do not print, copy, upload, or request their contents. Do not run the application, `config.Load`, or diagnostic commands against real configuration if that would load a secret file. Runtime application loading of `.env` remains intentional; this restriction applies to agent access during development and troubleshooting.

Use source code, explicitly whitelisted non-secret configuration fields, sanitized diagnostics, and isolated tests with agent-created dummy credentials. Tests may use temporary synthetic dotenv fixtures, but must never access user secret files. Only the empty `.env.example` template may be inspected or committed.
