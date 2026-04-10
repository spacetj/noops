package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spacetj/noops/internal/notion"
	"github.com/spf13/cobra"
)

var (
	flagToken   string
	flagDBID    string
	flagGoal    string
	flagVersion string
	flagDebug   bool
	flagJSON    bool
	flagDotEnv  string
	flagBaseURL string
)

// cliVersion holds the CLI version string. It can be overridden at build time via:
//
//	-ldflags "-X github.com/spacetj/noops/cmd.cliVersion=<version>"
var cliVersion = "dev"

var rootCmd = &cobra.Command{
	Use:   "noops",
	Short: "A Notion Tasks automation CLI",
	Long:  "Tools to clean, create, and validate tasks in a Notion database.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Optional dotenv loading (default .env if present)
		if flagDotEnv != "" {
			if err := loadDotEnv(flagDotEnv); err != nil {
				// non-fatal; continue
			}
		}
		needsToken, needsDB := notionRequirements(cmd)
		if flagToken == "" {
			flagToken = os.Getenv("NOTION_TOKEN")
		}
		if needsToken && flagToken == "" {
			return fmt.Errorf("NOTION_TOKEN is required (set --token or env NOTION_TOKEN)")
		}
		if flagDBID == "" {
			flagDBID = os.Getenv("TASKS_DB_ID")
		}
		if needsDB && flagDBID == "" {
			return fmt.Errorf("TASKS_DB_ID is required (set --db or env TASKS_DB_ID)")
		}
		if flagGoal == "" {
			flagGoal = os.Getenv("GOAL_PAGE_ID")
		}
		if flagDBID != "" {
			flagDBID = normalizeID(flagDBID)
		}
		if flagGoal != "" {
			flagGoal = normalizeID(flagGoal)
		}
		if flagVersion == "" {
			flagVersion = "2022-06-28"
		}
		if flagBaseURL == "" {
			flagBaseURL = os.Getenv("NOTION_BASE_URL")
		}
		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func notionRequirements(cmd *cobra.Command) (needsToken, needsDB bool) {
	if cmd == nil {
		return true, true
	}
	switch cmd.Name() {
	case "help", "completion", "prompt", "version":
		return false, false
	case "init":
		return true, false
	}
	return true, true
}

func init() {
	// Surface version via `--version` and `noops version`.
	rootCmd.Version = cliVersion

	rootCmd.PersistentFlags().StringVar(&flagToken, "token", "", "Notion integration token (env NOTION_TOKEN)")
	rootCmd.PersistentFlags().StringVar(&flagDBID, "db", "", "Tasks database ID or URL (env TASKS_DB_ID)")
	rootCmd.PersistentFlags().StringVar(&flagGoal, "goal", "", "Goal page ID or URL (env GOAL_PAGE_ID)")
	rootCmd.PersistentFlags().StringVar(&flagVersion, "notion-version", "2022-06-28", "Notion API version header")
	rootCmd.PersistentFlags().BoolVar(&flagDebug, "debug", false, "Enable debug logging")
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "Output machine-readable JSON")
	rootCmd.PersistentFlags().StringVar(&flagDotEnv, "dotenv", ".env", "Load environment variables from this file if present (set to empty to disable)")
	rootCmd.PersistentFlags().StringVar(&flagBaseURL, "notion-base-url", "", "Override the Notion API base URL (advanced)")
	_ = rootCmd.PersistentFlags().MarkHidden("notion-base-url")

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(testCmd)
	rootCmd.AddCommand(testAllCmd)
	rootCmd.AddCommand(cleanCmd)
}

func newNotionClient() *notion.Client {
	client := notion.NewClient(flagToken, flagVersion, flagDebug)
	if base := effectiveBaseURL(); base != "" {
		client.SetBaseURL(base)
	}
	return client
}

func effectiveBaseURL() string {
	if flagBaseURL != "" {
		return flagBaseURL
	}
	return os.Getenv("NOTION_BASE_URL")
}

func normalizeID(input string) string {
	s := strings.TrimSpace(input)
	if s == "" {
		return s
	}
	// Strip query string
	if i := strings.IndexByte(s, '?'); i >= 0 {
		s = s[:i]
	}
	s = strings.ToLower(s)
	// Find uuid in path
	for i := 0; i+36 <= len(s); i++ {
		sub := s[i : i+36]
		if isUUID(sub) {
			return sub
		}
	}
	// Find 32-hex id
	for i := 0; i+32 <= len(s); i++ {
		sub := s[i : i+32]
		if isHex(sub) {
			return hyphenate32(sub)
		}
	}
	// If looks like raw 32-hex
	if isHex(s) && len(s) == 32 {
		return hyphenate32(s)
	}
	// If looks like uuid already
	if isUUID(s) {
		return s
	}
	return s
}

func isHex(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if s[i] != '-' {
				return false
			}
			continue
		}
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func hyphenate32(s string) string {
	if len(s) != 32 || !isHex(s) {
		return s
	}
	return fmt.Sprintf("%s-%s-%s-%s-%s", s[0:8], s[8:12], s[12:16], s[16:20], s[20:32])
}

// Minimal .env loader (KEY=VALUE), ignores comments and blanks.
// Only sets variables that are not already present in the environment.
func loadDotEnv(path string) error {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	for _, ln := range lines {
		s := strings.TrimSpace(ln)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if i := strings.IndexByte(s, '='); i > 0 {
			key := strings.TrimSpace(s[:i])
			val := strings.TrimSpace(s[i+1:])
			// strip optional quotes
			if len(val) >= 2 {
				if (val[0] == '\'' && val[len(val)-1] == '\'') || (val[0] == '"' && val[len(val)-1] == '"') {
					val = val[1 : len(val)-1]
				}
			}
			if os.Getenv(key) == "" {
				_ = os.Setenv(key, val)
			}
		}
	}
	return nil
}
