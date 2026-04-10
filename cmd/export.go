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
	exportGoal    string
	exportWhere   []string
	exportSort    []string
	exportOut     string
	exportPage    int
	exportCompact bool
)

func init() {
	ex := &cobra.Command{Use: "export", Short: "Export tasks snapshot for backup (JSON)", RunE: runExport}
	rootCmd.AddCommand(ex)
	ex.Flags().StringVar(&exportGoal, "goal", "", "Goal page title or ID/URL to filter (optional)")
	ex.Flags().StringArrayVar(&exportWhere, "where", nil, "Additional filter DSL specs to AND (optional)")
	ex.Flags().StringArrayVar(&exportSort, "sort", nil, "Sort specs e.g. 'Due:desc'")
	ex.Flags().StringVar(&exportOut, "out", "", "Output file path (default: stdout)")
	ex.Flags().IntVar(&exportPage, "page-size", 100, "Page size per request (1-100)")
	ex.Flags().BoolVar(&exportCompact, "compact", false, "Output normalized rows [ {id,title,status,do,due,parent}, ... ]")
}

func runExport(cmd *cobra.Command, args []string) error {
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

	// Build filter: optional goal AND custom where clauses
	var mapped []string
	for _, w := range exportWhere {
		if w == "" {
			continue
		}
		dot := strings.IndexByte(w, '.')
		if dot > 0 {
			left, right := w[:dot], w[dot+1:]
			orig := left
			if i := strings.IndexByte(left, ':'); i >= 0 {
				orig = left[:i]
			}
			name := orig
			if strings.EqualFold(orig, "title") || strings.EqualFold(orig, "name") {
				name = r.TitleKey()
			}
			leftSuf := left[len(orig):]
			mapped = append(mapped, name+leftSuf+"."+right)
		} else {
			mapped = append(mapped, w)
		}
	}
	filter, err := ops.BuildFilter(mapped)
	if err != nil {
		return err
	}
	var and []any
	if filter != nil {
		if parts, ok := filter["and"].([]any); ok {
			and = append(and, parts...)
		}
	}
	if exportGoal != "" && r.HasProp("Goals", "relation") {
		pid := resolveGoalID(ctx, r, exportGoal)
		if pid == "" {
			pid = normalizeID(exportGoal)
		}
		if isUUIDStr(pid) {
			and = append(and, map[string]any{"property": "Goals", "relation": map[string]any{"contains": pid}})
		}
	}
	if len(and) > 0 {
		filter = map[string]any{"and": and}
	}
	sorts, err := ops.BuildSorts(exportSort)
	if err != nil {
		return err
	}

	// Paginate and collect all results
	cursor := ""
	var all []any
	for i := 0; i < 200; i++ { // hard cap safety
		body, _ := ops.MarshalQuery(filter, sorts, exportPage, cursor)
		b, code, err := r.Client().QueryDatabase(r.TitleDB(), body)
		if err != nil || code/100 != 2 {
			return fmt.Errorf("query: %d %v %s", code, err, string(b))
		}
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		results, _ := m["results"].([]any)
		all = append(all, results...)
		if more, _ := m["has_more"].(bool); !more {
			break
		}
		cursor, _ = m["next_cursor"].(string)
		if cursor == "" {
			break
		}
	}

	// Format output
	var out []byte
	if exportCompact {
		rows := make([]map[string]any, 0, len(all))
		for _, it := range all {
			row := it.(map[string]any)
			id, _ := row["id"].(string)
			props, _ := row["properties"].(map[string]any)
			one := map[string]any{"id": id, "title": ops.ReadTitle(props[r.TitleKey()])}
			if r.StatusKey() != "" {
				one["status"] = ops.ReadStatus(props[r.StatusKey()])
			} else {
				one["status"] = nil
			}
			if r.HasProp("Do", "date") {
				one["do"] = ops.ReadDateStart(props["Do"])
			} else {
				one["do"] = nil
			}
			if r.HasProp("Due", "date") {
				one["due"] = ops.ReadDateStart(props["Due"])
			} else {
				one["due"] = nil
			}
			if r.SelfRelationKey() != "" {
				one["parent"] = ops.ReadFirstRelationID(props[r.SelfRelationKey()])
			} else {
				one["parent"] = nil
			}
			rows = append(rows, one)
		}
		out, _ = json.Marshal(rows)
	} else {
		// Raw Notion JSON pages
		out, _ = json.Marshal(all)
	}

	if exportOut == "" {
		fmt.Println(string(out))
		return nil
	}
	return os.WriteFile(exportOut, out, 0o600)
}
