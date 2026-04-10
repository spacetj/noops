package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitFlagsFromEnvLoadsDotEnv(t *testing.T) {
	origToken, origDBID := flagToken, flagDBID
	origGoal, origVersion := flagGoal, flagVersion
	origDotEnv := flagDotEnv
	t.Cleanup(func() {
		flagToken = origToken
		flagDBID = origDBID
		flagGoal = origGoal
		flagVersion = origVersion
		flagDotEnv = origDotEnv
	})

	t.Setenv("NOTION_TOKEN", "")
	t.Setenv("TASKS_DB_ID", "")
	t.Setenv("GOAL_PAGE_ID", "")

	envPath := filepath.Join(t.TempDir(), "local.env")
	content := "NOTION_TOKEN=ntn_from_env\n" +
		"TASKS_DB_ID=0123456789abcdef0123456789abcdef\n" +
		"GOAL_PAGE_ID=fedcba9876543210fedcba9876543210\n"
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	flagToken = ""
	flagDBID = ""
	flagGoal = ""
	flagVersion = ""
	flagDotEnv = envPath

	initFlagsFromEnv()

	if flagToken != "ntn_from_env" {
		t.Fatalf("expected token from dotenv, got %q", flagToken)
	}
	if flagDBID != "01234567-89ab-cdef-0123-456789abcdef" {
		t.Fatalf("expected normalized db id, got %q", flagDBID)
	}
	if flagGoal != "fedcba98-7654-3210-fedc-ba9876543210" {
		t.Fatalf("expected normalized goal id, got %q", flagGoal)
	}
	if flagVersion != "2022-06-28" {
		t.Fatalf("expected default version, got %q", flagVersion)
	}
}

func TestProcessJSONRPCRequestInitialize(t *testing.T) {
	resp, ok := processJSONRPCRequest(&jsonrpcRequest{ID: 1, Method: "initialize"})
	if !ok {
		t.Fatalf("initialize should return response")
	}
	if resp.Error != nil {
		t.Fatalf("initialize returned error: %+v", resp.Error)
	}
	res, ok := resp.Result.(initializeResult)
	if !ok {
		t.Fatalf("result type mismatch: %#v", resp.Result)
	}
	if res.ServerInfo.Name != "noops" {
		t.Fatalf("unexpected server name: %q", res.ServerInfo.Name)
	}
	if res.ProtocolVersion == "" {
		t.Fatalf("protocol version empty")
	}
}

func TestProcessJSONRPCRequestNotifications(t *testing.T) {
	resp, ok := processJSONRPCRequest(&jsonrpcRequest{Method: "notifications/initialized"})
	if resp != nil {
		t.Fatalf("expected no response for notification, got %#v", resp)
	}
	if ok {
		t.Fatalf("notification should not write a response")
	}
}

func TestProcessJSONRPCRequestUnknown(t *testing.T) {
	resp, ok := processJSONRPCRequest(&jsonrpcRequest{ID: 9, Method: "bogus"})
	if !ok {
		t.Fatalf("unknown method should still respond")
	}
	if resp.Error == nil {
		t.Fatalf("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("unexpected error code: %d", resp.Error.Code)
	}
}

func TestRunMCPInitCodexWritesConfig(t *testing.T) {
	origConfig := flagCodexConfigPath
	origName := flagCodexServerName
	origForce := flagCodexForce
	origToken, origDBID := flagToken, flagDBID
	origGoal, origVersion := flagGoal, flagVersion
	t.Cleanup(func() {
		flagCodexConfigPath = origConfig
		flagCodexServerName = origName
		flagCodexForce = origForce
		flagToken = origToken
		flagDBID = origDBID
		flagGoal = origGoal
		flagVersion = origVersion
	})

	configPath := filepath.Join(t.TempDir(), "codex.json")
	flagCodexConfigPath = configPath
	flagCodexServerName = "noops-test"
	flagCodexForce = false
	flagToken = "ntn_test"
	flagDBID = "01234567-89ab-cdef-0123-456789abcdef"
	flagGoal = "fedcba98-7654-3210-fedc-ba9876543210"
	flagVersion = "2022-06-28"

	writtenPath, serverName, err := runMCPInitCodex()
	if err != nil {
		t.Fatalf("runMCPInitCodex: %v", err)
	}
	if writtenPath != configPath {
		t.Fatalf("expected config path %q, got %q", configPath, writtenPath)
	}
	if serverName != "noops-test" {
		t.Fatalf("unexpected server name %q", serverName)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg struct {
		Servers map[string]codexServer `json:"servers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse config json: %v", err)
	}
	entry, ok := cfg.Servers["noops-test"]
	if !ok {
		t.Fatalf("server entry not found")
	}
	if entry.Transport.Type != "stdio" {
		t.Fatalf("transport type mismatch: %q", entry.Transport.Type)
	}
	if len(entry.Transport.Args) != 1 || entry.Transport.Args[0] != "mcp" {
		t.Fatalf("unexpected args: %#v", entry.Transport.Args)
	}
	if entry.Transport.Env["NOTION_TOKEN"] != "ntn_test" {
		t.Fatalf("env token mismatch: %#v", entry.Transport.Env)
	}
	if entry.Transport.Env["GOAL_PAGE_ID"] != "fedcba98-7654-3210-fedc-ba9876543210" {
		t.Fatalf("env goal mismatch: %#v", entry.Transport.Env)
	}

	if _, _, err := runMCPInitCodex(); err == nil {
		t.Fatalf("expected error when entry exists without force")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}

	flagCodexForce = true
	if _, _, err := runMCPInitCodex(); err != nil {
		t.Fatalf("force write failed: %v", err)
	}
}

func TestRunMCPInitCodexDefaultName(t *testing.T) {
	origConfig := flagCodexConfigPath
	origName := flagCodexServerName
	origForce := flagCodexForce
	origToken, origDBID := flagToken, flagDBID
	origGoal, origVersion := flagGoal, flagVersion
	t.Cleanup(func() {
		flagCodexConfigPath = origConfig
		flagCodexServerName = origName
		flagCodexForce = origForce
		flagToken = origToken
		flagDBID = origDBID
		flagGoal = origGoal
		flagVersion = origVersion
	})

	flagCodexConfigPath = filepath.Join(t.TempDir(), "codex.json")
	flagCodexServerName = ""
	flagCodexForce = false
	flagToken = "ntn_default"
	flagDBID = "01234567-89ab-cdef-0123-456789abcdef"
	flagGoal = ""
	flagVersion = "2022-06-28"

	_, serverName, err := runMCPInitCodex()
	if err != nil {
		t.Fatalf("runMCPInitCodex: %v", err)
	}
	if serverName != "noops-local" {
		t.Fatalf("expected default server name, got %q", serverName)
	}
	data, err := os.ReadFile(flagCodexConfigPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg struct {
		Servers map[string]codexServer `json:"servers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if _, ok := cfg.Servers["noops-local"]; !ok {
		t.Fatalf("default server missing")
	}
}
