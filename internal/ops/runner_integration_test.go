package ops_test

import (
	"context"
	"io"
	"log"
	"testing"

	"github.com/spacetj/noops/internal/notion"
	"github.com/spacetj/noops/internal/notion/mock"
	"github.com/spacetj/noops/internal/ops"
)

func TestRunnerRunCleansUntitledAndSmoke(t *testing.T) {
	srv := mock.NewServer()
	t.Cleanup(srv.Close)
	dbID := "db-123"
	srv.AddDatabase(mock.StandardDatabase(dbID))
	srv.AddPage(mock.PageWithProperties(dbID, "page-keep", map[string]any{
		"Name":   mock.Title("Keep me"),
		"Status": mock.Status("Not Started"),
	}))
	srv.AddPage(mock.PageWithProperties(dbID, "page-archive", map[string]any{
		"Name":   mock.Title(""),
		"Status": mock.Status("Not Started"),
	}))

	client := notion.NewClient("ntn_mock", "2022-06-28", false)
	client.SetBaseURL(srv.URL())
	client.SetTransport(srv)
	logger := log.New(io.Discard, "", 0)
	runner := ops.NewRunner(client, logger)
	runner.SetDBID(dbID)

	ctx := context.Background()
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("runner.Run: %v", err)
	}

	if page, ok := srv.Page("page-archive"); !ok {
		t.Fatalf("expected page-archive to exist")
	} else if !page.Archived {
		t.Fatalf("expected page-archive to be archived")
	}

	if page, ok := srv.Page("page-keep"); !ok {
		t.Fatalf("expected page-keep to exist")
	} else if page.Archived {
		t.Fatalf("expected page-keep to remain unarchived")
	}
}

func TestRunnerBuildPropsFromSpecs(t *testing.T) {
	srv := mock.NewServer()
	t.Cleanup(srv.Close)
	dbID := "db-props"
	srv.AddDatabase(mock.StandardDatabase(dbID))

	client := notion.NewClient("ntn_mock", "2022-06-28", false)
	client.SetBaseURL(srv.URL())
	client.SetTransport(srv)
	runner := ops.NewRunner(client, log.New(io.Discard, "", 0))
	runner.SetDBID(dbID)

	ctx := context.Background()
	if err := runner.FetchDB(ctx); err != nil {
		t.Fatalf("FetchDB: %v", err)
	}

	specs := []string{
		"Due:date=2024-10-01",
		"Status:status=In Progress",
		"Priority=High",
	}

	props, warns, err := runner.BuildPropsFromSpecs(specs)
	if err != nil {
		t.Fatalf("BuildPropsFromSpecs: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("expected no warnings, got %v", warns)
	}

	due, ok := props["Due"].(map[string]any)
	if !ok {
		t.Fatalf("missing Due property")
	}
	dueVal, _ := due["date"].(map[string]any)
	if got := dueVal["start"]; got != "2024-10-01" {
		t.Fatalf("Due.start = %v, want 2024-10-01", got)
	}

	status, ok := props[runner.StatusKey()].(map[string]any)
	if !ok {
		t.Fatalf("missing status property")
	}
	statusVal, _ := status["status"].(map[string]any)
	if got := statusVal["name"]; got != "In Progress" {
		t.Fatalf("status.name = %v, want In Progress", got)
	}

	priority, ok := props["Priority"].(map[string]any)
	if !ok {
		t.Fatalf("missing Priority property")
	}
	priorityVal, _ := priority["select"].(map[string]any)
	if got := priorityVal["name"]; got != "High" {
		t.Fatalf("priority.name = %v, want High", got)
	}
}
