package cmd

import (
	"context"
	"log"

	"github.com/spacetj/noops/internal/ops"
	"github.com/spf13/cobra"
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Run verification tests against the Notion DB",
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := log.Default()
		if flagDebug {
			logger.SetFlags(log.LstdFlags | log.Lshortfile)
		}
		ctx := context.Background()
		client := newNotionClient()
		runner := ops.NewRunner(client, logger)
		runner.SetDBID(flagDBID)
		runner.SetGoalPageID(flagGoal)
		if err := runner.Tests(ctx); err != nil {
			if testSoft {
				if !flagJSON {
					log.Printf("soft test: %v", err)
				}
				return nil
			}
			return err
		}
		return nil
	},
}

var testSoft bool

func init() {
	testCmd.Flags().BoolVar(&testSoft, "soft", false, "Do not fail the command if checks are missing; log and continue")
}
