# Architecture overview

```
.
├── cmd/                 # Cobra commands and CLI entry points
│   ├── root.go          # Global flags, dotenv loading, command wiring
│   ├── init.go          # Bootstrap command to create page/databases schema
│   ├── task*.go         # Friendly task CRUD helpers
│   ├── list.go          # Advanced DSL-based querying
│   ├── export.go        # Backup/export utilities
│   ├── tasks_ensure.go  # Batch ensure/update/dedupe workflow
│   ├── doctor.go        # Environment and schema validation
│   ├── run.go           # Convenience workflow (cleanup + smoke tests)
│   ├── test*.go         # Smoke/E2E validation commands
│   ├── mcp.go           # Model Context Protocol server over stdio
│   └── serve.go         # HTTP wrapper for the MCP server
├── internal/
│   ├── notion/          # Minimal Notion REST client (retriable)
│   └── ops/             # Business logic & helpers consumed by commands
├── docs/                # Operational and contributor documentation
├── cmd/assets/          # Embedded prompt surfaced via `noops prompt`
├── Makefile             # Build/test helpers
├── Dockerfile           # Distroless container for `noops serve`
└── .env.example         # Sample environment configuration
```

## Key design points

- **Command separation** – Each Cobra command lives in its own file. Commands
  wire flags and delegate to small helpers that live in `internal/ops` for
  easier testing.
- **Bootstrapping** – `cmd/init.go` provisions a parent page, goals
  database, default goal page, and tasks database with the expected schema.
  Integration tests use it to set up fresh workspaces on demand.
- **Internal packages** – Everything that should not be consumed by external
  modules lives under `internal/`. The `notion` package wraps HTTP requests and
  retry logic, while the `ops` package contains higher level orchestration.
- **Dependency injection** – Commands construct a `notion.Client` and pass it to
  an `ops.Runner`. The runner exposes focused methods (`CleanupUntitled`,
  `Tests`, `TestAll`, `BuildPropsFromSpecs`, etc.) that can be exercised with a
  mock client in tests.
- **Environment handling** – Global flags are set via Cobra persistent flags.
  `.env` files are optional and only populate missing variables. Commands that
  do not hit the Notion API (`prompt`, `version`, `completion`, `help`) skip
  validation.
- **JSON-first output** – All commands can emit machine-readable JSON. Human
  friendly formatting happens only when `--json` is omitted.

## Extending the CLI

1. Add a new Cobra command under `cmd/`. Keep the method small—parse flags,
   assemble inputs, delegate to a function in `internal/ops`.
2. If you need to talk to Notion, extend `internal/notion.Client` with a focused
   helper (e.g. `Search`, `RetrieveBlock`). Prefer adding methods instead of
   leaking raw HTTP details into commands.
3. Update docs (`README.md`, `cmd/assets/PROMPT.md`) and add tests that exercise
   the new behavior against the mock server.
4. Run `gofmt`, `go test ./...`, and the GitHub Action workflows locally when
   possible (`.github/workflows/`).

## Persistence & local state

The CLI intentionally avoids global state beyond optional `.noops_state.json`,
which caches the last goal relation used by friendly commands. The file lives
in the project root and is excluded via `.gitignore`.

## MCP server

`noops mcp` and `noops serve` expose the same tools as the CLI. The MCP server
shares the same flag/env handling as the CLI, so environment variables (or
`--dotenv`) are still required. See `docs/cloudrun.md` for deployment guidance.
