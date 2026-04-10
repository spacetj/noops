Purpose
This document is a ready-to-use prompt for ChatGPT (and similar LLM agents) to reliably operate the Notion CLI in this repository to manage tasks in a Notion database.

Key Principles
- Always produce commands that are safe, idempotent when possible, and parseable by a shell.
- Prefer JSON output with the `--json` flag when available and act on the structured result.
- Prefer operating by stable IDs. If only titles are known, resolve IDs first (exact match), then apply updates by ID.
- Accept full Notion URLs for databases/pages; the CLI extracts IDs automatically.
- Handle Notion’s status vs select types: use `status` when present; fallback to `select` when `status` is not available.
- Treat delete as archive (soft-delete). The CLI archives pages; Notion preserves them.
- On errors (non-zero exit), read the response body and adjust command parameters (property names/types/options/IDs) before retrying.

Guardrails
- If the workspace is uninitialised, run `noops init` first. It provisions the
  parent page, goals database, default goal page, and tasks database, then
  writes the resulting IDs to `.env` (unless `--write-env=false` is passed).
- Run `noops doctor --json` first and require `ok: true` before making changes.
- Take a backup before bulk edits: `noops export --goal "<Title|URL|ID>" --compact --out backups/$(date +%Y%m%d_%H%M%S)_compact.json`.
- Use `--dotenv` (defaults to `.env`) rather than sourcing `.env` in scripts; disable via `--dotenv=` if needed.
- Prefer `task list --json` or `list --compact --json` for normalized outputs; avoid brittle jq paths over raw Notion JSON.
- For `tasks ensure`, set `--update-existing` only when you intend to overwrite populated fields; otherwise rely on conditional updates.
- Pass full URLs for relations; the CLI normalizes them (prevents Notion validation errors).
- Use `test --soft` for optional preflight checks that should not fail scripts.
- Always include `--json` when chaining commands so stdout stays machine-readable (logs go to stderr).

Environment & Configuration
- Required: `NOTION_TOKEN` (internal integration token, starts with `ntn_...`).
- Optional: `TASKS_DB_ID` (database ID or full DB URL). The CLI requires this for most commands. Created automatically by `noops init`.
- Optional: `GOAL_PAGE_ID` (goal page ID or full page URL) when relating tasks to a goal. Seeded by `noops init`.
- Optional: `NOTION_VERSION` (default `2022-06-28`).
- Debugging: add `--debug` to any command for per-request logs.

Using .env locally
- Store secrets in a local `.env` (not committed).
- The CLI supports `--dotenv` (defaults to `.env`), so most scripts do not need to `source` the file. To disable, pass `--dotenv=`.

Installation & Path
- Build: `make build` (outputs `./bin/noops` for local use)
- Install: `make install` (uses `go install` to install into `$GOBIN` or `$GOPATH/bin` as `notioncli`). Ensure PATH includes it:
  - `export PATH=${GOBIN:-$(go env GOPATH)/bin}:$PATH`
  - Optionally alias: `alias noops=notioncli`

CLI Binaries and Commands
- Binary path: prefer `./bin/noops` in scripts (built from this repo); otherwise use `noops` if installed in PATH.
- Global flags: `--token`, `--db`, `--goal`, `--json`, `--debug`, `--notion-version`.
- No-log JSON: when `--json` is used, commands print pure JSON to stdout (logs go to stderr), so it’s safe to pipe to `jq`.
- `.env` autoload: `--dotenv` loads environment variables from `.env` by default.

High-Level (Friendly) Commands
- `noops init [--write-env=false] [--page-name <Title>] [--tasks-name <Title>]` – bootstrap the workspace page, goals DB, default goal, and tasks DB.
- `noops task add --goal "<GoalName|ID>" --title "<Title>" [--do YYYY-MM-DD] [--due YYYY-MM-DD] [--status <Name>] [--priority <Name>] [--parent "<ParentTitle|ID>"] [--emoji "<Emoji>"] [--auto-emoji] [--desc "<Text>"] [--json]`
- `noops task list [--goal "<GoalName|ID-or-URL>"] [--status <Name>] [--parent "<ParentTitle|ID>"] [--json]`
- `noops goal list [--json]`
- `noops goal info --name "<GoalTitle>" | --id "<PageURL-or-ID>" [--json]`
- `noops goal create --title "<Title>" [--status Active|Completed] [--emoji "🎯"] [--json]`
- `noops goal update --id "<PageURL-or-ID>" [--title "<New Title>"] [--status <Name>] [--emoji "✨"] [--json]`
- `noops goal delete --id "<PageURL-or-ID>" [--json]`
- `noops tasks ensure --goal "<GoalURL-or-ID-or-ExactName>" --from-json [<file>|STDIN] [--update-existing] [--dedupe keep-first|keep-last|none] [--json] [--debug]`
- `noops migrate-notes --goal "<GoalURL|ID|Title>" [--from-prop Summary|Description] [--clear] [--dry-run] [--json]`

