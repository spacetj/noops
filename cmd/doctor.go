package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/spacetj/noops/internal/ops"
	"github.com/spf13/cobra"
)

func init() {
	dc := &cobra.Command{Use: "doctor", Short: "Check env, DB, goal access, and schema", RunE: runDoctor}
	rootCmd.AddCommand(dc)
}

type check struct{ Name, Status, Detail string }

func runDoctor(cmd *cobra.Command, args []string) error {
	checks := []check{}
	ok := true

	// token present
	if flagToken == "" {
		checks = append(checks, check{"NOTION_TOKEN", "fail", "missing"})
		ok = false
	} else {
		checks = append(checks, check{"NOTION_TOKEN", "ok", "present"})
	}

	logger := log.Default()
	if flagDebug {
		logger.SetFlags(log.LstdFlags | log.Lshortfile)
	}
	client := newNotionClient()
	r := ops.NewRunner(client, logger)
	r.SetDBID(normalizeID(flagDBID))
	ctx := context.Background()

	// DB access
	if err := r.FetchDB(ctx); err != nil {
		checks = append(checks, check{"Database", "fail", err.Error()})
		ok = false
	} else {
		checks = append(checks, check{"Database", "ok", "reachable"})
		// schema
		if r.TitleKey() == "" {
			checks = append(checks, check{"Schema:Title", "warn", "not detected"})
		} else {
			checks = append(checks, check{"Schema:Title", "ok", r.TitleKey()})
		}
		if r.StatusKey() == "" {
			checks = append(checks, check{"Schema:Status", "warn", "no status property"})
		} else {
			checks = append(checks, check{"Schema:Status", "ok", r.StatusKey()})
		}
		if r.SelfRelationKey() == "" {
			checks = append(checks, check{"Schema:SelfRelation", "warn", "no self-relation"})
		} else {
			checks = append(checks, check{"Schema:SelfRelation", "ok", r.SelfRelationKey()})
		}
	}

	// goal page if provided
	if flagGoal != "" {
		id := normalizeID(flagGoal)
		b, code, err := client.GetPage(id)
		if err != nil || code/100 != 2 {
			checks = append(checks, check{"Goal", "fail", fmt.Sprintf("%d %v", code, err)})
			ok = false
		} else {
			_ = b
			checks = append(checks, check{"Goal", "ok", "reachable"})
		}
	}

	if flagJSON {
		out := map[string]any{"ok": ok, "checks": checks}
		b, _ := json.Marshal(out)
		fmt.Println(string(b))
		return nil
	}
	for _, c := range checks {
		fmt.Printf("%-20s %-6s %s\n", c.Name, c.Status, c.Detail)
	}
	if !ok {
		return fmt.Errorf("doctor found issues")
	}
	return nil
}
