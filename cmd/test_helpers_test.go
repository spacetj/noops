package cmd

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func runCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	resetGlobalFlags()

	rootCmd.SetArgs(args)

	saveStdout := os.Stdout
	saveStderr := os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	outCh := make(chan string, 1)
	errCh := make(chan string, 1)

	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rOut)
		outCh <- buf.String()
	}()
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rErr)
		errCh <- buf.String()
	}()

	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	err := rootCmd.Execute()

	wOut.Close()
	wErr.Close()
	os.Stdout = saveStdout
	os.Stderr = saveStderr
	rootCmd.SetOut(os.Stdout)
	rootCmd.SetErr(os.Stderr)
	rootCmd.SetArgs(nil)

	stdout := <-outCh
	stderr := <-errCh

	return stdout, stderr, err
}

func resetGlobalFlags() {
	flagToken = ""
	flagDBID = ""
	flagGoal = ""
	flagVersion = "2022-06-28"
	flagDebug = false
	flagJSON = false
	flagDotEnv = ".env"
	flagBaseURL = ""

	setProps = nil
	taskID = ""
	taskTitle = ""
	newTitle = ""
	taskEmoji = ""
	taskDesc = ""

	listWhere = nil
	listSort = nil
	listPage = 50
	listCursor = ""
	listCompact = false
	fNameContains = ""
	fNameEquals = ""
	fStatusEq = ""
	fPriorityEq = ""
	fDueBefore = ""
	fDueAfter = ""
	fDoBefore = ""
	fDoAfter = ""
	fGoalIn = ""

	exportGoal = ""
	exportWhere = nil
	exportSort = nil
	exportOut = ""
	exportPage = 100
	exportCompact = false

	ensureFromJSON = ""
	ensureGoalArg = ""
	ensureUpdateExisting = false
	ensureDedupe = string(dedupeKeepFirst)

	goalInfoName = ""
	goalInfoID = ""
	goalCreateTitle = ""
	goalCreateStatus = ""
	goalCreateEmoji = ""
	goalUpdateID = ""
	goalUpdateTitle = ""
	goalUpdateRename = ""
	goalUpdateStatus = ""
	goalUpdateEmoji = ""
	goalDeleteID = ""
	goalDeleteTitle = ""

	migrateGoal = ""
	migrateFromProp = "Summary"
	migrateClear = true
	migrateDryRun = false

	testSoft = false

	flagServeAddr = ""
	flagAllowedEmail = ""

	fAddTitle = ""
	fAddDo = ""
	fAddDue = ""
	fAddStatus = ""
	fAddPriority = ""
	fAddGoal = ""
	fAddParent = ""
	fAddEmoji = ""
	fAddAutoEmoji = false
	fAddDesc = ""
	fListGoal = ""
	fListStatus = ""
	fListParent = ""

	initPageName = "Noops Ops"
	initTasksName = "Noops Tasks"
	initGoalsName = "Noops Goals"
	initDefaultGoalName = "Default Goal"
	initWriteEnv = true
}
