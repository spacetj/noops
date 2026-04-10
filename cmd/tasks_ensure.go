package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/spacetj/noops/internal/ops"
	"github.com/spf13/cobra"
)

type ensureItem struct {
	Title    string         `json:"title"`
	Do       string         `json:"do"`
	Due      string         `json:"due"`
	Priority string         `json:"priority"`
	Status   string         `json:"status"`
	Emoji    string         `json:"emoji"`
	Desc     string         `json:"desc"`
	Parent   string         `json:"parent"`
	Goal     string         `json:"goal"`
	Props    map[string]any `json:"props"`
}

type dedupeMode string

const (
	dedupeKeepFirst dedupeMode = "keep-first"
	dedupeKeepLast  dedupeMode = "keep-last"
	dedupeNone      dedupeMode = "none"
)

var (
	ensureFromJSON       string
	ensureGoalArg        string
	ensureUpdateExisting bool
	ensureDedupe         string
)

func init() {
	tasksCmd := &cobra.Command{Use: "tasks", Short: "Batch task operations"}
	// attach if not already present
	var found bool
	for _, c := range rootCmd.Commands() {
		if c.Use == "tasks" {
			found = true
			tasksCmd = c
			break
		}
	}
	if !found {
		rootCmd.AddCommand(tasksCmd)
	}

	ensureCmd := &cobra.Command{Use: "ensure", Short: "Ensure a set of tasks exist (create/update/dedupe)", RunE: runTasksEnsure}
	ensureCmd.Flags().StringVar(&ensureGoalArg, "goal", "", "Goal page URL/ID or exact title (required)")
	ensureCmd.Flags().StringVar(&ensureFromJSON, "from-json", "", "JSON file path or '-' for STDIN (required)")
	ensureCmd.Flags().BoolVar(&ensureUpdateExisting, "update-existing", false, "Overwrite fields provided in JSON for existing tasks")
	ensureCmd.Flags().StringVar(&ensureDedupe, "dedupe", string(dedupeKeepFirst), "Duplicate handling: keep-first|keep-last|none")
	tasksCmd.AddCommand(ensureCmd)
}

