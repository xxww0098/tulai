package ui

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/update"
)

func (a *App) installMenu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Role: mygo.RoleEditMenu},
		{Label: "帮助", Submenu: []*mygo.MenuItem{
			{
				Label:       "检查更新…",
				Accelerator: "CmdOrCtrl+U",
				Click: func(*mygo.MenuItem, *mygo.Window) {
					a.checkForUpdates()
				},
			},
		}},
		{Role: mygo.RoleWindowMenu},
	})
}

func currentVersion() string {
	if v := strings.TrimSpace(mygo.App.Version()); v != "" {
		return v
	}
	return "未打包"
}

func (a *App) persistUpdatePrefs() {
	if a == nil || a.store == nil {
		return
	}
	a.store.SetInt(update.KeyAutoCheck, boolInt(a.autoCheck))
	a.store.Set(update.KeyMirror, strings.TrimSpace(a.updateMirror))
}

func (a *App) updateSettings(c *ui.Context) {
	ui.Fieldset(c, "更新", func() {
		ui.Text(c, "当前版本 "+currentVersion()).FontSize(14)
		ui.Checkbox(c, &a.autoCheck, "自动检查更新").OnChange(func() { a.persistUpdatePrefs() })
		ui.Field(c, "更新镜像", func() {
			ui.TextInput(c, &a.updateMirror).Placeholder("留空用默认镜像，填 off 则只用 GitHub")
		})
		ui.Text(c, "镜像是地址前缀，例如 https://ghfast.top/。GitHub 连不上时才会用它。").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Button(c, "检查更新").Disabled(a.updateBusy).OnClick(func() { a.checkForUpdates() })
	}).Gap(8)
}

func (a *App) updateBanner(c *ui.Context) {
	if a.updateNotice == "" {
		return
	}
	ui.Column(c.Key("update-banner")).FillWidth().Padding(10, 12).Gap(6).Radius(10).Background(c.Theme().Surface).Border(1, c.Theme().Border).Transition(motionBanner()).Children(func() {
		ui.Text(c, "发现新版本 "+a.updateNotice).FontSize(14).Bold()
		if a.updateNotes != "" {
			ui.Text(c, a.updateNotes).FontSize(12).TextColor(c.Theme().TextMuted)
		}
		if a.updateDeferred {
			ui.Text(c, update.DeferMessage).FontSize(12).TextColor(c.Theme().Warning)
		}
		if a.updateErr != "" {
			ui.Text(c, a.updateErr).FontSize(12).TextColor(c.Theme().Danger)
		}
		if a.updateDownloading {
			installing := strings.Contains(a.updateProgressText, "正在安装")
			motionProgress(c, "update-progress", a.updateFraction, installing || a.updateFraction <= 0)
			if a.updateProgressText != "" {
				ui.Text(c, a.updateProgressText).FontSize(12).TextColor(c.Theme().TextMuted)
			}
		}
		ui.Text(c, "安装包未签名，下载后会用 SHA256SUMS 校验。").FontSize(11).TextColor(c.Theme().TextMuted)
		ui.Row(c).Gap(8).Children(func() {
			ui.Button(c, "立即更新").Disabled(a.updateBusy || a.updateDownloading).OnClick(func() { a.applyUpdate() })
			ui.Button(c, "稍后").Disabled(a.updateDownloading).OnClick(func() { a.dismissUpdate() })
			ui.Button(c, "跳过此版本").Disabled(a.updateDownloading).OnClick(func() { a.skipUpdate() })
		})
	})
}

// checkForUpdates is the manual entry from the menu, the title bar, and settings.
func (a *App) checkForUpdates() {
	if a.updateBusy {
		return
	}
	a.updateBusy = true
	go func() {
		defer a.onUI(func() { a.updateBusy = false })
		a.runUpdateCheck(true)
	}()
}

func (a *App) startUpdateLoop() {
	if a.updateStop == nil {
		return
	}
	go func() {
		a.silentCheck()
		ticker := time.NewTicker(update.CheckEvery)
		defer ticker.Stop()
		for {
			select {
			case <-a.updateStop:
				return
			case <-ticker.C:
				a.silentCheck()
			}
		}
	}()
}

