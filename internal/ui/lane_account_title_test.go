package ui

import (
	"testing"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestLaneAccountTitlePrefersEmail(t *testing.T) {
	ln := tasks.AccountLane{Platform: agy.Platform, Name: "acc1", Email: "xiewei0098@gmail.com"}
	if got := laneAccountTitle(ln); got != "xiewei0098@gmail.com" {
		t.Fatalf("got %q", got)
	}
	ln.Email = "  "
	if got := laneAccountTitle(ln); got != "acc1" {
		t.Fatalf("fallback %q", got)
	}
	other := tasks.AccountLane{Platform: "豆包", Name: "豆包1", Email: "ignored@x.com"}
	if got := laneAccountTitle(other); got != "豆包1" {
		t.Fatalf("other platform %q", got)
	}
}
