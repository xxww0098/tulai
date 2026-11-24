package ui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/store"
)

func TestDoubaoWashDefaultFromDraftToTask(t *testing.T) {
	for _, draft := range []string{"", DefaultWashPrompt, prompts.WindowReviewDoubaoWashPrompt, prompts.FullBleedDoubaoWashPrompt, "只把背景换成蓝色"} {
		t.Run(draft, func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			st.Set("wash_prompt", draft)
			st.Set("workspace_page", store.PageWash)
			a := NewApp(st, nil, nil, "")
			want := prompts.DoubaoWashPrompt
			if draft == "只把背景换成蓝色" {
				want = draft
			}
			if got := a.washPrompt(); got != want {
				t.Fatalf("restored draft = %q, want %q", got, want)
			}
			list := a.taskPromptList()
			if len(list) != 1 || list[0] != want {
				t.Fatalf("wash instruction was changed or split before dispatch: %v", list)
			}
			SetTestApp(a)
			defer SetTestApp(nil)
			setWorkbenchFieldBridge("resetWashPrompt", "")
			if a.prompts != prompts.DoubaoWashPrompt {
				t.Fatal("default button did not restore pure 2D artwork instructions")
			}
			a.clearWorkspace(pageWash)
			if a.prompts != prompts.DoubaoWashPrompt || a.workspaceHasContent(pageWash) {
				t.Fatal("new blank wash workspace has the wrong default or counts it as user content")
			}
		})
	}
}

func TestWashPlatformSwitchPreservesCustomInstructions(t *testing.T) {
	a := &App{statePage: pageWash, tab: pageWash, platDoubao: true, prompts: prompts.DoubaoWashPrompt}
	a.setSinglePlatform("千问")
	if a.prompts != DefaultWashPrompt {
		t.Fatal("another provider lost its general redraw default")
	}
	a.setSinglePlatform("豆包")
	if a.prompts != prompts.DoubaoWashPrompt {
		t.Fatal("Doubao did not select its artwork extraction default")
	}
	a.prompts = "保留画框，按用户自己的要求处理"
	a.setSinglePlatform("千问")
	a.setSinglePlatform("豆包")
	if a.prompts != "保留画框，按用户自己的要求处理" {
		t.Fatal("switching providers overwrote a custom instruction")
	}
	// The wash draft must also follow defaults while another page is open.
	a.statePage, a.tab = pageHome, pageHome
	a.parked = map[int]pageState{pageWash: {prompts: prompts.DoubaoWashPrompt}}
	a.setSinglePlatform("千问")
	if a.stateOf(pageWash).prompts != DefaultWashPrompt {
		t.Fatal("parked wash default was not updated")
	}
}

func TestWorkbenchOnlyShowsActiveCooldownError(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b, err := st.AddBrowser("豆包", "测试", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(b.ID, false, true)
	a := NewApp(st, nil, nil, "")
	SetTestApp(a)
	defer SetTestApp(nil)
	for _, seconds := range []int64{-60, 60, 0} {
		until := int64(0)
		if seconds != 0 {
			until = time.Now().Unix() + seconds
		}
		st.UpdateBrowserHealth(b.ID, store.HealthExhausted, until, "账号额度已用尽")
		a.refreshBrowsers()
		lanes := getWorkbenchStateBridge().Lanes
		if len(lanes) != 1 {
			t.Fatalf("lanes = %d", len(lanes))
		}
		if got := lanes[0].LastError != ""; got != (seconds >= 0) {
			t.Fatalf("cooldown offset %d: displayed error = %q", seconds, lanes[0].LastError)
		}
	}
}