func (a *App) silentCheck() {
	var on, busy bool
	a.onUI(func() {
		on = a.autoCheck
		busy = a.updateBusy
		if on && !busy {
			a.updateBusy = true
		}
	})
	if !on || busy {
		return
	}
	defer a.onUI(func() { a.updateBusy = false })
	a.runUpdateCheck(false)
}

func (a *App) onUI(fn func()) {
	if a.window == nil {
		fn()
		return
	}
	done := make(chan struct{})
	a.window.Update(func() {
		defer close(done)
		fn()
	})
	<-done
}

func (a *App) runUpdateCheck(manual bool) {
	current := mygo.App.Version()
	if current == "" {
		if manual {
			a.tellUpdate(mygo.MessageError, "检查更新失败", "没有当前版本")
		}
		return
	}
	var mirror, skipped string
	a.onUI(func() {
		mirror = a.updateMirror
		a.persistUpdatePrefs()
		if a.store != nil {
			skipped = update.Skipped(a.store)
		}
	})
	logging.Log.Info("正在检查更新（当前 %s）", current)
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	client := update.NewClient(current)
	client.ApplyMirrorSetting(mirror)
	rel, err := client.Latest(ctx)
	if err != nil {
		logging.Log.Errorf("检查更新失败: %v", err)
		if manual {
			a.tellUpdate(mygo.MessageError, "检查更新失败", err.Error())
		}
		return
	}
	latest, err := rel.Version()
	if err != nil {
		logging.Log.Errorf("检查更新失败: %v", err)
		if manual {
			a.tellUpdate(mygo.MessageError, "检查更新失败", err.Error())
		}
		return
	}
	newer, err := update.Newer(current, latest)
	if err != nil {
		logging.Log.Errorf("检查更新失败: %v", err)
		if manual {
			a.tellUpdate(mygo.MessageError, "检查更新失败", err.Error())
		}
		return
	}
	if !newer {
		logging.Log.Info("已是最新版本 %s", current)
		if manual {
			a.tellUpdate(mygo.MessageInfo, "已是最新版本", "当前版本 "+current)
		}
		return
	}
	if !manual {
		show, err := update.ShouldOffer(current, latest, skipped)
		if err != nil || !show {
			logging.Log.Info("已跳过版本 %s", latest)
			return
		}
	}
	notes := update.ShortNotes(rel.Body, 180)
	a.onUI(func() {
		a.updateRel = rel
		a.updateNotice = latest
		a.updateNotes = notes
		a.updateErr = ""
		a.updateAsset = ""
		a.updateDeferred = false
		a.updatePending = false
		a.updateDownloading = false
	})
	logging.Log.Info("发现新版本 %s", latest)
}

func (a *App) tasksRunning() bool {
	if a.running {
		return true
	}
	return a.engine != nil && a.engine.IsRunning()
}

func (a *App) applyUpdate() {
	if a.updateDownloading {
		return
	}
	switch update.NextStep(a.tasksRunning(), true, a.updateAsset != "") {
	case update.StepDefer:
		a.updatePending = true
		a.updateDeferred = true
		logging.Log.Info("%s", update.DeferMessage)
		return
	default:
		a.updatePending = true
		a.updateDeferred = false
		a.updateBusy = true
		go func() {
			defer a.onUI(func() {
				a.updateBusy = false
				a.updateDownloading = false
			})
			a.downloadAndInstall()
		}()
	}
}

func (a *App) maybeInstallPending() {
	if !a.updatePending || a.updateDownloading {
		return
	}
	if update.NextStep(a.tasksRunning(), true, a.updateAsset != "") == update.StepDefer {
		a.updateDeferred = true
		return
	}
	if a.updateBusy {
		return
	}
	a.updateDeferred = false
	a.updateBusy = true
	go func() {
		defer a.onUI(func() {
			a.updateBusy = false
			a.updateDownloading = false
		})
		a.downloadAndInstall()
	}()
}