Notes
- Friendly commands default to table output with columns: `Task ID | Title | Status | Due | Parent`. Use `--json` for raw JSON.
- Friendly `task list --json` returns a compact array of rows with normalized keys: `[ {"id","title","status","due","parent"}, ... ]`. Prefer this over the low-level `list` when scripting.
- `--auto-emoji` sets the page icon using a guessed emoji from the title if `--emoji` is not provided. Consider adding complementary emojis in titles and descriptions for visual appeal.
- Smart default: if `--goal` is omitted for `task add`, the CLI uses the most recently used goal (tracked locally).
- `tasks ensure` ingests a JSON array and idempotently creates/updates tasks under a goal; it can dedupe and soft-archive duplicates.
- JSON fields supported per item: `title` (string), `do` (YYYY-MM-DD), `due` (YYYY-MM-DD), `priority` (select), `status` (status), `emoji` (string), `parent` (exact title or page URL/ID), `goal` (page URL/ID or exact goal title override), `props` (extra property map).

High-Value Commands
1) Create a task
- Command:
  - `noops task create --title "<Title>" [--emoji "<Emoji>"] [--set "<Spec>"]... [--json] [--goal <URL-or-ID>]`
- Property spec grammar for `--set`:
  - `Name=Value` (infers type from DB)
  - `Name:date=YYYY-MM-DD`
  - `Name:select=OptionName`
  - `Name:status=StatusName`
  - `Name:relation=<PageURL-or-ID>`
  - `Name:clear` (clears date/status/select or empties relation)
- Output on success with `--json`:
 - `{ "ok": true, "id": "<page-id>" }`

2) Update a task
- Command:
  - By ID: `noops task update --id "<PageURL-or-ID>" [--rename "<NewTitle>"] [--emoji "<Emoji>"] [--desc "<Notes>"] [--set "<Spec>"]... [--json]`
  - By title (exact match): `noops task update --title "<Title>" [--emoji "<Emoji>"] [--desc "<Notes>"] ...`
- Notes:
  - Prefer `--id` for reliability. When using `--title`, ensure it is unique.
  - Use `Name:clear` to unset date/status/select/relation.
  - `--desc` appends notes to the page content (block children), not to a property.
- Output on success with `--json`:
  - `{ "ok": true }`

3) Get a task (raw Notion JSON)
- Command:
  - `noops task get --id "<PageURL-or-ID>" [--json]`
  - Or resolve by title: `noops task get --title "<Title>" [--json]`
- Output: full Notion page JSON (useful for introspection or extracting properties/IDs).

4) Delete (archive) a task
- Command:
  - `noops task delete --id "<PageURL-or-ID>" [--json]`
  - Or by title: `noops task delete --title "<Title>" [--json]`
- Behavior: archives the page (soft delete).
- Output on success with `--json`:
  - `{ "ok": true }`

5) Cleanup untitled tasks
- Command: `noops cleanup-untitled [--json]`
- Behavior: finds tasks where the DB title property is empty and archives them (paginated).

6) Test suites
- Quick tests: `noops test --debug` (verifies key parents, status, goal relation).
- E2E tests: `noops test-all --debug --goal "<TestGoalURL-or-ID>"` (creates, updates, sets relations, archives; validates end-to-end).

DB & Property Handling
- The CLI fetches database metadata and auto-detects:
  - Title property key (e.g., `Name` or custom) — used for create/rename and exact-title queries.
  - Status property — used for `status` updates; falls back to `select` if `status` not present.
  - Self-relation (relation to the same DB) — used for parent/child linking when available.
