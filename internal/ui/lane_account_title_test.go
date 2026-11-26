package ui

import (
	"testing"

	"github.com/xxww0098/tulai/internal/tasks"
)

func TestLaneAccountTitleUsesName(t *testing.T) {
	ln := tasks.AccountLane{Platform: "千问", Name: "千问1"}
	if got := laneAccountTitle(ln); got != "千问1" {
		t.Fatalf("got %q", got)
	}
	blank := tasks.AccountLane{Platform: "豆包", Name: "  "}
	if got := laneAccountTitle(blank); got != "豆包" {
		t.Fatalf("blank name %q", got)
	}
}
