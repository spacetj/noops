package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spacetj/noops/internal/ops"
	"github.com/spf13/cobra"
)

var (
	listWhere   []string
	listSort    []string
	listPage    int
	listCursor  string
	listCompact bool
	// human-friendly filters
	fNameContains string
	fNameEquals   string
	fStatusEq     string
	fPriorityEq   string
	fDueBefore    string
	fDueAfter     string
	fDoBefore     string
	fDoAfter      string
	fGoalIn       string
)

func init() {
	listCmd := &cobra.Command{Use: "list", Short: "List/search tasks with flexible filters", RunE: taskList}
	rootCmd.AddCommand(listCmd)
	listCmd.Flags().StringArrayVar(&listWhere, "where", nil, "Filter DSL e.g. 'Title:title.contains=foo', 'Status:status.equals=In Progress', 'Due:date.on_or_before=2025-10-31'")
	listCmd.Flags().StringArrayVar(&listSort, "sort", nil, "Sort spec 'Prop[:type]:asc|desc' e.g. 'Due:desc'")
	listCmd.Flags().IntVar(&listPage, "page-size", 50, "Page size (1-100)")
	listCmd.Flags().StringVar(&listCursor, "cursor", "", "Pagination cursor")
	listCmd.Flags().BoolVar(&listCompact, "compact", false, "Output normalized rows [ {id,title,status,do,due,parent}, ... ] (JSON)")
	// human-friendly filters
	listCmd.Flags().StringVar(&fNameContains, "name", "", "Filter: title contains substring")
	listCmd.Flags().StringVar(&fNameEquals, "name-equals", "", "Filter: title equals (exact)")
	listCmd.Flags().StringVar(&fStatusEq, "status", "", "Filter: status equals (if DB has status)")
	listCmd.Flags().StringVar(&fPriorityEq, "priority", "", "Filter: priority select equals (if present)")
	listCmd.Flags().StringVar(&fDueBefore, "due-before", "", "Filter: Due on_or_before (YYYY-MM-DD)")
	listCmd.Flags().StringVar(&fDueAfter, "due-after", "", "Filter: Due on_or_after (YYYY-MM-DD)")
	listCmd.Flags().StringVar(&fDoBefore, "do-before", "", "Filter: Do on_or_before (YYYY-MM-DD)")
	listCmd.Flags().StringVar(&fDoAfter, "do-after", "", "Filter: Do on_or_after (YYYY-MM-DD)")
	listCmd.Flags().StringVar(&fGoalIn, "goal-filter", "", "Filter: Goals relation contains the given page URL or ID")
}

func taskList(cmd *cobra.Command, args []string) error {
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
	// Map common aliases to actual property keys
	mapped := make([]string, 0, len(listWhere))
	for _, w := range listWhere {
		if w == "" {
			continue
		}
		dot := strings.IndexByte(w, '.')
		if dot > 0 {
			left, right := w[:dot], w[dot+1:]
			origName := left
			if i := strings.IndexByte(left, ':'); i >= 0 {
				origName = left[:i]
			}
			name := origName
			if eqFold(origName, "title") || eqFold(origName, "name") {
				name = r.TitleKey()
			}
			leftSuffix := left[len(origName):]
			mapped = append(mapped, name+leftSuffix+"."+right)
		} else {
			mapped = append(mapped, w)
		}
	}
	filter, err := ops.BuildFilter(mapped)
	if err != nil {
		return err
	}
	// append human-friendly filters as AND parts
	var andParts []any
	if filter != nil {
		if parts, ok := filter["and"].([]any); ok {
			andParts = append(andParts, parts...)
		}
	}
	// name/title filters
	if fNameContains != "" {
		andParts = append(andParts, map[string]any{"property": r.TitleKey(), "title": map[string]any{"contains": fNameContains}})
	}
	if fNameEquals != "" {
		andParts = append(andParts, map[string]any{"property": r.TitleKey(), "title": map[string]any{"equals": fNameEquals}})
	}
	// status
	if fStatusEq != "" && r.StatusKey() != "" {
		andParts = append(andParts, map[string]any{"property": r.StatusKey(), "status": map[string]any{"equals": fStatusEq}})
	}
	// priority (select)
	if fPriorityEq != "" && r.HasProp("Priority", "select") {
		andParts = append(andParts, map[string]any{"property": "Priority", "select": map[string]any{"equals": fPriorityEq}})
	}
	// dates
	if fDueBefore != "" && r.HasProp("Due", "date") {
		andParts = append(andParts, map[string]any{"property": "Due", "date": map[string]any{"on_or_before": fDueBefore}})
	}
	if fDueAfter != "" && r.HasProp("Due", "date") {
		andParts = append(andParts, map[string]any{"property": "Due", "date": map[string]any{"on_or_after": fDueAfter}})
	}
	if fDoBefore != "" && r.HasProp("Do", "date") {
		andParts = append(andParts, map[string]any{"property": "Do", "date": map[string]any{"on_or_before": fDoBefore}})
	}
	if fDoAfter != "" && r.HasProp("Do", "date") {
		andParts = append(andParts, map[string]any{"property": "Do", "date": map[string]any{"on_or_after": fDoAfter}})
	}
	// goal relation contains
	if fGoalIn != "" && r.HasProp("Goals", "relation") {
		andParts = append(andParts, map[string]any{"property": "Goals", "relation": map[string]any{"contains": normalizeID(fGoalIn)}})
	}
	if len(andParts) > 0 {
		filter = map[string]any{"and": andParts}
	}
	sorts, err := ops.BuildSorts(listSort)
	if err != nil {
		return err
	}
	body, err := ops.MarshalQuery(filter, sorts, listPage, listCursor)
	if err != nil {
		return err
	}
	b, code, err := r.Client().QueryDatabase(r.TitleDB(), body)
	if err != nil || code/100 != 2 {
		return fmt.Errorf("query: %d %v %s", code, err, string(b))
	}
	if flagJSON && listCompact {
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		results, _ := m["results"].([]any)
		rows := make([]map[string]any, 0, len(results))
		for _, it := range results {
			row := it.(map[string]any)
			id, _ := row["id"].(string)
			props, _ := row["properties"].(map[string]any)
			out := map[string]any{"id": id, "title": ops.ReadTitle(props[r.TitleKey()])}
			if r.StatusKey() != "" {
				out["status"] = ops.ReadStatus(props[r.StatusKey()])
			} else {
				out["status"] = nil
			}
			if r.HasProp("Do", "date") {
				out["do"] = ops.ReadDateStart(props["Do"])
			} else {
				out["do"] = nil
			}
			if r.HasProp("Due", "date") {
				out["due"] = ops.ReadDateStart(props["Due"])
			} else {
				out["due"] = nil
			}
			if r.SelfRelationKey() != "" {
				out["parent"] = ops.ReadFirstRelationID(props[r.SelfRelationKey()])
			} else {
				out["parent"] = nil
			}
			rows = append(rows, out)
		}
		ob, _ := json.Marshal(rows)
		fmt.Println(string(ob))
		return nil
	}
	if flagJSON {
		fmt.Println(string(b))
		return nil
	}
	// Pretty-print minimal info when not JSON
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	results, _ := m["results"].([]any)
	for _, it := range results {
		row := it.(map[string]any)
		id, _ := row["id"].(string)
		props, _ := row["properties"].(map[string]any)
		var title string
		if props != nil {
			title = ops.ReadTitle(props[r.TitleKey()])
		}
		fmt.Fprintf(os.Stdout, "%s\t%s\n", id, title)
	}
	return nil
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
func eqFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca = ca + 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb = cb + 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}