- The CLI only writes to properties that exist with the correct type, skipping unknown or mismatched ones.
- Relations accept full Notion page URLs or page IDs; the CLI extracts the UUID.
 - Parent resolution in `tasks ensure`: if `parent` looks like a valid page ID/URL (UUID after normalization), it is used directly; otherwise the CLI resolves the parent by exact title within the same tasks database.
 - Page emoji icon is not a property; it is set via `--emoji` and appears as the page icon in Notion.
 - Notes/content: `task update --desc` and `tasks ensure` with `desc` append notes as page blocks (paragraphs). They no longer write to a `Summary`/`Description` property.

Error Patterns & Recovery
- 404 object_not_found: incorrect ID/URL or integration not shared to DB/page. Ask user to share the integration or confirm IDs.
- 400 validation_error: property name/type mismatch or invalid option. Use `task get` to inspect schema and adjust property names/options.
- 429 / 5xx: temporary conditions; the CLI retries with backoff automatically.
- Exit code: non-zero indicates failure. Parse the error text (contains Notion JSON) to decide next steps.

Examples (Replace placeholders)
- Create a task with dates, priority, status, and relate to a goal:
  - `noops task create --title "📦 Book movers" --emoji "🚚" --set "Do:date=2025-10-01" --set "Due:date=2025-10-15" --set "Priority:select=High" --set "Status=status=In Progress" --set "Goals:relation=https://www.notion.so/Test-Goal-..." --json`
- Friendly add (preferred for humans):
  - `noops task add --goal "My Goal" --title "Book movers" --due 2025-10-15 --priority High --status "In Progress" --auto-emoji --desc "Get quotes; compare reviews; confirm booking 🚚"`
- Update by ID: change status and due date, rename, and append notes:
  - `noops task update --id "<PageURL-or-ID>" --emoji "✅" --rename "Book movers (confirmed)" --set "Status=status=Done" --set "Due:date=2025-10-18" --desc "Notes about the booking..." --json`
- Clear a date and unset goal relation by title:
  - `noops task update --title "Book movers" --set "Due:clear" --set "Goals:clear" --json`
- Archive by title:
 - `noops task delete --title "Book movers" --json`
- Inspect raw JSON for a task (useful for debugging):
  - `noops task get --title "Book movers" --json`

9) Manage goals
- Create: `noops goal create --title "<Title>" [--status Active|Completed] [--emoji "🎯"] --json`
- Update: `noops goal update --id "<GoalID>" [--rename "<New Title>"] [--status <Name>] [--emoji "✨"] --json`
- Delete: `noops goal delete --id "<GoalID>" --json`
- All goal commands output JSON `{ "ok": true, ... }` on success so automations can confirm the IDs created/updated/archived.

Compact listing for scripts
- Prefer: `noops task list --goal "<GoalTitle|URL|ID>" --json` → outputs `[ {id,title,status,due,parent}, ... ]`
- If you need raw Notion JSON instead, use the low-level list: `noops list --where 'Goals:relation.contains=<URL-or-ID>' --json`.
- For jq-free rows, use: `noops list --where 'Goals:relation.contains=<URL-or-ID>' --compact --json`.

Backups as Standard Practice
- Before any bulk changes (ensure, dedupe, archival), export a snapshot:
  - Raw snapshot: `noops export --goal "<Title|URL|ID>" --out backups/$(date +%Y%m%d_%H%M%S)_raw.json`
  - Compact snapshot: `noops export --goal "<Title|URL|ID>" --compact --out backups/$(date +%Y%m%d_%H%M%S)_compact.json`
- Keep both if you plan to restore or audit changes. The compact file is ideal for quick diffs; the raw snapshot contains full Notion page JSON.

Migrations
- If you have legacy notes stored in rich_text properties (e.g., `Summary` or `Description`), migrate them to page content and optionally clear the field:
  - Dry-run: `noops migrate-notes --goal "<GoalTitle|URL|ID>" --from-prop Summary --dry-run --json`
  - Apply + clear: `noops migrate-notes --goal "<GoalTitle|URL|ID>" --from-prop Summary --clear --json`
  - After migrating, it’s safe to remove the legacy property from the DB schema.

