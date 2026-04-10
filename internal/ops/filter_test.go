package ops

import (
	"encoding/json"
	"testing"
)

func TestBuildFilter_BasicText(t *testing.T) {
	f, err := BuildFilter([]string{"Title:title.equals=Hello"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(f)
	if string(b) == "{}" {
		t.Fatalf("empty filter")
	}
}

func TestBuildFilter_DateOps(t *testing.T) {
	f, err := BuildFilter([]string{"Due:date.on_or_before=2025-10-31", "Do:date.on_or_after=2025-10-01"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(f)
	if len(b) == 0 {
		t.Fatalf("no json")
	}
}

func TestBuildSorts(t *testing.T) {
	s, err := BuildSorts([]string{"Due:desc", "Priority:asc"})
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 2 {
		t.Fatalf("want 2 sorts")
	}
}
