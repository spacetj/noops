package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacetj/noops/internal/notion"
	"github.com/spacetj/noops/internal/notion/mock"
)

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type mcpCallResult struct {
	Content []mcpContentOut `json:"content"`
}

type mcpContentOut struct {
	Type string      `json:"type"`
	Text string      `json:"text,omitempty"`
	Data interface{} `json:"data,omitempty"`
}

func TestEndToEndWorkflow(t *testing.T) {
	srv := mock.NewServer()
	notion.SetDefaultTransport(srv)
	t.Cleanup(func() {
		notion.SetDefaultTransport(nil)
	})

	os.Setenv("NOTION_BASE_URL", srv.URL())
	os.Setenv("NOTION_TOKEN", "ntn_mock")
	t.Cleanup(func() {
		os.Unsetenv("NOTION_BASE_URL")
		os.Unsetenv("TASKS_DB_ID")
		os.Unsetenv("GOAL_PAGE_ID")
	})

	stdout, _, err := runCLI(t, "init", "--write-env=false", "--json")
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	var initResp map[string]any
	if err := json.Unmarshal([]byte(stdout), &initResp); err != nil {
		t.Fatalf("parse init json: %v", err)
	}
	tasksID := asString(initResp["tasks_db_id"])
	goalID := asString(initResp["default_goal_id"])
	if tasksID == "" || goalID == "" {
		t.Fatalf("init result missing ids: %v", initResp)
	}

	os.Setenv("TASKS_DB_ID", tasksID)
	os.Setenv("GOAL_PAGE_ID", goalID)

	stdout, _, err = runCLI(t, "task", "add", "--title", "CLI Task", "--status", "In Progress", "--priority", "High", "--goal", goalID, "--json")
	if err != nil {
		t.Fatalf("task add failed: %v", err)
	}
	var addResp map[string]any
	if err := json.Unmarshal([]byte(stdout), &addResp); err != nil {
		t.Fatalf("parse add json: %v", err)
	}
	taskID := asString(addResp["id"])
	if taskID == "" {
		t.Fatalf("task add missing id: %v", addResp)
	}

	if _, _, err := runCLI(t, "task", "list", "--json"); err != nil {
		t.Fatalf("task list failed: %v", err)
	}

	if _, _, err := runCLI(t, "task", "update", "--id", taskID, "--set", "Status:status=Done", "--json"); err != nil {
		t.Fatalf("task update failed: %v", err)
	}

	if _, _, err := runCLI(t, "task", "get", "--id", taskID, "--json"); err != nil {
		t.Fatalf("task get failed: %v", err)
	}

	ensurePath := filepath.Join(t.TempDir(), "ensure.json")
	ensurePayload := fmt.Sprintf(`[{"title":"Ensure Applied","goal":"%s","status":"In Progress","priority":"Medium"}]`, goalID)
	if err := os.WriteFile(ensurePath, []byte(ensurePayload), 0o600); err != nil {
		t.Fatalf("write ensure file: %v", err)
	}

	if _, _, err := runCLI(t, "tasks", "ensure", "--goal", goalID, "--from-json", ensurePath, "--json"); err != nil {
		t.Fatalf("tasks ensure failed: %v", err)
	}

	exportPath := filepath.Join(t.TempDir(), "export.json")
	if _, _, err := runCLI(t, "export", "--goal", goalID, "--compact", "--out", exportPath); err != nil {
		t.Fatalf("export failed: %v", err)
	}

	if _, _, err := runCLI(t, "run"); err != nil {
		t.Fatalf("run command failed: %v", err)
	}
	if _, _, err := runCLI(t, "test"); err != nil {
		t.Fatalf("test command failed: %v", err)
	}
	if _, _, err := runCLI(t, "test-all"); err != nil {
		t.Fatalf("test-all command failed: %v", err)
	}

	if _, _, err := runCLI(t, "task", "delete", "--id", taskID, "--json"); err != nil {
		t.Fatalf("task delete failed: %v", err)
	}

	goalOut, _, err := runCLI(t, "goal", "create", "--title", "CLI Goal", "--status", "Active", "--emoji", "🎯", "--json")
	if err != nil {
		t.Fatalf("goal create failed: %v", err)
	}
	var goalResp map[string]any
	if err := json.Unmarshal([]byte(goalOut), &goalResp); err != nil {
		t.Fatalf("parse goal create json: %v", err)
	}
	goalCreatedID := asString(goalResp["id"])
	if goalCreatedID == "" {
		t.Fatalf("goal create missing id: %v", goalResp)
	}
	if _, _, err := runCLI(t, "goal", "update", "--id", goalCreatedID, "--status", "Completed", "--json"); err != nil {
		t.Fatalf("goal update failed: %v", err)
	}
	if _, _, err := runCLI(t, "goal", "delete", "--id", goalCreatedID, "--json"); err != nil {
		t.Fatalf("goal delete failed: %v", err)
	}

	mcpTaskID := exerciseMCP(t, goalID)
	if mcpTaskID == "" {
		t.Fatalf("mcp task id empty")
	}

	page, ok := srv.Page(mcpTaskID)
	if !ok {
		t.Fatalf("mcp task not found in mock server")
	}
	if !page.Archived {
		t.Fatalf("mcp task expected archived after delete, got %#v", page)
	}
}