Operational Guidance for the Agent
1) Ensure configuration is present: token (`NOTION_TOKEN`). DB (`TASKS_DB_ID`) is optional and falls back to the configured default unless overridden by flag/env.
2) Prefer `--json` for create/update/delete and parse structured results.
3) When updating by title, confirm uniqueness (or request clarification); otherwise, resolve ID first using `task get --title` and then operate by `--id`.
4) If validation errors occur, introspect with `task get` and adjust property name/type (status vs select) and available options.
5) For relations, pass full Notion URLs to simplify ID handling; the CLI extracts UUIDs.
6) For bulk cleanup of “untitled” rows, use `cleanup-untitled`.
7) When asked to verify an operation, use `task get --id ... --json` to confirm the expected property values.
8) Prefer `list --compact --json` when you need a quick, script-friendly listing without additional jq logic.
9) When calling MCP tools, expect `application/json` responses in `content[].data`; parse that object instead of scraping textual output.

Scripting Tips (make scripts work first time)
- Start scripts with `set -euo pipefail` for safer execution; when using Bash associative arrays with special characters under `nounset`, wrap their declaration/usage with `set +u` ... `set -u`.
- At the beginning, export `.env` if present: `set -a; [ -f .env ] && source .env; set +a`.
- Prefer local `./bin/noops` so your script uses the version built from this repo; fall back to `noops` in PATH if not found.
- Always add `--json` and parse output (e.g., with `jq`) when chaining commands.
- Pass Notion URLs directly for `--db`, `--goal`, relations, and IDs in JSON; the CLI normalizes them to UUIDs.

Security & Safety
- Never print the token in logs or responses. Prefer passing it via environment variables.
- All destructive actions are soft deletes (archive). The user can restore in Notion.
7) List / Search tasks
- Command:
  - `noops list --where '<Prop[:type].op=val>' [--where ...] [--sort '<Prop[:type]:asc|desc>'] [--page-size N] [--cursor C] [--json]`
- DSL examples:
  - Title contains: `Title:title.contains=setup`
  - Status equals: `Status:status.equals=In Progress`
  - Priority high and due before date: `Priority:select.equals=High`, `Due:date.on_or_before=2025-12-31`
  - Related to goal: `Goals:relation.contains=<GoalURL-or-ID>`
- Notes:
  - Use `Title` alias for the DB’s title property; the CLI maps it automatically.
  - Combine multiple `--where` with logical AND.
  - Use `--json` to get structured results.
  - Human-friendly filters (ANDed with `--where`):
    - `--name <substring>` or `--name-equals <text>`
    - `--status <name>`
    - `--priority <name>`
    - `--due-before YYYY-MM-DD`, `--due-after YYYY-MM-DD`
    - `--do-before YYYY-MM-DD`, `--do-after YYYY-MM-DD`
    - `--goal-filter <PageURL-or-ID>`

Additional Guidance
- Prefer `--json` output and parse it to branch logic reliably.
- For updates, prefer `--id` where possible; otherwise resolve with `task get --title` first.
- When filters fail with validation_error, adjust property names or types (status vs select) using `task get` to inspect the schema.
Low-Level List (compact)
- `noops list --where '<Prop[:type].op=val>' [--where ...] --compact --json`
- Outputs normalized rows `[ {id,title,status,do,due,parent}, ... ]` suitable for scripts without `jq` transforms.

Doctor
Low-Level List (compact)
- `noops list --where '<Prop[:type].op=val>' [--where ...] --compact --json`
- Outputs normalized rows `[ {id,title,status,do,due,parent}, ... ]` suitable for scripts without `jq` transforms.

Doctor
- `noops doctor [--json]` validates:
  - Token present, DB reachable, schema basics (title/status/relation), goal page reachable (if provided).
  - Returns `{ ok, checks: [ { name, status, detail } ... ] }` in JSON.

Quiet Tests
- `noops test --soft` downgrades missing-fixture errors to warnings (exit 0). Useful as an optional preflight in scripts.
- Visual appeal: choose a relevant `--emoji` for each task, consider including emojis in titles, and keep descriptions structured (e.g., checklists) when editing in Notion.
- Visual appeal: choose a relevant `--emoji` for each task, consider including emojis in titles, and keep descriptions structured (e.g., checklists) when editing in Notion.
