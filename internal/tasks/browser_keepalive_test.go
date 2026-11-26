package tasks

import (
	"strings"
	"testing"

	"github.com/xxww0098/tulai/internal/browser"
)

func TestWebLauncherFailureLeavesTasksUndispatched(t *testing.T) {
	st := openTaskStore(t)
	addNamed(t, st, "豆包", "测试")
	e := NewEngine(st, browser.NewManagerWithConfig(t.TempDir()), nil, "")
	t.Cleanup(e.CloseSessions)
	opts := Options{Platforms: []string{"豆包"}, Prompts: []string{"测试一", "测试二"}, RetryCount: 3}
	err := e.Run(t.Context(), opts, nil, func(Progress) {})
	if err == nil || !strings.Contains(err.Error(), "Fingerprint 尚未准备好") {
		t.Fatalf("missing startup diagnosis: %v", err)
	}
	queue := e.View().Snapshot()
	if len(queue) != len(opts.Prompts) {
		t.Fatalf("queue length %d", len(queue))
	}
	for _, job := range queue {
		if !job.Undispatched || job.Status != StatusSkipped || job.Error != err.Error() || job.Attempts != 0 || !job.StartedAt.IsZero() {
			t.Fatalf("startup failure dispatched a job or lost its reason: %+v", job)
		}
	}
}