func exerciseMCP(t *testing.T, goalID string) string {
	readerR, readerW := io.Pipe()
	writerR, writerW := io.Pipe()

	done := make(chan error, 1)
	go func() {
		done <- runMCPServer(readerR, writerW)
	}()

	var createdTaskID string

	send := func(req map[string]any) {
		data, _ := json.Marshal(req)
		frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(data), data)
		if _, err := io.WriteString(readerW, frame); err != nil {
			t.Fatalf("write frame: %v", err)
		}
	}

	readResp := func() mcpResponse {
		reader := bufio.NewReader(writerR)
		length := 0
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("read header: %v", err)
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if strings.HasPrefix(strings.ToLower(line), "content-length:") {
				fmt.Sscanf(line, "Content-Length: %d", &length)
			}
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(reader, body); err != nil {
			t.Fatalf("read body: %v", err)
		}
		var resp mcpResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return resp
	}

	send(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
	})
	if resp := readResp(); resp.Error != nil {
		t.Fatalf("initialize error: %+v", resp.Error)
	}

	send(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})

	send(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	})
	if resp := readResp(); resp.Error != nil {
		t.Fatalf("tools/list error: %+v", resp.Error)
	}

	send(map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "task.create",
			"arguments": map[string]any{
				"title": "MCP Task",
				"set": []any{
					"Status:status=In Progress",
					fmt.Sprintf("Goals:relation=%s", goalID),
				},
			},
		},
	})
	resp := readResp()
	if resp.Error != nil {
		t.Fatalf("task.create error: %+v", resp.Error)
	}
	var callOut mcpCallResult
	if err := json.Unmarshal(resp.Result, &callOut); err != nil {
		t.Fatalf("decode task.create result: %v", err)
	}
	if len(callOut.Content) == 0 {
		t.Fatalf("task.create empty content")
	}
	createdTaskID = parseTaskID(callOut.Content[0])
	if createdTaskID == "" {
		t.Fatalf("unable to parse task id from %q", callOut.Content[0].Text)
	}

	send(map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "task.update",
			"arguments": map[string]any{
				"id":  createdTaskID,
				"set": []any{"Status:status=Done"},
			},
		},
	})
	if resp = readResp(); resp.Error != nil {
		t.Fatalf("task.update error: %+v", resp.Error)
	}

	send(map[string]any{
		"jsonrpc": "2.0",
		"id":      5,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "task.get",
			"arguments": map[string]any{"id": createdTaskID},
		},
	})
	if resp = readResp(); resp.Error != nil {
		t.Fatalf("task.get error: %+v", resp.Error)
	}

	send(map[string]any{
		"jsonrpc": "2.0",
		"id":      6,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "task.delete",
			"arguments": map[string]any{"id": createdTaskID},
		},
	})
	if resp = readResp(); resp.Error != nil {
		t.Fatalf("task.delete error: %+v", resp.Error)
	}

	readerW.Close()
	<-done
	writerW.Close()

	return createdTaskID
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func parseTaskID(content mcpContentOut) string {
	if content.Type == "application/json" {
		if m, ok := content.Data.(map[string]any); ok {
			return asString(m["id"])
		}
		if raw, ok := content.Data.(map[string]interface{}); ok {
			return asString(raw["id"])
		}
	}
	if content.Text != "" {
		var payload map[string]any
		if err := json.Unmarshal([]byte(content.Text), &payload); err == nil {
			return asString(payload["id"])
		}
	}
	return ""
}
