SHELL := /bin/bash

BIN := bin/noops
PKG := github.com/spacetj/noops

# Derive version for ldflags: prefer git describe, fallback to dev
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X '$(PKG)/cmd.cliVersion=$(VERSION)'

.PHONY: build install run test-cli cleanup tidy clean mcp serve-http

build:
	@echo "==> Building $(BIN) (version: $(VERSION))"
	@mkdir -p bin
	@go build -ldflags "$(LDFLAGS)" -o $(BIN) .

install:
	@echo "==> Installing via go install (GOBIN or GOPATH/bin), version: $(VERSION)"
	@go install -ldflags "$(LDFLAGS)" .

run: build
	@set -a; [ -f .env ] && source .env; set +a; \
	$(BIN) run --debug $${NOTION_TOKEN:+--token $$NOTION_TOKEN} $${TASKS_DB_ID:+--db $$TASKS_DB_ID} $${GOAL_PAGE_ID:+--goal $$GOAL_PAGE_ID}

mcp: build
	@set -a; [ -f .env ] && source .env; set +a; \
	$(BIN) mcp $${NOTION_TOKEN:+--token $$NOTION_TOKEN} $${TASKS_DB_ID:+--db $$TASKS_DB_ID} $${GOAL_PAGE_ID:+--goal $$GOAL_PAGE_ID}

serve-http: build
	@set -a; [ -f .env ] && source .env; set +a; \
	$(BIN) serve $${NOTION_TOKEN:+--token $$NOTION_TOKEN} $${TASKS_DB_ID:+--db $$TASKS_DB_ID} $${GOAL_PAGE_ID:+--goal $$GOAL_PAGE_ID}

test-cli: build
	@set -a; [ -f .env ] && source .env; set +a; \
	$(BIN) test --debug $${NOTION_TOKEN:+--token $$NOTION_TOKEN} $${TASKS_DB_ID:+--db $$TASKS_DB_ID} $${GOAL_PAGE_ID:+--goal $$GOAL_PAGE_ID}

cleanup: build
	@set -a; [ -f .env ] && source .env; set +a; \
	$(BIN) cleanup-untitled --debug $${NOTION_TOKEN:+--token $$NOTION_TOKEN} $${TASKS_DB_ID:+--db $$TASKS_DB_ID}

tidy:
	go mod tidy

clean:
	rm -rf bin
