package strategy

import (
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/logging"
)

func TestResultSettleUsesDecodedPixels(t *testing.T) {
	thumb := []Result{{URL: "https://cdn.example/a.png", Width: 32, Height: 32}}
	if resultSettleFor(thumb) != imageStableFor {
		t.Fatalf("small bitmap should keep the long settle, got %s", resultSettleFor(thumb))
	}
	if resultSettleFor([]Result{{URL: "https://cdn.example/a.png"}}) != imageStableFor {
		t.Fatal("missing dimensions should keep the long settle")
	}
	full := []Result{{URL: "https://cdn.example/a.png", Width: 1024, Height: 1024}}
	if resultSettleFor(full) != imagePixelStableFor {
		t.Fatalf("decoded image should settle on the next poll, got %s", resultSettleFor(full))
	}
	grown := liveKey([]Result{{URL: "https://cdn.example/a.png", Width: 256, Height: 256}})
	settled := liveKey([]Result{{URL: "https://cdn.example/a.png", Width: 1024, Height: 1024}})
	if grown == settled {
		t.Fatal("a larger decode of the same URL must reset the settle clock")
	}
}

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
