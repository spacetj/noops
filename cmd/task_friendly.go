package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spacetj/noops/internal/ops"
	"github.com/spf13/cobra"
)

// Flags for friendly task add/list
var (
	fAddTitle     string
	fAddDo        string
	fAddDue       string
	fAddStatus    string
	fAddPriority  string
	fAddGoal      string
	fAddParent    string
	fAddEmoji     string
	fAddAutoEmoji bool
	fAddDesc      string

	fListGoal   string
	fListStatus string
	fListParent string
)

func init() {
	// attach to existing 'task' group
	var taskGroup *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Use == "task" {
			taskGroup = c
			break
		}
	}
	if taskGroup == nil {
		taskGroup = &cobra.Command{Use: "task", Short: "Task management commands"}
		rootCmd.AddCommand(taskGroup)
	}

	// Add (friendly create)
	add := &cobra.Command{Use: "add", Short: "Add a task (friendly)", RunE: taskAdd}
	add.Flags().StringVar(&fAddTitle, "title", "", "Task title (required)")
	add.Flags().StringVar(&fAddDo, "do", "", "Do date YYYY-MM-DD")
	add.Flags().StringVar(&fAddDue, "due", "", "Due date YYYY-MM-DD")
	add.Flags().StringVar(&fAddStatus, "status", "", "Status name")
	add.Flags().StringVar(&fAddPriority, "priority", "", "Priority option")
	add.Flags().StringVar(&fAddGoal, "goal", "", "Goal page name or ID/URL (defaults to last used)")
	add.Flags().StringVar(&fAddParent, "parent", "", "Parent task name or ID/URL (self-relation)")
	add.Flags().StringVar(&fAddEmoji, "emoji", "", "Emoji icon for page (e.g., '✅')")
	add.Flags().BoolVar(&fAddAutoEmoji, "auto-emoji", false, "Auto-suggest an emoji based on title if --emoji not set")
	add.Flags().StringVar(&fAddDesc, "desc", "", "Description text (uses 'Description' rich_text property if present)")
	taskGroup.AddCommand(add)

	// List (friendly)
	list := &cobra.Command{Use: "list", Short: "List tasks (friendly)", RunE: taskListFriendly}
	list.Flags().StringVar(&fListGoal, "goal", "", "Filter by goal name or ID/URL")
	list.Flags().StringVar(&fListStatus, "status", "", "Filter by status name")
	list.Flags().StringVar(&fListParent, "parent", "", "Filter by parent task name or ID/URL")
	taskGroup.AddCommand(list)
}

// ===== State (smart defaults) =====

type stateFile struct {
	LastGoalID string `json:"last_goal_id"`
}

func statePath() string { return filepath.Join(".noops_state.json") }

func loadState() stateFile {
	var s stateFile
	b, err := os.ReadFile(statePath())
	if err != nil {
		return s
	}
	_ = json.Unmarshal(b, &s)
	return s
}

func saveState(s stateFile) {
	b, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(statePath(), b, 0o600)
}

// ===== Helpers =====

var rxUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func isUUIDStr(s string) bool { return rxUUID.MatchString(strings.ToLower(strings.TrimSpace(s))) }

func suggestEmojiFor(title string) string {
	t := strings.ToLower(title)
	table := []struct{ key, e string }{
		{"internet", "🌐"}, {"wifi", "📶"}, {"grocer", "🛒"}, {"rent", "🏠"}, {"move", "🚚"},
		{"book", "📚"}, {"read", "📖"}, {"email", "📧"}, {"call", "📞"}, {"setup", "🧩"},
		{"pay", "💳"}, {"bank", "🏦"}, {"doctor", "🩺"}, {"todo", "✅"}, {"meeting", "📅"},
	}
	for _, it := range table {
		if strings.Contains(t, it.key) {
			return it.e
		}
	}
	return "📝"
}

func printTaskTable(rows []map[string]string) {
	// Columns: Task ID | Title | Status | Due | Parent
	headers := []string{"Task ID", "Title", "Status", "Due", "Parent"}
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, r := range rows {
		vals := []string{r["id"], r["title"], r["status"], r["due"], r["parent"]}
		for i, v := range vals {
			if len(v) > widths[i] {
				widths[i] = len(v)
			}
		}
	}
	// print header
	for i, h := range headers {
		fmt.Printf("%-*s", widths[i]+2, h)
	}
	fmt.Println()
	// separator
	for i := range headers {
		fmt.Printf("%-*s", widths[i]+2, strings.Repeat("-", widths[i]))
	}
	fmt.Println()
	// rows
	for _, r := range rows {
		vals := []string{r["id"], r["title"], r["status"], r["due"], r["parent"]}
		for i, v := range vals {
			fmt.Printf("%-*s", widths[i]+2, v)
		}
		fmt.Println()
	}
}

