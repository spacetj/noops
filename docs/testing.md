# Testing guide

noops ships with unit tests, integration tests that exercise the `ops.Runner`
against a mocked Notion API, and end-to-end tests that can be run manually
against a real workspace.

## Unit tests

```bash
GOCACHE=$(pwd)/.gocache go test ./...
```

The unit tests rely only on the Go standard library and the in-repo packages.
They use the mock Notion server described below to avoid network access.

## Integration tests with the mock Notion API

The package `internal/notion/mock` provides a lightweight HTTP server that
simulates the Notion endpoints touched by the CLI (databases, pages, queries,
updates). Tests spin up an `httptest.Server`, point a `notion.Client` at it, and
assert on the behaviour of higher-level workflows without calling the public
Notion API.

Example (see `internal/ops/runner_integration_test.go`):

```go
srv := mock.NewServer()
defer srv.Close()
client := notion.NewClient("ntn_mock", "2022-06-28", false)
client.SetBaseURL(srv.URL())
client.SetTransport(srv)
```

Refer to the test file for the full setup; the mock server exposes helpers to
seed databases, pages, and relation data. The transport processes requests
in-memory, so tests do not need to bind to local ports.

## End-to-end tests

`cmd/e2e_test.go` exercises the full CLI workflow against the in-memory Notion
mock:

- `noops init` provisions the workspace page, goals database, default
  goal page, and tasks database. The test relies on the IDs returned by the
  command instead of hard-coded schema assumptions.
- Core CLI commands (`task add/list/update/delete`, `tasks ensure`, `run`,
  `test`, `test-all`, `export`) run against that schema to validate the main
  user journeys.
- The Model Context Protocol server is started in-process and driven via
  JSON-RPC requests to create, update, read, and archive tasks.

The mock server intercepts HTTP requests through `notion.SetDefaultTransport`,
so the test suite never talks to the public Notion API and does not require
network access.

## Linting and vetting

Before sending a pull request, run:

```bash
GOCACHE=$(pwd)/.gocache go test ./...
GOFLAGS='-trimpath' go vet ./...
```

GitHub Actions re-run these steps on every pull request.
