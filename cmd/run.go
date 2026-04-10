package cmd

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/spacetj/noops/internal/ops"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the Notion tasks automation",
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := log.Default()
		if flagDebug {
			logger.SetFlags(log.LstdFlags | log.Lshortfile)
		} else {
			logger.SetFlags(log.LstdFlags)
		}
		ctx := context.Background()
		client := newNotionClient()
		runner := ops.NewRunner(client, logger)
		runner.SetDBID(flagDBID)
		runner.SetGoalPageID(flagGoal)
		start := time.Now()
		if err := runner.Run(ctx); err != nil {
			return err
		}
		fmt.Printf("Completed in %s\n", time.Since(start))
		return nil
	},
}
