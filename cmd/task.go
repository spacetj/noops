package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/spacetj/noops/internal/ops"
	"github.com/spf13/cobra"
)

var (
	setProps  []string
	taskID    string
	taskTitle string
	newTitle  string
	taskEmoji string
	taskDesc  string
)

func init() {
	// group (if not already added by friendly commands)
	taskCmd := &cobra.Command{Use: "task", Short: "Task management commands"}
	rootCmd.AddCommand(taskCmd)

	// create
	create := &cobra.Command{Use: "create", Short: "Create a task", RunE: taskCreate}
	create.Flags().StringVar(&taskTitle, "title", "", "Task title (required if no --id)")
	create.Flags().StringArrayVar(&setProps, "set", nil, "Property set spec (e.g., 'Due:date=2025-10-31', 'Priority:select=High', 'Goals:relation=<id/url>')")
	create.Flags().StringVar(&taskEmoji, "emoji", "", "Emoji icon to set on the page (e.g., '📌')")
	taskCmd.AddCommand(create)

	// update
	update := &cobra.Command{Use: "update", Short: "Update a task", RunE: taskUpdate}
	update.Flags().StringVar(&taskID, "id", "", "Task/page ID or URL")
	update.Flags().StringVar(&taskTitle, "title", "", "Find task by title if --id omitted")
	update.Flags().StringVar(&newTitle, "rename", "", "Rename task title")
	update.Flags().StringArrayVar(&setProps, "set", nil, "Property set spec (e.g., 'Status=status=In Progress', 'Do:date=2025-10-01')")
	update.Flags().StringVar(&taskEmoji, "emoji", "", "Set/replace page emoji icon (e.g., '✅')")
	update.Flags().StringVar(&taskDesc, "desc", "", "Set description text if your DB has a 'Description' (or 'Summary') rich_text property")
	taskCmd.AddCommand(update)

	// delete (archive)
	del := &cobra.Command{Use: "delete", Short: "Archive (delete) a task", RunE: taskDelete}
	del.Flags().StringVar(&taskID, "id", "", "Task/page ID or URL")
	del.Flags().StringVar(&taskTitle, "title", "", "Find task by title if --id omitted")
	taskCmd.AddCommand(del)

	// get
	get := &cobra.Command{Use: "get", Short: "Get a task/page", RunE: taskGet}
	get.Flags().StringVar(&taskID, "id", "", "Task/page ID or URL")
	get.Flags().StringVar(&taskTitle, "title", "", "Find task by title if --id omitted")
	taskCmd.AddCommand(get)
}

func buildRunner() (*ops.Runner, error) {
	logger := log.Default()
	if flagDebug {
		logger.SetFlags(log.LstdFlags | log.Lshortfile)
	}
	client := newNotionClient()
	r := ops.NewRunner(client, logger)
	r.SetDBID(normalizeID(flagDBID))
	if flagGoal != "" {
		r.SetGoalPageID(normalizeID(flagGoal))
	}
	return r, nil
}

func taskCreate(cmd *cobra.Command, args []string) error {
	if taskTitle == "" {
		return fmt.Errorf("--title is required")
	}
	r, _ := buildRunner()
	ctx := context.Background()
	if err := r.FetchDB(ctx); err != nil {
		return err
	}
	props, warns, err := r.BuildPropsFromSpecs(setProps)
	if len(warns) > 0 && !flagJSON {
		fmt.Println("WARN:", strings.Join(warns, "; "))
	}
	if err != nil {
		return err
	}
	id, err := r.CreateWithPropsEmoji(ctx, taskTitle, props, taskEmoji)
	if err != nil {
		return err
	}
	if flagJSON {
		out := map[string]any{"ok": true, "id": id}
		b, _ := json.Marshal(out)
		fmt.Println(string(b))
	} else {
		fmt.Println("created", id)
	}
	return nil
}

func taskUpdate(cmd *cobra.Command, args []string) error {
	r, _ := buildRunner()
	ctx := context.Background()
	if err := r.FetchDB(ctx); err != nil {
		return err
	}
	id := normalizeID(taskID)
	if id == "" && taskTitle != "" {
		id = r.FirstByTitle(ctx, taskTitle)
	}
	if id == "" {
		return fmt.Errorf("task not found: specify --id or --title")
	}
	props, warns, err := r.BuildPropsFromSpecs(setProps)
	if len(warns) > 0 && !flagJSON {
		fmt.Println("WARN:", strings.Join(warns, "; "))
	}
	if err != nil {
		return err
	}
	if newTitle != "" {
		props[r.TitleKey()] = ops.TitleProp(newTitle)
	}
	// Apply property updates first
	if err := r.ApplyProps(ctx, id, props); err != nil {
		return err
	}
	if taskEmoji != "" {
		if err := r.ApplyEmoji(ctx, id, taskEmoji); err != nil {
			return err
		}
	}
	// Then append desc as page notes (block children) if provided
	if taskDesc != "" {
		if err := r.AppendPageNotes(ctx, id, taskDesc); err != nil {
			return err
		}
	}
	if flagJSON {
		fmt.Println(`{"ok":true}`)
	} else {
		fmt.Println("updated", id)
	}
	return nil
}

func taskDelete(cmd *cobra.Command, args []string) error {
	r, _ := buildRunner()
	ctx := context.Background()
	if err := r.FetchDB(ctx); err != nil {
		return err
	}
	id := normalizeID(taskID)
	if id == "" && taskTitle != "" {
		id = r.FirstByTitle(ctx, taskTitle)
	}
	if id == "" {
		return fmt.Errorf("task not found: specify --id or --title")
	}
	if err := r.ArchivePage(ctx, id); err != nil {
		return err
	}
	if flagJSON {
		fmt.Println(`{"ok":true}`)
	} else {
		fmt.Println("archived", id)
	}
	return nil
}

func taskGet(cmd *cobra.Command, args []string) error {
	r, _ := buildRunner()
	ctx := context.Background()
	id := normalizeID(taskID)
	if id == "" && taskTitle != "" {
		if err := r.FetchDB(ctx); err == nil {
			id = r.FirstByTitle(ctx, taskTitle)
		}
	}
	if id == "" {
		return fmt.Errorf("task not found: specify --id or --title")
	}
	b, code, err := r.Client().GetPage(id)
	if err != nil || code/100 != 2 {
		return fmt.Errorf("get page: %d %v %s", code, err, string(b))
	}
	if flagJSON {
		fmt.Println(string(b))
	} else {
		fmt.Println(string(b))
	}
	return nil
}
