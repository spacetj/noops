package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spacetj/noops/internal/notion"
	"github.com/spacetj/noops/internal/notion/mock"
)

func TestWriteEnvValuesCreatesFile(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "config", ".env.local")

	abs, err := writeEnvValues(envPath, map[string]string{
		"TASKS_DB_ID":  "db-123",
		"GOAL_PAGE_ID": "proj-456",
	})
	if err != nil {
		t.Fatalf("writeEnvValues: %v", err)
	}

	if got, want := abs, absPathOrFail(t, envPath); got != want {
		t.Fatalf("abs path = %q, want %q", got, want)
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), lines)
	}
	if lines[0] != "GOAL_PAGE_ID=\"proj-456\"" || lines[1] != "TASKS_DB_ID=\"db-123\"" {
		t.Fatalf("unexpected file contents: %q", lines)
	}

	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatalf("stat env: %v", err)
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("file mode = %o, want 0600", perm)
		}
	}
}

func TestWriteEnvValuesMergesExisting(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	initial := "# keep this\nTASKS_DB_ID=\"old\"\nOTHER=\"keep\"\n"
	if err := os.WriteFile(envPath, []byte(initial), 0o600); err != nil {
		t.Fatalf("seed env: %v", err)
	}

	if _, err := writeEnvValues(envPath, map[string]string{
		"TASKS_DB_ID":  "new",
		"GOAL_PAGE_ID": "proj",
	}); err != nil {
		t.Fatalf("writeEnvValues: %v", err)
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	expected := "# keep this\nTASKS_DB_ID=\"new\"\nOTHER=\"keep\"\nGOAL_PAGE_ID=\"proj\"\n"
	if string(data) != expected {
		t.Fatalf("env mismatch\n got: %q\nwant: %q", string(data), expected)
	}
}

func TestInitWritesEnvFile(t *testing.T) {
	t.Setenv("NOTION_TOKEN", "ntn_mock")
	t.Setenv("TASKS_DB_ID", "")
	t.Setenv("GOAL_PAGE_ID", "")

	srv := mock.NewServer()
	notion.SetDefaultTransport(srv)
	t.Cleanup(func() {
		notion.SetDefaultTransport(nil)
		srv.Close()
	})

	envPath := filepath.Join(t.TempDir(), ".env")
	stdout, stderr, err := runCLI(t, "--dotenv", envPath, "--notion-base-url", srv.URL(), "init", "--json")
	if err != nil {
		t.Fatalf("init command failed: %v", err)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("parse init json: %v", err)
	}
	tasksID := asString(resp["tasks_db_id"])
	goalID := asString(resp["default_goal_id"])
	if tasksID == "" || goalID == "" {
		t.Fatalf("init json missing ids: %v", resp)
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	contents := string(data)
	if !strings.Contains(contents, fmt.Sprintf("TASKS_DB_ID=\"%s\"", tasksID)) {
		t.Fatalf("env missing tasks id: %q", contents)
	}
	if !strings.Contains(contents, fmt.Sprintf("GOAL_PAGE_ID=\"%s\"", goalID)) {
		t.Fatalf("env missing goal id: %q", contents)
	}
}

func TestInitWithWriteEnvDisabled(t *testing.T) {
	t.Setenv("NOTION_TOKEN", "ntn_mock")
	t.Setenv("TASKS_DB_ID", "")
	t.Setenv("GOAL_PAGE_ID", "")

	srv := mock.NewServer()
	notion.SetDefaultTransport(srv)
	t.Cleanup(func() {
		notion.SetDefaultTransport(nil)
		srv.Close()
	})

	envPath := filepath.Join(t.TempDir(), ".env")
	stdout, _, err := runCLI(t, "--dotenv", envPath, "--notion-base-url", srv.URL(), "init", "--write-env=false", "--json")
	if err != nil {
		t.Fatalf("init command failed: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("parse init json: %v", err)
	}

	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Fatalf("expected env file to be skipped, stat err=%v", err)
	}

	if got := os.Getenv("TASKS_DB_ID"); got == "" {
		t.Fatalf("expected TASKS_DB_ID to be set in process")
	}
	if got := os.Getenv("GOAL_PAGE_ID"); got == "" {
		t.Fatalf("expected GOAL_PAGE_ID to be set in process")
	}
}

func TestInitRequiresToken(t *testing.T) {
	t.Setenv("NOTION_TOKEN", "")

	_, _, err := runCLI(t, "init")
	if err == nil {
		t.Fatalf("expected error when token missing")
	}
	if !strings.Contains(err.Error(), "NOTION_TOKEN is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func absPathOrFail(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	return abs
}
