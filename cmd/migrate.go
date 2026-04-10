package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/spacetj/noops/internal/ops"
	"github.com/spf13/cobra"
)

var (
	migrateGoal     string
	migrateFromProp string
	migrateClear    bool
	migrateDryRun   bool
)

func init() {
	mg := &cobra.Command{Use: "migrate-notes", Short: "Move rich_text property content to page notes (block children)", RunE: runMigrateNotes}
	rootCmd.AddCommand(mg)
	mg.Flags().StringVar(&migrateGoal, "goal", "", "Goal page title or ID/URL to scope migration (required)")
	mg.Flags().StringVar(&migrateFromProp, "from-prop", "Summary", "Rich text property name to migrate (e.g., 'Summary' or 'Description')")
	mg.Flags().BoolVar(&migrateClear, "clear", true, "Clear the source property after appending notes")
	mg.Flags().BoolVar(&migrateDryRun, "dry-run", false, "Only report what would change without writing")
}

func runMigrateNotes(cmd *cobra.Command, args []string) error {
	if migrateGoal == "" {
		return fmt.Errorf("--goal is required")
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

	// Resolve goal id filter
	pid := resolveGoalID(ctx, r, migrateGoal)
	if pid == "" {
		pid = normalizeID(migrateGoal)
	}
	if pid == "" {
		return fmt.Errorf("unable to resolve --goal to a page id")
	}

	// Build filter: Goals relation contains pid; if schema allows, also from-prop is_not_empty
	and := []any{map[string]any{"property": "Goals", "relation": map[string]any{"contains": pid}}}
	if r.HasProp(migrateFromProp, "rich_text") {
		and = append(and, map[string]any{"property": migrateFromProp, "rich_text": map[string]any{"is_not_empty": true}})
	}
	filter := map[string]any{"and": and}

	migrated := []map[string]any{}
	cursor := ""
	for i := 0; i < 200; i++ { // safety cap
		body, _ := ops.MarshalQuery(filter, nil, 100, cursor)
		b, code, err := r.Client().QueryDatabase(r.TitleDB(), body)
		if err != nil || code/100 != 2 {
			return fmt.Errorf("query: %d %v %s", code, err, string(b))
		}
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		results, _ := m["results"].([]any)
		for _, it := range results {
			row := it.(map[string]any)
			id, _ := row["id"].(string)
			// Fetch page for properties
			pb, pcode, perr := r.Client().GetPage(id)
			if perr != nil || pcode/100 != 2 {
				return fmt.Errorf("get page: %d %v", pcode, perr)
			}
			var pm map[string]any
			_ = json.Unmarshal(pb, &pm)
			props, _ := pm["properties"].(map[string]any)
			src, _ := props[migrateFromProp].(map[string]any)
			if src == nil {
				continue
			}
			rich, _ := src["rich_text"].([]any)
			if len(rich) == 0 {
				continue
			}
			// Extract plain text segments
			text := ""
			for _, seg := range rich {
				sm, _ := seg.(map[string]any)
				text += fmt.Sprint(sm["plain_text"]) // Notion includes plain_text
			}
			if migrateDryRun {
				migrated = append(migrated, map[string]any{"id": id, "movedChars": len(text)})
				continue
			}
			// Append as page notes
			if err := r.AppendPageNotes(ctx, id, text); err != nil {
				return err
			}
			// Clear property if requested
			if migrateClear && r.HasProp(migrateFromProp, "rich_text") {
				clear := map[string]any{"properties": map[string]any{migrateFromProp: map[string]any{"rich_text": []any{}}}}
				cb, _ := json.Marshal(clear)
				if _, ccode, cerr := r.Client().UpdatePage(id, cb); cerr != nil || ccode/100 != 2 {
					return fmt.Errorf("clear prop: %d %v", ccode, cerr)
				}
			}
			migrated = append(migrated, map[string]any{"id": id, "movedChars": len(text)})
		}
		if hv, _ := m["has_more"].(bool); !hv {
			break
		}
		cursor, _ = m["next_cursor"].(string)
		if cursor == "" {
			break
		}
	}

	if flagJSON {
		out := map[string]any{"ok": true, "count": len(migrated), "migrated": migrated}
		jb, _ := json.Marshal(out)
		fmt.Println(string(jb))
		return nil
	}
	fmt.Printf("Migrated notes for %d page(s)\n", len(migrated))
	return nil
}
