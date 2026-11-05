package ui

import (
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/wizard"
)

func TestChatGPTProviderUsesBoundAccountsInEveryMode(t *testing.T) {
	for _, extension := range []bool{false, true} {
		for _, width := range []int{760, 1280} {
			a := &App{platformAuto: true, concurrencyAuto: true, stageTab: 3, stagePicked: true, prompts: "一只猫",
				browserRows: []store.Browser{{ID: 1, Platform: "ChatGPT", Name: "ChatGPT1", IsLoggedIn: true, Health: store.HealthReady}}}
			if extension {
				st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer st.Close()
				st.Set("browserBackend", "extension")
				row, err := st.AddBrowser("ChatGPT", "ChatGPT1", "")
				if err != nil {
					t.Fatal(err)
				}
				st.ConfirmLogin(row.ID)
				a.store = st
			}
			plan := a.workbenchPlan()
			if plan.Action != StartGenerate || len(plan.Platforms) != 1 || plan.Platforms[0] != "ChatGPT" || a.parallelSlots() != 1 || len(a.laneBrowsers()) != 1 {
				t.Fatalf("extension=%v: plan=%+v slots=%d", extension, plan, a.parallelSlots())
			}
			tt := ui.NewTester(a.view, width, 900)
			tt.Advance(400 * time.Millisecond)
			assertVisible(t, tt, "ChatGPT", float32(width), 900)
			assertVisible(t, tt, "ChatGPT1", float32(width), 900)
			assertVisible(t, tt, "查看浏览器 ChatGPT1", float32(width), 900)
			if dir := os.Getenv("TULAI_CHATGPT_SHOTS"); dir != "" && !extension {
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				name := "chatgpt-wide.png"
				if width == 760 {
					name = "chatgpt-narrow.png"
				}
				f, err := os.Create(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				if err := png.Encode(f, tt.Image()); err != nil {
					f.Close()
					t.Fatal(err)
				}
				f.Close()
			}
		}
	}
}

func TestChatGPTWizardAndModelPersist(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{store: st, manager: browser.NewManager(), dataRoot: t.TempDir()}
	a.loadSettings()
	a.applyWizardGate()
	a.wizardChoice = "ChatGPT"
	a.wizardFirst = true
	a.wizardApplyChoice()
	if !a.platChatGPT || a.platDoubao || !a.wizardNeedsBrowser() || stageIndex("ChatGPT") != 3 || !wizard.Valid("ChatGPT") {
		t.Fatal("ChatGPT wizard missing")
	}
	a.wizardLandOnChoice()
	if a.stagePlatform() != "ChatGPT" {
		t.Fatal(a.stagePlatform())
	}
	a.wizardQueue = wizard.NewQueue([]string{"ChatGPT"})
	row, err := a.ensureWizardAccount()
	if err != nil || row.Platform != "ChatGPT" {
		t.Fatalf("account=%+v err=%v", row, err)
	}
	if err := st.SetBrowserProxy(row.ID, "http://127.0.0.1:8080"); err != nil {
		t.Fatal(err)
	}
	a.chatGPTModel = "auto"
	a.saveSettings()
	reloaded := &App{store: st}
	reloaded.loadSettings()
	if !reloaded.platChatGPT || reloaded.chatGPTModel != "auto" || reloaded.taskOptions(nil, nil, nil).ChatGPTModel != "auto" {
		t.Fatal("ChatGPT settings not restored")
	}
}

func TestChatGPTWizardChoiceVisible(t *testing.T) {
	for _, width := range []int{760, 1280} {
		a := &App{wizardOn: true, wizardStep: wizard.StepPlatform, wizardChoice: "豆包"}
		tt := ui.NewTester(a.view, width, 900)
		tt.Advance(400 * time.Millisecond)
		if dir := os.Getenv("TULAI_CHATGPT_SHOTS"); dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			name := "wizard-wide.png"
			if width == 760 {
				name = "wizard-narrow.png"
			}
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, tt.Image()); err != nil {
				f.Close()
				t.Fatal(err)
			}
			f.Close()
		}
		for _, name := range wizard.Platforms() {
			assertVisible(t, tt, name, float32(width), 900)
		}
		first := assertVisible(t, tt, "豆包", float32(width), 900)
		second := assertVisible(t, tt, "千问", float32(width), 900)
		third := assertVisible(t, tt, "Antigravity", float32(width), 900)
		fourth := assertVisible(t, tt, "ChatGPT", float32(width), 900)
		if math.Abs(float64(second.Y-first.Y)) > 2 || math.Abs(float64(fourth.Y-third.Y)) > 2 || third.Y <= first.Y {
			t.Fatalf("%dpx wizard must have two columns: %v %v %v %v", width, first, second, third, fourth)
		}
		if err := tt.Click("ChatGPT"); err != nil {
			t.Fatal(err)
		}
		if a.wizardChoice != "ChatGPT" {
			t.Fatal("ChatGPT choice did not respond")
		}

	}
}
