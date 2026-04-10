package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/spacetj/noops/internal/ops"
	"github.com/spf13/cobra"
)

var (
	goalInfoName string
	goalInfoID   string

	goalCreateTitle  string
	goalCreateStatus string
	goalCreateEmoji  string

	goalUpdateID     string
	goalUpdateTitle  string
	goalUpdateRename string
	goalUpdateStatus string
	goalUpdateEmoji  string

	goalDeleteID    string
	goalDeleteTitle string
)

func init() {
	goalCmd := &cobra.Command{Use: "goal", Short: "Goal helpers (via Goals relation)"}
	rootCmd.AddCommand(goalCmd)

	gl := &cobra.Command{Use: "list", Short: "List distinct related goals", RunE: goalList}
	goalCmd.AddCommand(gl)

	gi := &cobra.Command{Use: "info", Short: "Show goal info by name or ID", RunE: goalInfo}
	gi.Flags().StringVar(&goalInfoName, "name", "", "Goal page title (exact match)")
	gi.Flags().StringVar(&goalInfoID, "id", "", "Goal page ID or URL")
	goalCmd.AddCommand(gi)

	gc := &cobra.Command{Use: "create", Short: "Create a goal page", RunE: goalCreate}
	gc.Flags().StringVar(&goalCreateTitle, "title", "", "Goal title (required)")
	gc.Flags().StringVar(&goalCreateStatus, "status", "", "Goal status name (optional)")
	gc.Flags().StringVar(&goalCreateEmoji, "emoji", "", "Emoji icon for the goal page (optional)")
	goalCmd.AddCommand(gc)

	gu := &cobra.Command{Use: "update", Short: "Update a goal page", RunE: goalUpdate}
	gu.Flags().StringVar(&goalUpdateID, "id", "", "Goal ID or URL")
	gu.Flags().StringVar(&goalUpdateTitle, "title", "", "Find goal by title when --id not provided")
	gu.Flags().StringVar(&goalUpdateRename, "rename", "", "Rename goal title")
	gu.Flags().StringVar(&goalUpdateStatus, "status", "", "Set goal status name")
	gu.Flags().StringVar(&goalUpdateEmoji, "emoji", "", "Set page emoji icon")
	goalCmd.AddCommand(gu)

	gd := &cobra.Command{Use: "delete", Short: "Archive (delete) a goal", RunE: goalDelete}
	gd.Flags().StringVar(&goalDeleteID, "id", "", "Goal ID or URL")
	gd.Flags().StringVar(&goalDeleteTitle, "title", "", "Goal title if --id not provided")
	goalCmd.AddCommand(gd)
}

func goalList(cmd *cobra.Command, args []string) error {
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
	if !r.HasProp("Goals", "relation") {
		return fmt.Errorf("database has no 'Goals' relation")
	}

	filter := map[string]any{"and": []any{map[string]any{"property": "Goals", "relation": map[string]any{"is_not_empty": true}}}}
	body, _ := ops.MarshalQuery(filter, nil, 100, "")
	b, code, err := r.Client().QueryDatabase(r.TitleDB(), body)
	if err != nil || code/100 != 2 {
		return fmt.Errorf("query: %d %v %s", code, err, string(b))
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	results, _ := m["results"].([]any)
	uniq := map[string]struct{}{}
	ids := []string{}
	for _, it := range results {
		props, _ := it.(map[string]any)["properties"].(map[string]any)
		arr, _ := props["Goals"].(map[string]any)["relation"].([]any)
		for _, ri := range arr {
			id := ri.(map[string]any)["id"].(string)
			if _, ok := uniq[id]; !ok {
				uniq[id] = struct{}{}
				ids = append(ids, id)
			}
		}
	}
	rows := make([][2]string, 0, len(ids))
	for _, id := range ids {
		pb, pcode, _ := r.Client().GetPage(id)
		if pcode/100 != 2 {
			continue
		}
		var pm map[string]any
		_ = json.Unmarshal(pb, &pm)
		props, _ := pm["properties"].(map[string]any)
		title := ops.ReadTitle(props[r.TitleKey()])
		rows = append(rows, [2]string{id, title})
	}
	sort.Slice(rows, func(i, j int) bool { return strings.ToLower(rows[i][1]) < strings.ToLower(rows[j][1]) })
	if flagJSON {
		out := make([]map[string]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, map[string]string{"id": r[0], "title": r[1]})
		}
		b, _ := json.Marshal(out)
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("%-38s  %s\n", "Goal ID", "Title")
	fmt.Printf("%-38s  %s\n", strings.Repeat("-", 38), strings.Repeat("-", 32))
	for _, r := range rows {
		fmt.Printf("%-38s  %s\n", r[0], r[1])
	}
	return nil
}

func goalInfo(cmd *cobra.Command, args []string) error {
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
	if goalInfoID == "" && goalInfoName == "" {
		return fmt.Errorf("provide --id or --name")
	}
	id := normalizeID(goalInfoID)
	if id == "" && goalInfoName != "" {
		id = resolveGoalID(ctx, r, goalInfoName)
	}
	if id == "" {
		return fmt.Errorf("goal not found")
	}
	b, code, err := r.Client().GetPage(id)
	if err != nil || code/100 != 2 {
		return fmt.Errorf("get page: %d %v %s", code, err, string(b))
	}
	if flagJSON {
		fmt.Println(string(b))
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	props, _ := m["properties"].(map[string]any)
	title := ops.ReadTitle(props[r.TitleKey()])
	fmt.Printf("ID: %s\nTitle: %s\n", id, title)
	return nil
}

func goalCreate(cmd *cobra.Command, args []string) error {
	if strings.TrimSpace(goalCreateTitle) == "" {
		return fmt.Errorf("--title is required")
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
	id, err := r.CreateGoal(ctx, goalCreateTitle, goalCreateStatus, goalCreateEmoji)
	if err != nil {
		return err
	}
	if flagJSON {
		resp := map[string]any{"ok": true, "id": id}
		b, _ := json.Marshal(resp)
		fmt.Println(string(b))
		return nil
	}
	fmt.Println("created goal", id)
	return nil
}

func goalUpdate(cmd *cobra.Command, args []string) error {
	if goalUpdateID == "" && goalUpdateTitle == "" {
		return fmt.Errorf("provide --id or --title")
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
	id := normalizeID(goalUpdateID)
	if id == "" && goalUpdateTitle != "" {
		id = r.GoalFirstByTitle(ctx, goalUpdateTitle)
	}
	if id == "" {
		return fmt.Errorf("goal not found")
	}
	if err := r.UpdateGoal(ctx, id, goalUpdateRename, goalUpdateStatus, goalUpdateEmoji); err != nil {
		return err
	}
	if flagJSON {
		fmt.Println(`{"ok":true}`)
	} else {
		fmt.Println("updated goal", id)
	}
	return nil
}

func goalDelete(cmd *cobra.Command, args []string) error {
	if goalDeleteID == "" && goalDeleteTitle == "" {
		return fmt.Errorf("provide --id or --title")
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
	id := normalizeID(goalDeleteID)
	if id == "" && goalDeleteTitle != "" {
		id = r.GoalFirstByTitle(ctx, goalDeleteTitle)
	}
	if id == "" {
		return fmt.Errorf("goal not found")
	}
	if err := r.ArchiveGoal(ctx, id); err != nil {
		return err
	}
	if flagJSON {
		fmt.Println(`{"ok":true}`)
	} else {
		fmt.Println("archived goal", id)
	}
	return nil
}
