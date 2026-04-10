package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spacetj/noops/internal/ops"
	"github.com/spf13/cobra"
)

var (
	initPageName        string
	initTasksName       string
	initGoalsName       string
	initDefaultGoalName string
	initWriteEnv        bool
)

func init() {
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Create a Notion workspace page and databases required by noops",
		RunE:  runInit,
	}
	initCmd.Flags().StringVar(&initPageName, "page-name", "Noops Ops", "Title for the parent workspace page to create")
	initCmd.Flags().StringVar(&initTasksName, "tasks-name", "Noops Tasks", "Title for the tasks database")
	initCmd.Flags().StringVar(&initGoalsName, "goals-name", "Noops Goals", "Title for the goals database")
	initCmd.Flags().StringVar(&initDefaultGoalName, "goal-title", "Default Goal", "Default goal page to seed within the goals database")
	initCmd.Flags().BoolVar(&initWriteEnv, "write-env", true, "Write TASKS_DB_ID and GOAL_PAGE_ID to the dotenv file")

	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	if flagToken == "" {
		flagToken = os.Getenv("NOTION_TOKEN")
	}
	if flagToken == "" {
		return fmt.Errorf("NOTION_TOKEN is required (set --token or env NOTION_TOKEN) for init")
	}

	logger := log.Default()
	if flagDebug {
		logger.SetFlags(log.LstdFlags | log.Lshortfile)
	} else {
		logger.SetFlags(0)
	}

	client := newNotionClient()

	pageID, err := createWorkspacePage(client, initPageName)
	if err != nil {
		return err
	}

	goalsDBID, err := createGoalsDatabase(client, pageID, initGoalsName)
	if err != nil {
		return err
	}

	defaultGoalID, err := createGoalEntry(client, goalsDBID, initDefaultGoalName)
	if err != nil {
		return err
	}

	tasksDBID, err := createTasksDatabase(client, pageID, goalsDBID, initTasksName)
	if err != nil {
		return err
	}

	if err := addSelfRelation(client, tasksDBID); err != nil {
		return err
	}

	flagDBID = tasksDBID
	flagGoal = defaultGoalID
	_ = os.Setenv("TASKS_DB_ID", tasksDBID)
	_ = os.Setenv("GOAL_PAGE_ID", defaultGoalID)

	result := map[string]any{
		"ok":              true,
		"page_id":         pageID,
		"tasks_db_id":     tasksDBID,
		"goals_db_id":     goalsDBID,
		"default_goal_id": defaultGoalID,
	}

	envPath := ""
	if initWriteEnv {
		envPath, err = writeEnvValues(flagDotEnv, map[string]string{
			"TASKS_DB_ID":  tasksDBID,
			"GOAL_PAGE_ID": defaultGoalID,
		})
		if err != nil {
			return fmt.Errorf("write env: %w", err)
		}
		if envPath != "" {
			result["env_file"] = envPath
		}
	}

	if flagJSON {
		b, _ := json.Marshal(result)
		fmt.Println(string(b))
		return nil
	}

	fmt.Printf("Created workspace page %s\n", pageID)
	fmt.Printf("Goals database: %s\n", goalsDBID)
	fmt.Printf("Tasks database: %s\n", tasksDBID)
	fmt.Printf("Default goal page: %s\n", defaultGoalID)
	if envPath != "" {
		fmt.Printf("Updated %s with TASKS_DB_ID and GOAL_PAGE_ID\n", envPath)
	}
	return nil
}

func createWorkspacePage(client interface {
	CreatePage([]byte) ([]byte, int, error)
}, title string) (string, error) {
	payload := map[string]any{
		"parent": map[string]any{"workspace": true},
		"icon":   map[string]any{"type": "emoji", "emoji": "🗂️"},
		"properties": map[string]any{
			"title": map[string]any{"title": notionRichText(title)},
		},
	}
	body, _ := json.Marshal(payload)
	b, code, err := client.CreatePage(body)
	if err != nil || code/100 != 2 {
		return "", fmt.Errorf("create workspace page: %d %v %s", code, err, string(b))
	}
	id, err := extractID(b)
	if err != nil {
		return "", err
	}
	return id, nil
}

