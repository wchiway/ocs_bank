# Repository Guidelines

## Project Structure & Module Organization

This Python service provides an OCS-compatible question-bank API backed by SQLite and SiliconFlow.

- `main.py`: Flask application factory, database initialization, and `GET /api/query`; checks cached answers before querying AI and storing results.
- `utils/aiAnwser.py`: model requests, prompts, and retries. Preserve this existing filename when importing it.
- `config.json`: server host, port, and provider API key.
- `question_bank.db`: local answer database.
- `test_app.py` and `test_api.py`: manual HTTP checks at the repository root.
- `requirements.txt` and `README.md`: dependencies and setup instructions. There are no separate frontend assets or build directories.

## Build, Test, and Development Commands

Run commands from the repository root; configuration and database paths are relative to the working directory. Use an isolated Python environment.

- `python -m pip install -r requirements.txt`: install pinned dependencies.
- `python main.py`: initialize SQLite tables and start the Flask development server using `config.json`.
- `python test_app.py`: run three sample requests, including an empty title, against port 5000; start the server first.
- `python test_api.py`: open an interactive query tester using the configured port.

There is no separate build step.

## Coding Style & Naming Conventions

Use four-space indentation, `snake_case` functions and variables, and uppercase configuration constants such as `API_CONFIG`. Follow existing UTF-8 text and Chinese comments where appropriate. Keep SQL parameterized with `?` placeholders and close database connections reliably. No formatter or linter is configured; keep changes focused and avoid unrelated reformatting.

## Testing Guidelines

Current tests print responses rather than assert outcomes; no testing framework or coverage threshold is configured. Inspect HTTP status and JSON manually. Check missing-title rejection, cached answers, AI fallback, and provider failures when relevant. Name additional checks `test_*.py`. Live queries may call SiliconFlow and modify SQLite; use disposable data and mock provider calls for automated tests.

## Commit & Pull Request Guidelines

Recent commits mostly use `m`, so no meaningful commit convention is established. Use concise, imperative messages describing the change. PRs should explain behavior changes, link relevant issues, and list validation commands and results. Include request/response examples for API changes.

## Security & Configuration

Never commit real API keys or incidental database changes. Provider debug output currently includes authorization headers; redact logs before sharing. Keep the development server, which enables Flask debug mode, local.
