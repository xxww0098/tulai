package ui

import (
	"context"
	"errors"
	"fmt"
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
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, "当前版本 "+currentVersion()).FontSize(14).Grow(1).MinWidth(0).SingleLine()
		ui.Button(c, "检查更新").Shrink(0).Disabled(a.updateBusy).OnClick(func() { a.checkForUpdates() })
	})
	ui.Checkbox(c, &a.autoCheck, "自动检查更新").OnChange(func() { a.persistUpdatePrefs() })
	ui.Field(c, "更新镜像", func() {
		ui.TextInput(c, &a.updateMirror).Placeholder("留空用默认镜像，填 off 则只用 GitHub")
	})
	ui.Text(c, "镜像是地址前缀。GitHub 连不上时才会用它。").FontSize(12).TextColor(c.Theme().TextMuted)
}

func (a *App) updateBanner(c *ui.Context) {
	if a.updateNotice == "" {
		return
	}
	if a.updateHidden && a.updateDownloading {
		return
	}
	a.updateHidden = false
	tok := a.look
	w, h := c.Size()
	bottom := float32(16)
	if a.wizardOn {
		bottom = 80
	}
	ui.Overlay(c, func() {
		ui.Column(c.Key("update-banner")).Absolute().Right(16).Bottom(bottom).Width(min(320, max(0, w-32))).
			Padding(10, 12).Gap(8).Radius(10).Background(tok.card).Border(1, tok.border).
			Shadow(0, 4, 16, 0, tok.shadow).Role(ui.RoleStatus).Label("更新提示").Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "发现新版本 "+a.updateNotice).FontSize(13).Bold().Grow(1).MinWidth(0).SingleLine()
				label, accessible := "详情", "查看更新详情"
				if a.updateDetailsOpen {
					label, accessible = "收起", "收起更新详情"
				}
				ui.Button(c.Key("update-details"), label).Label(accessible).FontSize(12).Padding(4, 8).Shrink(0).
					OnClick(func() { a.updateDetailsOpen = !a.updateDetailsOpen })
				if a.updateDownloading {
					ui.Button(c, "隐藏").Label("隐藏更新提示").FontSize(12).Padding(4, 8).Shrink(0).
						OnClick(func() { a.updateHidden = true })
				} else {
					ui.Button(c, "稍后").FontSize(12).Padding(4, 8).Shrink(0).OnClick(func() { a.dismissUpdate() })
				}
			})
			if a.updateDeferred {
				ui.Text(c, update.DeferMessage).FontSize(12).TextColor(tok.warning).SingleLine().Tooltip(update.DeferMessage)
			}
			if a.updateErr != "" {
				ui.Text(c, a.updateErr).FontSize(12).TextColor(tok.danger).SingleLine().Tooltip(a.updateErr)
			}
			if a.updateDownloading {
				installing := strings.Contains(a.updateProgressText, "正在安装")
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, a.updateProgressText).FontSize(12).TextColor(tok.muted).Grow(1).MinWidth(0).SingleLine().Tooltip(a.updateProgressText)
					if !installing && a.updateFraction > 0 {
						ui.Text(c, fmt.Sprintf("%.0f%%", min(a.updateFraction, 1)*100)).FontSize(12).TextColor(tok.muted)
					}
				})
				motionProgress(c, "update-progress", a.updateFraction, installing || a.updateFraction <= 0)
			}
			if a.updateDetailsOpen {
				verification := "安装包未签名，下载后会用 SHA256SUMS 校验。"
				if a.updateSigned != nil {
					verification = "更新包由 MyGo 验证开发者签名。"
				}
				ui.Text(c, verification).FontSize(12).TextColor(tok.muted).SingleLine().Tooltip(verification)
				a.updateBannerActions(c)
				if a.updateNotes != "" && a.updateNotesOpen {
					ui.Scroll(c.Key("update-notes")).FillWidth().MaxHeight(min(120, max(40, h-bottom-180))).Label("更新说明").Children(func() {
						ui.Column(c).FillWidth().Shrink(0).Children(func() {
							for _, line := range splitLines(a.updateNotes) {
								ui.Text(c, line).FontSize(12).TextColor(tok.muted).Shrink(0)
							}
						})
					})
				}
			}
		})
	})
}

