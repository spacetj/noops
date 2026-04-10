# Contributing to noops

Thanks for your interest in improving **noops**! This project welcomes bug
reports, feature ideas, documentation updates, and code contributions.

## Getting started

1. Fork the repository and clone your fork.
2. Install Go 1.22 or newer and ensure `go` is on your PATH.
3. Copy `.env.example` to `.env` and populate it with credentials for a scratch
   Notion workspace (never use production secrets for development).
4. Install tools:
   ```bash
   make build
   ```
5. Run the test suite:
   ```bash
   GOCACHE=$(pwd)/.gocache go test ./...
   ```

If you are working on integration features, use the in-repo Notion mock server
(described in [`docs/testing.md`](docs/testing.md)) so that CI and local tests do
not require external network access.

## Development workflow

- Create a feature branch for your work.
- Keep changes focused. Multiple unrelated fixes should be submitted as separate
  pull requests.
- Run `go fmt ./...`, `go vet ./...`, and the unit/integration tests before
  pushing.
- Update or add documentation when behavior changes.
- Include tests that cover the new behavior whenever possible.

## Commit and PR guidelines

- Start commit messages with a verb in the imperative mood (e.g., `Add`,
  `Fix`, `Refactor`).
- Reference issues in the commit body or pull request description when
  applicable.
- Provide context in the PR description: the problem, the solution, and any
  remaining follow up work.
- Expect automated checks to run on each PR (lint, build, and tests).

## Code of conduct

All contributors are expected to uphold our
[Code of Conduct](CODE_OF_CONDUCT.md). Instances of abusive or unacceptable
behavior may be reported to `conduct@noops.dev`.

Thank you for helping make **noops** better!
