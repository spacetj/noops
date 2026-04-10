package cmd

import (
	"context"
	"log"

	"github.com/spacetj/noops/internal/ops"
	"github.com/spf13/cobra"
)

var cleanCmd = &cobra.Command{
	Use:   "cleanup-untitled",
	Short: "Archive pages with empty titles",
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := log.Default()
		if flagDebug {
			logger.SetFlags(log.LstdFlags | log.Lshortfile)
		}
		ctx := context.Background()
		client := newNotionClient()
		runner := ops.NewRunner(client, logger)
		runner.SetDBID(flagDBID)
		return runner.CleanupUntitled(ctx)
	},
}
