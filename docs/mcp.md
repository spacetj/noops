# MCP server usage

`noops` exposes its task/goal helpers through the [Model Context Protocol](https://modelcontextprotocol.io) so that any MCP-aware client can talk to your Notion workspace. Two transports are available:

- `noops mcp` – STDIO JSON-RPC server (ideal for local tools such as Codex CLI or Claude Desktop).
- `noops serve` – HTTP wrapper around the same server (friendly for Cloud Run or other remote deployments).

Both variants share the global CLI flags, automatically load `.env` (unless `--dotenv=` is passed), and require `NOTION_TOKEN` plus `TASKS_DB_ID`.

## Run the STDIO server locally

```bash
make build
# export credential env vars or populate .env first
make mcp          # wraps ./bin/noops mcp and streams MCP traffic over stdio
```

When the command is running, point your preferred MCP client at the binary. A Codex CLI config snippet looks like:

```json
{
  "servers": {
    "noops-local": {
      "type": "mcp",
      "transport": {
        "type": "stdio",
        "command": "./bin/noops",
        "args": ["mcp"],
        "env": {
          "NOTION_TOKEN": "ntn_…",
          "TASKS_DB_ID": "<database-id>",
          "GOAL_PAGE_ID": "<optional-goal-id>"
        }
      }
    }
  }
}
```

Restart the client after updating secrets. `noops mcp` advertises the `task.create`, `task.update`, `task.delete`, `task.get`, `goal.list`, `goal.info`, `goal.create`, `goal.update`, and `goal.delete` tools. Each call returns an `application/json` content block so automation can parse structured data without scraping stdout.

### Codex CLI quick setup

Skip the manual JSON editing with:

```bash
noops mcp init codex --token $NOTION_TOKEN --db $TASKS_DB_ID
```

The helper writes `~/.config/codex/config.json` (override via `--config`) and adds/updates a `stdio` server entry named `noops-local` (override via `--name`). Pass `--force` to overwrite an existing entry. The generated transport inherits the current CLI binary path and embeds the Notion token/database IDs in the Codex config so MCP sessions start without extra prompts.

> **Security tip:** the Codex config file stores the Notion token in plain text. Ensure the file permissions stay restricted (written as `0600`).

## Remote / HTTP transport

`noops serve` accepts JSON-RPC 2.0 requests over HTTP POST at `/mcp`. Use this mode for Cloud Run or other containerized deployments:

```bash
make build
PORT=8080 ./bin/noops serve --listen :8080

# Health + tools/list smoke test
curl -s "http://localhost:8080/healthz"
curl -s -H "Content-Type: application/json" -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' "http://localhost:8080/mcp" | jq
```

For a hardened deployment with Google IAP and Secret Manager, follow `docs/cloudrun.md`. The HTTP server is protocol-compatible with the STDIO variant, so MCP clients can switch between them by changing only the transport block.

## Testing & troubleshooting

- `go test ./cmd -run TestEndToEndWorkflow` exercises the MCP server end-to-end against the Notion mock server.
- `--debug` surfaces request/response logs on stderr (useful when wiring new clients).
- Ensure the CLI can talk to Notion via `noops doctor --json` before relying on the MCP server.

Once the server is reachable, use it as the primary integration point for LLM agents—every tool exposed over MCP mirrors an existing CLI command and produces the same JSON results.