func runTasksEnsure(cmd *cobra.Command, args []string) error {
	if ensureGoalArg == "" {
		return fmt.Errorf("--goal is required")
	}
	if ensureFromJSON == "" {
		return fmt.Errorf("--from-json is required (file path or '-')")
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

	// resolve base goal id
	goalID := resolveGoalID(ctx, r, ensureGoalArg)
	if goalID == "" {
		goalID = normalizeID(ensureGoalArg)
	}
	if goalID == "" {
		return fmt.Errorf("unable to resolve --goal to a page id")
	}

	// load items
	items, err := loadEnsureItems(ensureFromJSON)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return fmt.Errorf("no tasks provided in JSON")
	}

	mode := dedupeMode(ensureDedupe)
	if mode != dedupeKeepFirst && mode != dedupeKeepLast && mode != dedupeNone {
		return fmt.Errorf("invalid --dedupe: %s", ensureDedupe)
	}

	type upd struct{ ID, Title string }
	created := []upd{}
	updated := []upd{}
	skipped := []map[string]string{}
	deduped := []map[string]any{}

	for _, it := range items {
		if strings.TrimSpace(it.Title) == "" {
			skipped = append(skipped, map[string]string{"title": "", "reason": "missing-title"})
			continue
		}
		itemGoal := goalID
		if it.Goal != "" {
			if gid := resolveGoalID(ctx, r, it.Goal); gid != "" {
				itemGoal = gid
			} else {
				itemGoal = normalizeID(it.Goal)
			}
		}
		// find existing by exact title within goal
		ids, err := findByTitleInGoal(ctx, r, it.Title, itemGoal)
		if err != nil {
			return err
		}
		// dedupe if multiple
		if len(ids) > 1 && mode != dedupeNone {
			keepIdx := 0
			if mode == dedupeKeepLast {
				keepIdx = len(ids) - 1
			}
			keep := ids[keepIdx]
			var archived []string
			for i, id := range ids {
				if i == keepIdx {
					continue
				}
				_ = r.ArchivePage(ctx, id)
				archived = append(archived, id)
			}
			deduped = append(deduped, map[string]any{"kept": keep, "archived": archived})
			ids = []string{keep}
		}

		if len(ids) == 0 {
			// create
			props := map[string]any{}
			if itemGoal != "" && r.HasProp("Goals", "relation") {
				props["Goals"] = map[string]any{"relation": []any{map[string]any{"id": itemGoal}}}
			}
			addBasicProps(r, props, it)
			// parent relation: accept URL/ID; otherwise resolve by exact title
			if it.Parent != "" && r.SelfRelationKey() != "" {
				pid := normalizeID(it.Parent)
				if !isUUID(pid) {
					pid = ""
				}
				if pid == "" {
					pid = r.FirstByTitle(ctx, it.Parent)
				}
				if pid != "" {
					props[r.SelfRelationKey()] = map[string]any{"relation": []any{map[string]any{"id": pid}}}
				}
			}
			// extra props
			if len(it.Props) > 0 {
				applyExtraProps(r, props, it.Props)
			}
			id, err := r.CreateWithPropsEmoji(ctx, it.Title, props, it.Emoji)
			if err != nil {
				return err
			}
			created = append(created, upd{ID: id, Title: it.Title})
			if strings.TrimSpace(it.Desc) != "" {
				_ = r.AppendPageNotes(ctx, id, it.Desc)
			}
			continue
		}

		// update existing (ids[0])
		id := ids[0]
		// fetch page to inspect current values
		pb, code, err := r.Client().GetPage(id)
		if err != nil || code/100 != 2 {
			return fmt.Errorf("get page: %d %v %s", code, err, string(pb))
		}
		if strings.TrimSpace(it.Desc) != "" {
			_ = r.AppendPageNotes(ctx, id, it.Desc)
		}
		var page map[string]any
		_ = json.Unmarshal(pb, &page)
		pmap, _ := page["properties"].(map[string]any)

		props := map[string]any{}
		// Title is not overwritten by default; skip unless update-existing and provided
		if ensureUpdateExisting && it.Title != "" {
			props[r.TitleKey()] = ops.TitleProp(it.Title)
		}
		// Goal relation: set if empty or update-existing
		if itemGoal != "" && r.HasProp("Goals", "relation") {
			if ensureUpdateExisting || relationEmpty(pmap["Goals"]) {
				props["Goals"] = map[string]any{"relation": []any{map[string]any{"id": itemGoal}}}
			}
		}
		// basic fields
		addBasicPropsConditional(r, props, pmap, it, ensureUpdateExisting)
		// parent: accept URL/ID; otherwise resolve by exact title
		if it.Parent != "" && r.SelfRelationKey() != "" {
			key := r.SelfRelationKey()
			if ensureUpdateExisting || relationEmpty(pmap[key]) {
				pid := normalizeID(it.Parent)
				if !isUUID(pid) {
					pid = ""
				}
				if pid == "" {
					pid = r.FirstByTitle(ctx, it.Parent)
				}
				if pid != "" {
					props[key] = map[string]any{"relation": []any{map[string]any{"id": pid}}}
				}
			}
		}
		// extra props
		if len(it.Props) > 0 {
			applyExtraPropsConditional(r, props, pmap, it.Props, ensureUpdateExisting)
		}
		// emoji
		if it.Emoji != "" {
			_ = r.ApplyEmoji(ctx, id, it.Emoji)
		}

		if len(props) == 0 {
			skipped = append(skipped, map[string]string{"title": it.Title, "reason": "exists-and-complete"})
		} else {
			if err := r.ApplyProps(ctx, id, props); err != nil {
				return err
			}
			updated = append(updated, upd{ID: id, Title: it.Title})
		}
	}

	if flagJSON {
		out := map[string]any{"ok": true, "goalId": goalID, "created": created, "updated": updated, "skipped": skipped, "deduped": deduped}
		b, _ := json.Marshal(out)
		fmt.Println(string(b))
		return nil
	}
	// table output
	rows := []map[string]string{}
	for _, c := range created {
		rows = append(rows, map[string]string{"id": c.ID, "title": c.Title, "status": "created", "due": "", "parent": ""})
	}
	for _, u := range updated {
		rows = append(rows, map[string]string{"id": u.ID, "title": u.Title, "status": "updated", "due": "", "parent": ""})
	}
	// sort by title
	sort.Slice(rows, func(i, j int) bool { return rows[i]["title"] < rows[j]["title"] })
	printTaskTable(rows)
	if len(skipped) > 0 {
		fmt.Printf("Skipped %d item(s).\n", len(skipped))
	}
	if len(deduped) > 0 {
		fmt.Printf("Deduped %d title(s).\n", len(deduped))
	}
	return nil
}