func resolveGoalID(ctx context.Context, r *ops.Runner, in string) string {
	if in == "" {
		return ""
	}
	cand := normalizeID(in)
	if isUUIDStr(cand) {
		return cand
	}
	// search among distinct related goal ids referenced by tasks
	client := r.Client()
	// query pages where Goals relation is not empty
	filter := map[string]any{"and": []any{map[string]any{"property": "Goals", "relation": map[string]any{"is_not_empty": true}}}}
	body, _ := ops.MarshalQuery(filter, nil, 100, "")
	b, code, err := client.QueryDatabase(r.TitleDB(), body)
	if err != nil || code/100 != 2 {
		return ""
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	results, _ := m["results"].([]any)
	uniq := map[string]struct{}{}
	ids := []string{}
	for _, it := range results {
		row := it.(map[string]any)
		props, _ := row["properties"].(map[string]any)
		goalRel, _ := props["Goals"].(map[string]any)
		if goalRel == nil {
			continue
		}
		arr, _ := goalRel["relation"].([]any)
		for _, ri := range arr {
			id := ri.(map[string]any)["id"].(string)
			if _, ok := uniq[id]; !ok {
				uniq[id] = struct{}{}
				ids = append(ids, id)
			}
		}
	}
	// fetch titles and match
	for _, id := range ids {
		pb, pcode, _ := client.GetPage(id)
		if pcode/100 != 2 {
			continue
		}
		var pm map[string]any
		_ = json.Unmarshal(pb, &pm)
		props, _ := pm["properties"].(map[string]any)
		t := ops.ReadTitle(props[r.TitleKey()])
		if strings.EqualFold(strings.TrimSpace(t), strings.TrimSpace(in)) {
			return id
		}
	}
	return ""
}

func taskAdd(cmd *cobra.Command, args []string) error {
	if fAddTitle == "" {
		return errors.New("--title is required")
	}
	logger := log.Default()
	if flagDebug {
		logger.SetFlags(log.LstdFlags | log.Lshortfile)
	}
	client := newNotionClient()
	r := ops.NewRunner(client, logger)
	r.SetDBID(normalizeID(flagDBID))
	ctx := context.Background()
	if err := r.FetchDB(ctx); err != nil {
		return err
	}

	// resolve goal with smart defaults
	st := loadState()
	goalID := ""
	if fAddGoal != "" {
		goalID = resolveGoalID(ctx, r, fAddGoal)
		if goalID == "" {
			goalID = normalizeID(fAddGoal)
		}
	} else if st.LastGoalID != "" {
		goalID = st.LastGoalID
	} else if flagGoal != "" {
		goalID = normalizeID(flagGoal)
	}

	// emoji
	emoji := fAddEmoji
	if emoji == "" && fAddAutoEmoji {
		emoji = suggestEmojiFor(fAddTitle)
	}

	// properties
	props := map[string]any{}
	if r.HasProp("Do", "date") && fAddDo != "" {
		props["Do"] = map[string]any{"date": map[string]any{"start": fAddDo}}
	}
	if r.HasProp("Due", "date") && fAddDue != "" {
		props["Due"] = map[string]any{"date": map[string]any{"start": fAddDue}}
	}
	if r.HasProp("Priority", "select") && fAddPriority != "" {
		props["Priority"] = map[string]any{"select": map[string]any{"name": fAddPriority}}
	}
	if r.StatusKey() != "" && fAddStatus != "" {
		props[r.StatusKey()] = map[string]any{"status": map[string]any{"name": fAddStatus}}
	}
	if goalID != "" && r.HasProp("Goals", "relation") {
		props["Goals"] = map[string]any{"relation": []any{map[string]any{"id": goalID}}}
	}
	// optional description (rich_text) property
	if fAddDesc != "" {
		if r.HasProp("Description", "rich_text") {
			props["Description"] = map[string]any{"rich_text": []any{map[string]any{"text": map[string]any{"content": fAddDesc}}}}
		}
	}

	id, err := r.CreateWithPropsEmoji(ctx, fAddTitle, props, emoji)
	if err != nil {
		return err
	}

	// parent relation
	if fAddParent != "" && id != "" {
		pid := normalizeID(fAddParent)
		if !isUUIDStr(pid) {
			pid = r.FirstByTitle(ctx, fAddParent)
		}
		if pid != "" {
			if err := r.SetParent(ctx, id, pid); err != nil && flagDebug {
				logger.Printf("warn set parent: %v", err)
			}
		}
	}

	// save last goal id
	if goalID != "" {
		st.LastGoalID = goalID
		saveState(st)
	}

	if flagJSON {
		fmt.Printf("{\"ok\":true,\"id\":\"%s\"}\n", id)
		return nil
	}

	// fetch data for pretty row
	b, code, _ := r.Client().GetPage(id)
	if code/100 != 2 {
		fmt.Println("created", id)
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	propsOut, _ := m["properties"].(map[string]any)
	row := map[string]string{
		"id":     id,
		"title":  ops.ReadTitle(propsOut[r.TitleKey()]),
		"status": "",
		"due":    "",
		"parent": "",
	}
	if r.StatusKey() != "" {
		if s := ops.ReadStatus(propsOut[r.StatusKey()]); s != "" {
			row["status"] = s
		}
	}
	if d := ops.ReadDateStart(propsOut["Due"]); d != "" {
		row["due"] = d
	}
	if key := r.SelfRelationKey(); key != "" {
		row["parent"] = ops.ReadFirstRelationID(propsOut[key])
	}
	printTaskTable([]map[string]string{row})
	return nil
}

func taskListFriendly(cmd *cobra.Command, args []string) error {
	logger := log.Default()
	if flagDebug {
		logger.SetFlags(log.LstdFlags | log.Lshortfile)
	}
	client := newNotionClient()
	r := ops.NewRunner(client, logger)
	r.SetDBID(normalizeID(flagDBID))
	ctx := context.Background()
	if err := r.FetchDB(ctx); err != nil {
		return err
	}

	// build filter
	var and []any
	if fListGoal != "" && r.HasProp("Goals", "relation") {
		pid := resolveGoalID(ctx, r, fListGoal)
		if pid == "" {
			pid = normalizeID(fListGoal)
		}
		if isUUIDStr(pid) {
			and = append(and, map[string]any{"property": "Goals", "relation": map[string]any{"contains": pid}})
		}
	}
	if fListStatus != "" && r.StatusKey() != "" {
		and = append(and, map[string]any{"property": r.StatusKey(), "status": map[string]any{"equals": fListStatus}})
	}
	if fListParent != "" && r.SelfRelationKey() != "" {
		pid := normalizeID(fListParent)
		if !isUUIDStr(pid) {
			pid = r.FirstByTitle(ctx, fListParent)
		}
		if isUUIDStr(pid) {
			and = append(and, map[string]any{"property": r.SelfRelationKey(), "relation": map[string]any{"contains": pid}})
		}
	}
	var filter map[string]any
	if len(and) > 0 {
		filter = map[string]any{"and": and}
	}
	body, _ := ops.MarshalQuery(filter, nil, 100, "")
	b, code, err := r.Client().QueryDatabase(r.TitleDB(), body)
	if err != nil || code/100 != 2 {
		return fmt.Errorf("query: %d %v %s", code, err, string(b))
	}
	if flagJSON {
		fmt.Println(string(b))
		return nil
	}
	// parse
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	results, _ := m["results"].([]any)
	rows := make([]map[string]string, 0, len(results))
	for _, it := range results {
		row := it.(map[string]any)
		id, _ := row["id"].(string)
		props, _ := row["properties"].(map[string]any)
		out := map[string]string{"id": id, "title": ops.ReadTitle(props[r.TitleKey()])}
		if r.StatusKey() != "" {
			out["status"] = ops.ReadStatus(props[r.StatusKey()])
		}
		if r.HasProp("Do", "date") {
			out["do"] = ops.ReadDateStart(props["Do"])
		}
		if r.HasProp("Due", "date") {
			out["due"] = ops.ReadDateStart(props["Due"])
		}
		if r.SelfRelationKey() != "" {
			out["parent"] = ops.ReadFirstRelationID(props[r.SelfRelationKey()])
		}
		rows = append(rows, out)
	}
	// stable sort by title
	sort.Slice(rows, func(i, j int) bool { return rows[i]["title"] < rows[j]["title"] })
	if flagJSON {
		b, _ := json.Marshal(rows)
		fmt.Println(string(b))
		return nil
	}
	printTaskTable(rows)
	return nil
}