func (a *App) updateBannerActions(c *ui.Context) {
	ui.Row(c).FillWidth().Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
		if a.updateNotes != "" {
			label := "查看更新说明"
			if a.updateNotesOpen {
				label = "收起更新说明"
			}
			ui.Button(c, label).FontSize(12).Shrink(0).OnClick(func() { a.updateNotesOpen = !a.updateNotesOpen })
		}
		if !a.updateDownloading {
			ui.Button(c, "跳过此版本").FontSize(12).Shrink(0).OnClick(func() { a.skipUpdate() })
			ui.Button(c, "立即更新").FontSize(12).Shrink(0).Background(a.look.accent.Alpha(0.1)).TextColor(a.look.accent).
				Disabled(a.updateBusy).OnClick(func() { a.applyUpdate() })
		}
	})
}

// checkForUpdates is the manual entry from the menu, the title bar, and settings.
func (a *App) checkForUpdates() {
	if a.updateHidden && a.updateDownloading {
		a.updateHidden = false
		return
	}
	if a.updateBusy {
		return
	}
	a.updateChecks++
	if a.window == nil {
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
	var signed *mygo.Update
	exe, err := update.RunningExecutable()
	if err != nil {
		logging.Log.Errorf("定位应用失败: %v", err)
		if manual {
			a.tellUpdate(mygo.MessageError, "检查更新失败", err.Error())
		}
		return
	}
	if update.MyGoInstall(runtime.GOOS, exe) && !mygo.IsDev() {
		signed, err = client.CheckSigned(ctx, rel)
		if err != nil {
			logging.Log.Errorf("检查签名更新失败: %v", err)
			if manual {
				a.tellUpdate(mygo.MessageError, "检查更新失败", err.Error())
			}
			return
		}
	}
	notes := update.ShortNotes(rel.Body, 180)
	a.onUI(func() {
		a.updateRel = rel
		a.updateSigned = signed
		a.updateSignedClient = client
		if a.updateNotice != latest {
			a.updateDetailsOpen = false
			a.updateNotesOpen = false
			a.updateHidden = false
		}
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
	if a.running || a.fileRename.busy || a.edit.running || a.dedup.busy || a.camoufoxBusy || a.downloadsRunning > 0 {
		return true
	}
	for _, task := range a.dlTasks {
		if task.Phase == dlActive || task.Phase == dlQueued {
			return true
		}
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
				if !a.updatePending {
					a.pumpDownloads()
				}
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
			if !a.updatePending {
				a.pumpDownloads()
			}
		})
		a.downloadAndInstall()
	}()
}

func (a *App) downloadAndInstall() {
	var running bool
	var rel update.Release
	var asset, mirror, ver string
	var signed *mygo.Update
	var signedClient *update.Client
	a.onUI(func() {
		running = a.tasksRunning()
		rel = a.updateRel
		asset = a.updateAsset
		mirror = a.updateMirror
		ver = a.updateNotice
		signed = a.updateSigned
		signedClient = a.updateSignedClient
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
	if signed != nil {
		signedClient.ApplyMirrorSetting(mirror)
		signedClient.OnProgress = func(p update.Progress) {
			a.onUI(func() {
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
		if err := signed.Install(ctx, nil); err != nil {
			logging.Log.Errorf("安装签名更新失败: %v", err)
			a.failUpdate(err)
			return
		}
		logging.Log.Success("已安装签名更新 %s", ver)
		mygo.App.Relaunch()
		return
	}
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
	a.updateDetailsOpen = false
	a.updateNotesOpen = false
	a.updateHidden = false
	a.updateErr = ""
	a.updatePending = false
	a.updateDeferred = false
	a.updateDownloading = false
	a.updateProgressText = ""
	a.updateAsset = ""
	a.updateSigned = nil
	a.updateSignedClient = nil
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