func loadEnsureItems(path string) ([]ensureItem, error) {
	var r io.Reader
	if path == "-" || path == "" {
		r = bufio.NewReader(os.Stdin)
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var arr []ensureItem
	if err := json.Unmarshal(b, &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

func findByTitleInGoal(ctx context.Context, r *ops.Runner, title, goalID string) ([]string, error) {
	// Build filter: Title equals AND Goals relation contains goalID
	filter := map[string]any{"and": []any{
		map[string]any{"property": r.TitleKey(), "title": map[string]any{"equals": title}},
	}}
	if goalID != "" && r.HasProp("Goals", "relation") {
		and := filter["and"].([]any)
		and = append(and, map[string]any{"property": "Goals", "relation": map[string]any{"contains": goalID}})
		filter["and"] = and
	}
	body, _ := ops.MarshalQuery(filter, nil, 100, "")
	b, code, err := r.Client().QueryDatabase(r.TitleDB(), body)
	if err != nil || code/100 != 2 {
		return nil, fmt.Errorf("query: %d %v %s", code, err, string(b))
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	results, _ := m["results"].([]any)
	ids := make([]string, 0, len(results))
	for _, it := range results {
		row := it.(map[string]any)
		ids = append(ids, row["id"].(string))
	}
	return ids, nil
}

func relationEmpty(v any) bool {
	m, _ := v.(map[string]any)
	if m == nil {
		return true
	}
	arr, _ := m["relation"].([]any)
	return len(arr) == 0
}

func dateEmpty(v any) bool {
	m, _ := v.(map[string]any)
	if m == nil {
		return true
	}
	d, _ := m["date"].(map[string]any)
	if d == nil {
		return true
	}
	s, _ := d["start"].(string)
	return strings.TrimSpace(s) == ""
}

func selectEmpty(v any, key string) bool {
	m, _ := v.(map[string]any)
	if m == nil {
		return true
	}
	s, _ := m[key].(map[string]any)
	if s == nil {
		return true
	}
	name, _ := s["name"].(string)
	return strings.TrimSpace(name) == ""
}

func addBasicProps(r *ops.Runner, props map[string]any, it ensureItem) {
	if r.HasProp("Do", "date") && it.Do != "" {
		props["Do"] = map[string]any{"date": map[string]any{"start": it.Do}}
	}
	if r.HasProp("Due", "date") && it.Due != "" {
		props["Due"] = map[string]any{"date": map[string]any{"start": it.Due}}
	}
	if r.HasProp("Priority", "select") && it.Priority != "" {
		props["Priority"] = map[string]any{"select": map[string]any{"name": it.Priority}}
	}
	if r.StatusKey() != "" && it.Status != "" {
		props[r.StatusKey()] = map[string]any{"status": map[string]any{"name": it.Status}}
	}
	if r.StatusKey() == "" && r.HasProp("Status", "select") && it.Status != "" {
		props["Status"] = map[string]any{"select": map[string]any{"name": it.Status}}
	}
	// desc is handled by appending page notes (not a property)
}

func addBasicPropsConditional(r *ops.Runner, props map[string]any, current map[string]any, it ensureItem, overwrite bool) {
	if r.HasProp("Do", "date") && it.Do != "" {
		if overwrite || dateEmpty(current["Do"]) {
			props["Do"] = map[string]any{"date": map[string]any{"start": it.Do}}
		}
	}
	if r.HasProp("Due", "date") && it.Due != "" {
		if overwrite || dateEmpty(current["Due"]) {
			props["Due"] = map[string]any{"date": map[string]any{"start": it.Due}}
		}
	}
	if r.HasProp("Priority", "select") && it.Priority != "" {
		if overwrite || selectEmpty(current["Priority"], "select") {
			props["Priority"] = map[string]any{"select": map[string]any{"name": it.Priority}}
		}
	}
	if it.Status != "" {
		if r.StatusKey() != "" {
			if overwrite || selectEmpty(current[r.StatusKey()], "status") {
				props[r.StatusKey()] = map[string]any{"status": map[string]any{"name": it.Status}}
			}
		} else if r.HasProp("Status", "select") {
			if overwrite || selectEmpty(current["Status"], "select") {
				props["Status"] = map[string]any{"select": map[string]any{"name": it.Status}}
			}
		}
	}
	// desc is handled by appending page notes (not a property)
}

func applyExtraProps(r *ops.Runner, props map[string]any, extra map[string]any) {
	for k, v := range extra {
		switch vv := v.(type) {
		case string:
			// try by schema
			if r.HasProp(k, "date") {
				if strings.TrimSpace(vv) != "" {
					props[k] = map[string]any{"date": map[string]any{"start": vv}}
				}
			} else if r.HasProp(k, "status") {
				props[k] = map[string]any{"status": map[string]any{"name": vv}}
			} else if r.HasProp(k, "select") {
				props[k] = map[string]any{"select": map[string]any{"name": vv}}
			} else if r.HasProp(k, "relation") {
				id := normalizeID(vv)
				if id != "" {
					props[k] = map[string]any{"relation": []any{map[string]any{"id": id}}}
				}
			} else if r.HasProp(k, "rich_text") {
				props[k] = map[string]any{"rich_text": []any{map[string]any{"text": map[string]any{"content": vv}}}}
			}
		case []any:
			if r.HasProp(k, "multi_select") {
				// assume array of strings
				var arr []any
				for _, e := range vv {
					if s, ok := e.(string); ok {
						arr = append(arr, map[string]any{"name": s})
					}
				}
				props[k] = map[string]any{"multi_select": arr}
			} else if r.HasProp(k, "relation") {
				var arr []any
				for _, e := range vv {
					if s, ok := e.(string); ok {
						id := normalizeID(s)
						if id != "" {
							arr = append(arr, map[string]any{"id": id})
						}
					}
				}
				if len(arr) > 0 {
					props[k] = map[string]any{"relation": arr}
				}
			}
		}
	}
}

func applyExtraPropsConditional(r *ops.Runner, props map[string]any, current map[string]any, extra map[string]any, overwrite bool) {
	tmp := map[string]any{}
	applyExtraProps(r, tmp, extra)
	for k, v := range tmp {
		if !overwrite {
			// only set if empty by simple heuristic per type
			if m, ok := v.(map[string]any); ok {
				if _, ok := m["date"]; ok {
					if !dateEmpty(current[k]) {
						continue
					}
				}
				if _, ok := m["select"]; ok {
					if !selectEmpty(current[k], "select") {
						continue
					}
				}
				if _, ok := m["status"]; ok {
					if !selectEmpty(current[k], "status") {
						continue
					}
				}
				if _, ok := m["relation"]; ok {
					if !relationEmpty(current[k]) {
						continue
					}
				}
				if _, ok := m["multi_select"]; ok { /* if already set skip */
					if cur, _ := current[k].(map[string]any); cur != nil {
						if arr, _ := cur["multi_select"].([]any); len(arr) > 0 {
							continue
						}
					}
				}
				if _, ok := m["rich_text"]; ok {
					if cur, _ := current[k].(map[string]any); cur != nil {
						if arr, _ := cur["rich_text"].([]any); len(arr) > 0 {
							continue
						}
					}
				}
			}
		}
		props[k] = v
	}
}
