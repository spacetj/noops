# noops — Notion Ops CLI

**noops** is a Go CLI that streamlines operations on a Notion Tasks database.
It normalises IDs, loads environment variables, performs common hygiene tasks,
exports backups, and exposes a friendly command surface for both humans and
automation (including Model Context Protocol clients).

## Highlights

- 🔐 **Integration ready** – works with Notion internal integrations (`ntn_…`).
- 🔎 **Smart IDs** – accepts full URLs or 32/36 character IDs and normalises
  them automatically.
- 🧠 **Schema introspection** – auto-detects title, status, select, date, and
  self-relation properties.
- 🚀 **One-command setup** – `noops init` provisions a ready-to-use workspace
  page, goals database, and tasks database with the expected schema.
- 🧹 **Cleanup utilities** – archive untitled pages safely with pagination.
- 🧪 **Built-in smoke tests** – run quick health checks or full end-to-end tests.
- ⚙️ **Automation-first** – JSON output everywhere, friendly subcommands, and an
  MCP server mode for ChatGPT Desktop.

## Requirements

- Go 1.25.9+ (or the latest patched Go release supported by this project)
- A Notion internal integration token (`ntn_…`) shared with your Tasks database
  (and optional default goal page)

## Installation

```bash
# clone the repository
make build             # builds ./bin/noops
make install           # optional: installs to $GOBIN as `notioncli`
```

To upgrade, pull the latest changes and rerun `make build` (or `make install`).

## Configuration

Copy `.env.example` to `.env` and fill in your workspace-specific values:

```bash
cp .env.example .env
```

If you're starting from an empty workspace, run `./bin/noops init`. It creates a
workspace page, a goals database (with a default goal page), and a tasks
database that matches the expected schema. By default it writes the resulting
IDs (`TASKS_DB_ID`, `GOAL_PAGE_ID`) into your `.env` file so subsequent
commands "just work". Use `--write-env=false` to opt out.

Required environment variables (or matching CLI flags):

| Variable         | Flag        | Description                                   |
|------------------|-------------|-----------------------------------------------|
| `NOTION_TOKEN`   | `--token`   | Notion internal integration token             |
| `TASKS_DB_ID`    | `--db`      | Tasks database ID or full Notion URL          |
| `GOAL_PAGE_ID`   | `--goal`    | (Optional) default Goals relation page        |
| `NOTION_VERSION` | `--notion-version` | (Optional) Notion API version header |

The CLI loads `.env` automatically; disable with `--dotenv=`.

## Quick start

```bash
make build
./bin/noops init                          # create page + databases, write IDs to .env
./bin/noops doctor --dotenv .env --json   # verify connectivity and schema
./bin/noops cleanup-untitled              # archive untitled rows
./bin/noops task add --title "Plan sprint" --status "In Progress" --json
./bin/noops tasks ensure --goal "North Star" --from-json tasks.json --json
```

Common commands:

| Command | Purpose |
|---------|---------|
| `noops init` | Provision the workspace page, goals DB, and tasks DB |
| `noops run` | Cleanup untitled pages and run smoke checks |
| `noops doctor` | Validate credentials, database reachability, and schema |
| `noops task add/list/update/delete/get` | Friendly task management helpers |
| `noops goal list/info/create/update/delete` | Manage goals (Notion pages) |
| `noops list` | Flexible DSL-based querying with JSON output |
| `noops export` | Snapshot tasks to JSON (raw or compact) |
| `noops tasks ensure` | Idempotently create/update tasks from JSON |
| `noops migrate-notes` | Move rich text properties into page content |
| `noops test` / `noops test-all` | Smoke or end-to-end validation suites |
| `noops mcp` / `noops serve` | Run the CLI as an MCP server over stdio or HTTP |

## MCP server

Prefer the MCP server whenever an automation or LLM client needs to talk to Notion. There are two flavors:

- `noops mcp` keeps the JSON-RPC 2.0 server on STDIO for local MCP clients. Build with `make build` and run with `make mcp` or `./bin/noops mcp`.
- `noops serve` exposes the same tools over HTTP (`/mcp`) and adds a health probe. Use this for Cloud Run or container deployments.

Quick Codex CLI setup:

```bash
noops mcp init codex --token $NOTION_TOKEN --db $TASKS_DB_ID
```

The helper writes `~/.config/codex/config.json` (overridable) and adds a `stdio` MCP server entry pointing at the current `noops` binary. Use `--name` to pick a custom server name or `--force` to overwrite an existing entry.

See [`docs/mcp.md`](docs/mcp.md) for client configuration snippets, curl smoke tests, and troubleshooting tips.

All MCP tool responses are emitted as `application/json` content blocks so clients can parse structured data without extra scraping.

Add `--json` for machine-readable output and `--debug` for verbose logging.

## Testing & QA

```bash
# Unit tests (use a workspace-local build cache to avoid sandbox issues)
GOCACHE=$(pwd)/.gocache go test ./...
```

A lightweight Notion mock server and additional scenarios are documented in
[`docs/testing.md`](docs/testing.md). Use the mock server for CI and local tests
so that no external network access or production credentials are required.

## Documentation

- [`docs/architecture.md`](docs/architecture.md) – project layout & design
- [`docs/testing.md`](docs/testing.md) – unit/integration testing & mock server
- [`docs/cloudrun.md`](docs/cloudrun.md) – deploy the MCP server to Cloud Run

## Contributing

We welcome issues, documentation improvements, and pull requests. See
[`CONTRIBUTING.md`](CONTRIBUTING.md) for setup instructions and
[`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md) for community expectations.

## Security

Please report vulnerabilities responsibly by following
[`SECURITY.md`](SECURITY.md).

## License

Released under the [MIT License](LICENSE).
