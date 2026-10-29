package strategy

import (
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/logging"
)

func TestDetectLogOnlyOnChange(t *testing.T) {
	start := len(logging.Log.Entries())
	var last string
	idle := liveShot{Reason: "no-reply"}
	for range 5 {
		if sig := detectKey(idle); sig != last {
			last = sig
			logging.Log.Debugf("%s", detectLine(idle, 0))
		}
	}
	busy := liveShot{Busy: true, Reason: "generating"}
	if sig := detectKey(busy); sig != last {
		last = sig
		logging.Log.Debugf("%s", detectLine(busy, 0))
	}
	// 同一组图在稳住的几秒里 stable_ms 会变，不能因此再刷一行。
	if sig := detectKey(busy); sig != last {
		t.Fatal("stable clock must not look like a new detection")
	}
	var lines []string
	for _, entry := range logging.Log.Entries()[start:] {
		if strings.HasPrefix(entry.Text, "检测 ") {
			lines = append(lines, entry.Text)
		}
	}
	if len(lines) != 2 || !strings.Contains(lines[0], "images=0") || !strings.Contains(lines[0], "no-reply") || !strings.Contains(lines[1], "generating") {
		t.Fatalf("logs = %q", lines)
	}
	if detectLine(idle, 300*time.Millisecond) == detectLine(idle, 0) {
		t.Fatal("the line itself still carries stable_ms when the state does change")
	}
}
