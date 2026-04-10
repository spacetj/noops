package cmd

import (
	"context"
	"log"

	"github.com/spacetj/noops/internal/ops"
	"github.com/spf13/cobra"
)

var testAllCmd = &cobra.Command{
	Use:     "test-all",
	Aliases: []string{"e2e"},
	Short:   "Run extensive end-to-end tests against the DB/goal",
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := log.Default()
		if flagDebug {
			logger.SetFlags(log.LstdFlags | log.Lshortfile)
		}
		ctx := context.Background()
		client := newNotionClient()
		r := ops.NewRunner(client, logger)
		r.SetDBID(flagDBID)
		r.SetGoalPageID(flagGoal)
		return r.TestAll(ctx)
	},
}