func createGoalsDatabase(client interface {
	CreateDatabase([]byte) ([]byte, int, error)
}, parentPageID, title string) (string, error) {
	payload := map[string]any{
		"parent": map[string]any{"type": "page_id", "page_id": parentPageID},
		"title":  notionDatabaseTitle(title),
		"properties": map[string]any{
			"Name": map[string]any{"title": map[string]any{}},
			"Status": map[string]any{
				"status": map[string]any{
					"options": []any{
						map[string]any{"name": "Active"},
						map[string]any{"name": "Completed"},
					},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)
	b, code, err := client.CreateDatabase(body)
	if err != nil || code/100 != 2 {
		return "", fmt.Errorf("create goals database: %d %v %s", code, err, string(b))
	}
	id, err := extractID(b)
	if err != nil {
		return "", err
	}
	return id, nil
}

func createGoalEntry(client interface {
	CreatePage([]byte) ([]byte, int, error)
}, goalsDBID, title string) (string, error) {
	payload := map[string]any{
		"parent": map[string]any{"database_id": goalsDBID},
		"properties": map[string]any{
			"Name": ops.TitleProp(title),
			"Status": map[string]any{
				"status": map[string]any{"name": "Active"},
			},
		},
	}
	body, _ := json.Marshal(payload)
	b, code, err := client.CreatePage(body)
	if err != nil || code/100 != 2 {
		return "", fmt.Errorf("create default goal page: %d %v %s", code, err, string(b))
	}
	id, err := extractID(b)
	if err != nil {
		return "", err
	}
	return id, nil
}

func createTasksDatabase(client interface {
	CreateDatabase([]byte) ([]byte, int, error)
}, parentPageID, goalsDBID, title string) (string, error) {
	payload := map[string]any{
		"parent": map[string]any{"type": "page_id", "page_id": parentPageID},
		"title":  notionDatabaseTitle(title),
		"properties": map[string]any{
			"Name":      map[string]any{"title": map[string]any{}},
			"Status":    map[string]any{"status": map[string]any{"options": defaultStatusOptions()}},
			"Priority":  map[string]any{"select": map[string]any{"options": defaultPriorityOptions()}},
			"Do":        map[string]any{"date": map[string]any{}},
			"Due":       map[string]any{"date": map[string]any{}},
			"Completed": map[string]any{"checkbox": map[string]any{}},
			"Tags":      map[string]any{"multi_select": map[string]any{}},
			"URL":       map[string]any{"url": map[string]any{}},
			"Assignee":  map[string]any{"people": map[string]any{}},
			"Goals": map[string]any{
				"relation": map[string]any{
					"database_id":          goalsDBID,
					"synced_property_name": "Tasks",
				},
				"type": "relation",
			},
		},
	}
	body, _ := json.Marshal(payload)
	b, code, err := client.CreateDatabase(body)
	if err != nil || code/100 != 2 {
		return "", fmt.Errorf("create tasks database: %d %v %s", code, err, string(b))
	}
	id, err := extractID(b)
	if err != nil {
		return "", err
	}
	return id, nil
}

func addSelfRelation(client interface {
	UpdateDatabase(string, []byte) ([]byte, int, error)
}, tasksDBID string) error {
	payload := map[string]any{
		"properties": map[string]any{
			"Parent-task": map[string]any{
				"relation": map[string]any{
					"database_id":          tasksDBID,
					"synced_property_name": "Sub-tasks",
				},
				"type": "relation",
			},
		},
	}
	body, _ := json.Marshal(payload)
	b, code, err := client.UpdateDatabase(tasksDBID, body)
	if err != nil || code/100 != 2 {
		return fmt.Errorf("update tasks database (self relation): %d %v %s", code, err, string(b))
	}
	return nil
}

func extractID(body []byte) (string, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return "", err
	}
	id, _ := m["id"].(string)
	if id == "" {
		return "", errors.New("response missing id")
	}
	return id, nil
}

func notionRichText(text string) []any {
	return []any{map[string]any{
		"type":       "text",
		"plain_text": text,
		"text": map[string]any{
			"content": text,
		},
	}}
}

func notionDatabaseTitle(text string) []any {
	return []any{map[string]any{
		"type": "text",
		"text": map[string]any{"content": text},
	}}
}

func defaultStatusOptions() []any {
	return []any{
		map[string]any{"name": "Not Started"},
		map[string]any{"name": "In Progress"},
		map[string]any{"name": "Blocked"},
		map[string]any{"name": "Done"},
	}
}

func defaultPriorityOptions() []any {
	return []any{
		map[string]any{"name": "High"},
		map[string]any{"name": "Medium"},
		map[string]any{"name": "Low"},
	}
}

func writeEnvValues(path string, values map[string]string) (string, error) {
	if path == "" {
		path = ".env"
	}
	content := []string{}
	seen := map[string]bool{}

	data, err := os.ReadFile(path)
	if err == nil {
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				content = append(content, line)
				continue
			}
			if idx := strings.Index(trimmed, "="); idx >= 0 {
				key := strings.TrimSpace(trimmed[:idx])
				if val, ok := values[key]; ok {
					content = append(content, fmt.Sprintf("%s=%q", key, val))
					seen[key] = true
					continue
				}
			}
			content = append(content, line)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	keys := make([]string, 0, len(values))
	for k := range values {
		if seen[k] {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		content = append(content, fmt.Sprintf("%s=%q", k, values[k]))
	}

	builder := strings.Join(content, "\n")
	if len(content) > 0 {
		builder += "\n"
	}
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	tmp, err := os.CreateTemp(dir, fmt.Sprintf(".%s.tmp-*", filepath.Base(path)))
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	succeeded := false
	defer func() {
		if succeeded {
			return
		}
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.WriteString(builder); err != nil {
		return "", err
	}
	if err := tmp.Chmod(0o600); err != nil {
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", err
	}
	succeeded = true
	if dirHandle, err := os.Open(dir); err == nil {
		_ = dirHandle.Sync()
		_ = dirHandle.Close()
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path, nil
	}
	return abs, nil
}
