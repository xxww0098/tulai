package tasks

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/store"
)

func TestDailyCapsApplyPerPlatform(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	doubao := addNamed(t, st, "豆包", "甲")
	qianwen := addNamed(t, st, "千问", "乙")
	for _, b := range []store.Browser{doubao, qianwen} {
		st.UpdateBrowser(b.ID, true, true)
	}
	now := time.Now()
	for i := 0; i < 2; i++ {
		st.AddUse(doubao.ID, now)
		st.AddUse(qianwen.ID, now)
	}
	caps := CapsFrom(map[string]int{"豆包": 2, "千问": 5}, DefaultDailyCap)
	board := AssembleCaps(st.Browsers(), LaneLive{}, caps, now)
	if len(board.Lanes) != 2 {
		t.Fatalf("lanes = %+v", board.Lanes)
	}
	for _, lane := range board.Lanes {
		switch lane.Platform {
		case "豆包":
			if lane.State != LaneFull || lane.Cap != 2 {
				t.Fatalf("豆包 lane = %+v", lane)
			}
		case "千问":
			if lane.State != LaneIdle || lane.Cap != 5 {
				t.Fatalf("千问 lane = %+v", lane)
			}
		}
	}
	if slots := ParallelSlotsCaps(st.Browsers(), now, caps); slots != 1 {
		t.Fatalf("only the 千问 account has room: slots=%d", slots)
	}
}

func TestEngineCapForUsesDailyCaps(t *testing.T) {
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{DailyCap: 100, DailyCaps: map[string]int{"豆包": 3}}
	if got := e.capFor("豆包"); got != 3 {
		t.Fatalf("豆包 cap = %d", got)
	}
	if got := e.capFor("千问"); got != 100 {
		t.Fatalf("fallback cap = %d", got)
	}
}

func TestCapsFromFallsBackToDailyCap(t *testing.T) {
	caps := CapsFrom(map[string]int{"豆包": 3}, 7)
	if got := caps.For("豆包"); got != 3 {
		t.Fatalf("豆包 cap = %d", got)
	}
	if got := caps.For("千问"); got != 7 {
		t.Fatalf("fallback cap = %d", got)
	}
	if got := FlatCap(0).For("豆包"); got != 0 {
		t.Fatalf("zero cap should mean unlimited, got %d", got)
	}
	if got := CapsFrom(map[string]int{"豆包": 0}, 7).For("豆包"); got != 0 {
		t.Fatalf("explicit zero should mean unlimited, got %d", got)
	}
}

func TestUnlimitedCapNeverMarksAccountFull(t *testing.T) {
	now := time.Now()
	lane := MapLane(LaneFact{ID: 1, Platform: "豆包", Name: "甲", LoggedIn: true, Health: store.HealthReady, UsedToday: 500, Cap: 0}, now)
	if lane.State != LaneIdle || lane.Cap != 0 {
		t.Fatalf("unlimited lane = %+v", lane)
	}
	rows := []store.Browser{{ID: 1, Platform: "豆包", Name: "甲", IsLoggedIn: true, Health: store.HealthReady, DayKey: now.Format("2006-01-02"), DayCount: 500}}
	caps := CapsFrom(map[string]int{"豆包": 0}, DefaultDailyCap)
	if slots := ParallelSlotsCaps(rows, now, caps); slots != 1 {
		t.Fatalf("unlimited account should still dispatch: slots=%d", slots)
	}
}

func TestDefaultDoubaoUnlimitedStillHonorsPlatformExhaustion(t *testing.T) {
	now := time.Now()
	caps := CapsFrom(nil, 0)
	if caps.For("豆包") != 0 || caps.For("千问") != DefaultDailyCap {
		t.Fatalf("default caps = 豆包:%d 千问:%d", caps.For("豆包"), caps.For("千问"))
	}
	b := store.Browser{Platform: "豆包", IsLoggedIn: true, Health: store.HealthReady, DayKey: now.Format("2006-01-02"), DayCount: 500}
	if !openAccountCaps(b, now, caps) {
		t.Fatal("unlimited account was blocked by its local count")
	}
	b.Health, b.CoolUntil = store.HealthExhausted, now.Add(time.Hour).Unix()
	if openAccountCaps(b, now, caps) {
		t.Fatal("unlimited local cap must not ignore the platform exhaustion state")
	}
	b.Health, b.CoolUntil = store.HealthReady, 0
	if openAccountCaps(b, now, CapsFrom(map[string]int{"豆包": 2}, 0)) {
		t.Fatal("a user-set positive cap was not enforced")
	}
}