func (a *App) downloadAndInstall() {
	var running bool
	var rel update.Release
	var asset, mirror, ver string
	a.onUI(func() {
		running = a.tasksRunning()
		rel = a.updateRel
		asset = a.updateAsset
		mirror = a.updateMirror
		ver = a.updateNotice
	})
	if running {
		a.onUI(func() {
			a.updatePending = true
			a.updateDeferred = true
			a.updateDownloading = false
			a.updateProgressText = update.DeferMessage
		})
		return
	}
	current := mygo.App.Version()
	exe, err := update.RunningExecutable()
	if err != nil {
		a.failUpdate(err)
		return
	}
	mode := update.DetectMode(runtime.GOOS, exe)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if asset == "" {
		if rel.Tag == "" {
			a.failUpdate(errors.New("还没有可安装的版本"))
			return
		}
		client := update.NewClient(current)
		client.ApplyMirrorSetting(mirror)
		client.OnProgress = func(p update.Progress) {
			a.onUI(func() {
				a.updateDownloading = true
				a.updateProgressText = "正在下载 " + p.Name
				if p.Total > 0 {
					a.updateFraction = float64(p.Done) / float64(p.Total)
				}
			})
		}
		a.onUI(func() {
			a.updateDownloading = true
			a.updateProgressText = "正在下载 " + ver
			a.updateFraction = 0
			a.updateErr = ""
		})
		prepared, err := client.Download(ctx, rel, current, mode, runtime.GOARCH)
		if err != nil {
			logging.Log.Errorf("下载更新失败: %v", err)
			a.failUpdate(err)
			return
		}
		if prepared.UpToDate {
			a.onUI(func() {
				a.updateNotice = ""
				a.updatePending = false
				a.updateDownloading = false
			})
			return
		}
		asset = prepared.Path
		if v, err := prepared.Release.Version(); err == nil {
			ver = v
		}
		a.onUI(func() {
			a.updateAsset = asset
			running = a.tasksRunning()
		})
		if running {
			a.onUI(func() {
				a.updatePending = true
				a.updateDeferred = true
				a.updateDownloading = false
				a.updateProgressText = update.DeferMessage
			})
			return
		}
	}
	a.onUI(func() {
		a.updateDownloading = true
		a.updateProgressText = "正在安装…"
		a.updateFraction = 1
	})
	out, err := update.Install(update.Options{
		Mode:       mode,
		Executable: exe,
		AssetPath:  asset,
		Version:    ver,
	})
	if err != nil {
		logging.Log.Errorf("安装更新失败: %v", err)
		a.failUpdate(err)
		return
	}
	logging.Log.Success("%s", firstLine(out.Message))
	if out.OpenPath != "" {
		if err := update.Open(out.OpenPath); err != nil {
			logging.Log.Errorf("打开安装包失败: %v", err)
		}
		a.onUI(func() {
			a.updateDownloading = false
			a.updatePending = false
			a.updateDeferred = false
			a.updateProgressText = ""
			a.updateNotes = out.Message
		})
		a.tellUpdate(mygo.MessageInfo, "已下载安装包", out.Message)
		return
	}
	if out.Helper != "" {
		if err := update.StartDetached(out.Helper, out.HelperArgs...); err != nil {
			a.failUpdate(err)
			return
		}
	}
	if out.Quit {
		mygo.App.Quit()
		return
	}
	if out.Relaunch {
		mygo.App.Relaunch()
	}
}

func (a *App) failUpdate(err error) {
	if err == nil {
		return
	}
	a.onUI(func() {
		a.updateDownloading = false
		a.updateErr = err.Error()
		a.updateProgressText = ""
		a.updatePending = false
	})
}

func (a *App) dismissUpdate() {
	a.updateNotice = ""
	a.updateNotes = ""
	a.updateErr = ""
	a.updatePending = false
	a.updateDeferred = false
	a.updateDownloading = false
	a.updateProgressText = ""
	a.updateAsset = ""
}

func (a *App) skipUpdate() {
	version := a.updateNotice
	if a.store != nil && version != "" {
		if err := update.RememberSkip(a.store, version); err != nil {
			a.updateErr = err.Error()
			return
		}
	}
	logging.Log.Info("已跳过版本 %s", version)
	a.dismissUpdate()
}

func (a *App) tellUpdate(kind mygo.MessageType, message, detail string) {
	_, _ = mygo.Dialog.Message(mygo.MessageOptions{
		Parent:  a.window,
		Type:    kind,
		Title:   "检查更新",
		Message: message,
		Detail:  detail,
		Buttons: []string{"好"},
	})
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
