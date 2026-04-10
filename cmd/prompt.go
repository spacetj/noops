package cmd

import (
    "fmt"
    _ "embed"
    "github.com/spf13/cobra"
)

//go:embed assets/PROMPT.md
var promptText string

var promptCmd = &cobra.Command{
    Use:   "prompt",
    Short: "Print the ChatGPT ops prompt for this CLI",
    Long:  "Outputs a ready-to-use prompt that teaches an AI agent how to operate this Notion CLI safely and effectively.",
    RunE: func(cmd *cobra.Command, args []string) error {
        fmt.Print(promptText)
        return nil
    },
}

func init() {
    rootCmd.AddCommand(promptCmd)
}
