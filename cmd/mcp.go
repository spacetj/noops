package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

// Minimal MCP over stdio using JSON-RPC 2.0 with Content-Length framing.

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *jsonrpcError `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCP structures (subset)
type initializeResult struct {
	ProtocolVersion string      `json:"protocolVersion"`
	ServerInfo      serverInfo  `json:"serverInfo"`
	Capabilities    interface{} `json:"capabilities"`
}
type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type toolsListResult struct {
	Tools []mcpTool `json:"tools"`
}

type mcpTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

type toolsCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type toolsCallResult struct {
	Content []mcpContent `json:"content"`
}

type mcpContent struct {
	Type string      `json:"type"`
	Text string      `json:"text,omitempty"`
	Data interface{} `json:"data,omitempty"`
}

var (
	flagCodexConfigPath string
	flagCodexServerName string
	flagCodexForce      bool
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run an MCP server over stdio",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize flags from env similar to CLI pre-run for server usage.
		initFlagsFromEnv()
		if flagToken == "" || flagDBID == "" {
			return fmt.Errorf("mcp requires NOTION_TOKEN and TASKS_DB_ID to be configured")
		}
		return runMCPServer(os.Stdin, os.Stdout)
	},
}

var mcpInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Bootstrap MCP client integrations",
}

var mcpInitCodexCmd = &cobra.Command{
	Use:   "codex",
	Short: "Configure Codex CLI to talk to the local MCP server",
	Long: "Adds or updates a Codex CLI MCP server entry that runs this `noops` binary over stdio. " +
		"The generated entry includes the current Notion token/database IDs so Codex can start the MCP session automatically.",
	Example: "  noops mcp init codex --token $NOTION_TOKEN --db $TASKS_DB_ID",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, serverName, err := runMCPInitCodex()
		if err != nil {
			return err
		}
		if flagJSON {
			payload := map[string]any{
				"ok":      true,
				"config":  configPath,
				"server":  serverName,
				"command": "mcp",
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(payload)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Configured Codex server %q in %s\n", serverName, configPath)
		return nil
	},
}

func init() {
	mcpInitCmd.AddCommand(mcpInitCodexCmd)
	mcpInitCodexCmd.Flags().StringVar(&flagCodexConfigPath, "config", "", "Path to Codex CLI config (default ~/.config/codex/config.json)")
	mcpInitCodexCmd.Flags().StringVar(&flagCodexServerName, "name", "", "Codex MCP server name (default noops-local)")
	mcpInitCodexCmd.Flags().BoolVar(&flagCodexForce, "force", false, "Overwrite existing Codex server entry if present")
	mcpCmd.AddCommand(mcpInitCmd)
	rootCmd.AddCommand(mcpCmd)
}

func initFlagsFromEnv() {
	// mimic PersistentPreRunE logic for server context
	if flagDotEnv != "" {
		// Ensure dotenv variables are visible before reading from the environment
		_ = loadDotEnv(flagDotEnv)
	}

	if flagToken == "" {
		flagToken = os.Getenv("NOTION_TOKEN")
	}
	if flagDBID == "" {
		flagDBID = os.Getenv("TASKS_DB_ID")
	}
	if flagGoal == "" {
		flagGoal = os.Getenv("GOAL_PAGE_ID")
	}
	if flagDBID != "" {
		flagDBID = normalizeID(flagDBID)
	}
	if flagGoal != "" {
		flagGoal = normalizeID(flagGoal)
	}
	if flagVersion == "" {
		flagVersion = "2022-06-28"
	}
}

func runMCPServer(r io.Reader, w io.Writer) error {
	rw := &framedRW{r: bufio.NewReader(r), w: bufio.NewWriter(w)}
	defer rw.w.Flush()

	// advertise tools on list
	for {
		req, err := rw.read()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		resp, ok := processJSONRPCRequest(req)
		if !ok {
			continue
		}
		if err := rw.write(*resp); err != nil {
			return err
		}
	}
}

func processJSONRPCRequest(req *jsonrpcRequest) (*jsonrpcResponse, bool) {
	if req == nil {
		return nil, false
	}
	if req.ID == nil && req.Method == "notifications/initialized" {
		// Notifications do not expect a response
		return nil, false
	}

	switch req.Method {
	case "initialize":
		res := initializeResult{
			ProtocolVersion: "2024-11-05",
			ServerInfo:      serverInfo{Name: "noops", Version: cliVersion},
			Capabilities: map[string]any{
				"tools": map[string]any{},
			},
		}
		return &jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: res}, true
	case "tools/list":
		tools := toolsListResult{Tools: mcpToolsDefinition()}
		return &jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: tools}, true
	case "tools/call":
		var p toolsCallParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return newJSONRPCError(req.ID, -32602, "invalid params: "+err.Error()), true
		}
		contents, err := dispatchToolCall(p.Name, p.Arguments)
		if err != nil {
			return newJSONRPCError(req.ID, -32000, err.Error()), true
		}
		return &jsonrpcResponse{JSONRPC: "2.0", ID: req.ID, Result: toolsCallResult{Content: contents}}, true
	default:
		return newJSONRPCError(req.ID, -32601, "method not found: "+req.Method), true
	}
}

func newJSONRPCError(id any, code int, msg string) *jsonrpcResponse {
	return &jsonrpcResponse{JSONRPC: "2.0", ID: id, Error: &jsonrpcError{Code: code, Message: msg}}
}

// Framed reader/writer for Content-Length headers (LSP-style)
type framedRW struct {
	r *bufio.Reader
	w *bufio.Writer
	m sync.Mutex
}

func (rw *framedRW) read() (*jsonrpcRequest, error) {
	tp := textproto.NewReader(rw.r)
	// Read headers until blank line
	headers := map[string]string{}
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return nil, err
		}
		if line == "" {
			break
		}
		if i := strings.Index(line, ":"); i >= 0 {
			k := strings.TrimSpace(line[:i])
			v := strings.TrimSpace(line[i+1:])
			headers[strings.ToLower(k)] = v
		}
	}
	cl, ok := headers["content-length"]
	if !ok {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	n, err := strconv.Atoi(cl)
	if err != nil {
		return nil, fmt.Errorf("bad Content-Length: %v", err)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(rw.r, buf); err != nil {
		return nil, err
	}
	var req jsonrpcRequest
	if err := json.Unmarshal(buf, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

func (rw *framedRW) write(resp jsonrpcResponse) error {
	rw.m.Lock()
	defer rw.m.Unlock()
	b, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(rw.w, "Content-Length: %d\r\n\r\n", len(b)); err != nil {
		return err
	}
	if _, err := rw.w.Write(b); err != nil {
		return err
	}
	return rw.w.Flush()
}

func (rw *framedRW) writeErr(id any, code int, msg string) error {
	return rw.write(jsonrpcResponse{JSONRPC: "2.0", ID: id, Error: &jsonrpcError{Code: code, Message: msg}})
}

func mcpToolsDefinition() []mcpTool {
	// JSON Schemas are minimal but sufficient
	return []mcpTool{
		{
			Name:        "task.create",
			Description: "Create a task in the Notion Tasks DB",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title": map[string]any{"type": "string"},
					"set":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"emoji": map[string]any{"type": "string"},
				},
				"required": []string{"title"},
			},
		},
		{
			Name:        "task.update",
			Description: "Update a task by id or title",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":     map[string]any{"type": "string"},
					"title":  map[string]any{"type": "string"},
					"rename": map[string]any{"type": "string"},
					"set":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"emoji":  map[string]any{"type": "string"},
					"desc":   map[string]any{"type": "string"},
				},
			},
		},
		{
			Name:        "task.delete",
			Description: "Archive (delete) a task",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":    map[string]any{"type": "string"},
					"title": map[string]any{"type": "string"},
				},
			},
		},
		{
			Name:        "task.get",
			Description: "Get a task/page by id or title",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":    map[string]any{"type": "string"},
					"title": map[string]any{"type": "string"},
				},
			},
		},
		{
			Name:        "goal.list",
			Description: "List distinct related goals from Tasks DB",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name:        "goal.info",
			Description: "Show goal info by name or ID",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":   map[string]any{"type": "string"},
					"name": map[string]any{"type": "string"},
				},
			},
		},
		{
			Name:        "goal.create",
			Description: "Create a goal page",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":  map[string]any{"type": "string"},
					"status": map[string]any{"type": "string"},
					"emoji":  map[string]any{"type": "string"},
				},
				"required": []string{"title"},
			},
		},
		{
			Name:        "goal.update",
			Description: "Update a goal by id or title",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":     map[string]any{"type": "string"},
					"title":  map[string]any{"type": "string"},
					"rename": map[string]any{"type": "string"},
					"status": map[string]any{"type": "string"},
					"emoji":  map[string]any{"type": "string"},
				},
			},
		},
		{
			Name:        "goal.delete",
			Description: "Archive (delete) a goal",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":    map[string]any{"type": "string"},
					"title": map[string]any{"type": "string"},
				},
			},
		},
	}
}

var dispatchMu sync.Mutex

func dispatchToolCall(name string, args map[string]interface{}) ([]mcpContent, error) {
	dispatchMu.Lock()
	defer dispatchMu.Unlock()

	// Ensure env init on each call (in case env changed)
	initFlagsFromEnv()
	// Capture output by temporarily redirecting stdout (the CLI prints results)
	// but we prefer to get structured strings. We'll let the underlying command print
	// and capture it.
	var outBuf strings.Builder
	log.SetOutput(os.Stderr)

	// Save and swap os.Stdout
	saveStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				outBuf.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	// Reset shared flag vars before each invocation
	resetSharedFlags()

	var callErr error
	switch name {
	case "task.create":
		// map args to globals then call taskCreate
		if v, ok := args["title"].(string); ok {
			taskTitle = v
		}
		if arr, ok := toStrSlice(args["set"]); ok {
			setProps = arr
		}
		if v, ok := args["emoji"].(string); ok {
			taskEmoji = v
		}
		callErr = taskCreate(nil, nil)
	case "task.update":
		if v, ok := args["id"].(string); ok {
			taskID = v
		}
		if v, ok := args["title"].(string); ok {
			taskTitle = v
		}
		if v, ok := args["rename"].(string); ok {
			newTitle = v
		}
		if arr, ok := toStrSlice(args["set"]); ok {
			setProps = arr
		}
		if v, ok := args["emoji"].(string); ok {
			taskEmoji = v
		}
		if v, ok := args["desc"].(string); ok {
			taskDesc = v
		}
		callErr = taskUpdate(nil, nil)
	case "task.delete":
		if v, ok := args["id"].(string); ok {
			taskID = v
		}
		if v, ok := args["title"].(string); ok {
			taskTitle = v
		}
		callErr = taskDelete(nil, nil)
	case "task.get":
		if v, ok := args["id"].(string); ok {
			taskID = v
		}
		if v, ok := args["title"].(string); ok {
			taskTitle = v
		}
		callErr = taskGet(nil, nil)
	case "goal.list":
		callErr = goalList(nil, nil)
	case "goal.info":
		if v, ok := args["id"].(string); ok {
			goalInfoID = v
		}
		if v, ok := args["name"].(string); ok {
			goalInfoName = v
		}
		callErr = goalInfo(nil, nil)
	case "goal.create":
		if v, ok := args["title"].(string); ok {
			goalCreateTitle = v
		}
		if v, ok := args["status"].(string); ok {
			goalCreateStatus = v
		}
		if v, ok := args["emoji"].(string); ok {
			goalCreateEmoji = v
		}
		callErr = goalCreate(nil, nil)
	case "goal.update":
		if v, ok := args["id"].(string); ok {
			goalUpdateID = v
		}
		if v, ok := args["title"].(string); ok {
			goalUpdateTitle = v
		}
		if v, ok := args["rename"].(string); ok {
			goalUpdateRename = v
		}
		if v, ok := args["status"].(string); ok {
			goalUpdateStatus = v
		}
		if v, ok := args["emoji"].(string); ok {
			goalUpdateEmoji = v
		}
		callErr = goalUpdate(nil, nil)
	case "goal.delete":
		if v, ok := args["id"].(string); ok {
			goalDeleteID = v
		}
		if v, ok := args["title"].(string); ok {
			goalDeleteTitle = v
		}
		callErr = goalDelete(nil, nil)
	default:
		callErr = fmt.Errorf("unknown tool: %s", name)
	}

	// Restore stdout
	w.Close()
	<-done
	os.Stdout = saveStdout

	if callErr != nil {
		return nil, callErr
	}
	return buildMCPContent(outBuf.String()), nil
}

func resetSharedFlags() {
	// clear per-command inputs
	setProps = nil
	taskID = ""
	taskTitle = ""
	newTitle = ""
	taskEmoji = ""
	taskDesc = ""
	goalInfoID = ""
	goalInfoName = ""
	goalCreateTitle = ""
	goalCreateStatus = ""
	goalCreateEmoji = ""
	goalUpdateID = ""
	goalUpdateTitle = ""
	goalUpdateRename = ""
	goalUpdateStatus = ""
	goalUpdateEmoji = ""
	goalDeleteID = ""
	goalDeleteTitle = ""
	// Prefer JSON output for MCP consumption
	flagJSON = true
}

func toStrSlice(v any) ([]string, bool) {
	if v == nil {
		return nil, false
	}
	switch t := v.(type) {
	case []string:
		return t, true
	case []any:
		out := make([]string, 0, len(t))
		for _, it := range t {
			if s, ok := it.(string); ok {
				out = append(out, s)
			}
		}
		return out, true
	default:
		return nil, false
	}
}

func buildMCPContent(out string) []mcpContent {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return []mcpContent{{Type: "text", Text: ""}}
	}
	var payload any
	if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
		return []mcpContent{{Type: "application/json", Data: payload}}
	}
	return []mcpContent{{Type: "text", Text: trimmed}}
}

func runMCPInitCodex() (string, string, error) {
	configPath := flagCodexConfigPath
	if configPath == "" {
		var err error
		configPath, err = defaultCodexConfigPath()
		if err != nil {
			return "", "", err
		}
	}
	serverName := flagCodexServerName
	if strings.TrimSpace(serverName) == "" {
		serverName = "noops-local"
	}

	exe, err := os.Executable()
	if err != nil {
		exe = "noops"
	} else if abs, absErr := filepath.Abs(exe); absErr == nil {
		exe = abs
	}

	fileConfig := map[string]any{}
	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, &fileConfig); err != nil {
			return "", "", fmt.Errorf("parse Codex config: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("read Codex config: %w", err)
	}

	servers := map[string]codexServer{}
	if raw, ok := fileConfig["servers"]; ok && raw != nil {
		if b, err := json.Marshal(raw); err == nil {
			if err := json.Unmarshal(b, &servers); err != nil {
				return "", "", fmt.Errorf("parse existing Codex servers: %w", err)
			}
		}
	}
	if servers == nil {
		servers = map[string]codexServer{}
	}
	if _, exists := servers[serverName]; exists && !flagCodexForce {
		return "", "", fmt.Errorf("Codex server %q already exists (use --force to overwrite)", serverName)
	}

	env := map[string]string{
		"NOTION_TOKEN": flagToken,
		"TASKS_DB_ID":  flagDBID,
	}
	if flagGoal != "" {
		env["GOAL_PAGE_ID"] = flagGoal
	}
	if flagVersion != "" {
		env["NOTION_VERSION"] = flagVersion
	}
	if base := effectiveBaseURL(); base != "" {
		env["NOTION_BASE_URL"] = base
	}

	servers[serverName] = codexServer{
		Type: "mcp",
		Transport: codexTransport{
			Type:    "stdio",
			Command: exe,
			Args:    []string{"mcp"},
			Env:     env,
		},
	}
	fileConfig["servers"] = servers

	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return "", "", fmt.Errorf("create Codex config directory: %w", err)
	}
	data, err := json.MarshalIndent(fileConfig, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("encode Codex config: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		return "", "", fmt.Errorf("write Codex config: %w", err)
	}
	if err := os.Chmod(configPath, 0o600); err != nil {
		return "", "", fmt.Errorf("chmod Codex config: %w", err)
	}
	return configPath, serverName, nil
}

func defaultCodexConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("detect user home: %w", err)
	}
	return filepath.Join(home, ".config", "codex", "config.json"), nil
}

type codexServer struct {
	Type      string         `json:"type"`
	Transport codexTransport `json:"transport"`
}

type codexTransport struct {
	Type    string            `json:"type"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}
