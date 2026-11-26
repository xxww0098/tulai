package ui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/update"
)

func (a *App) persistUpdatePrefs() {
	if a == nil || a.store == nil {
		return
	}
	a.store.SetInt(update.KeyAutoCheck, boolInt(a.autoCheck))
	a.store.Set(update.KeyMirror, strings.TrimSpace(a.updateMirror))
}

func (a *App) checkForUpdates() {
	if a.updateHidden && a.updateDownloading {
		a.updateHidden = false
		a.emit("update")
		return
	}
	if a.updateBusy {
		return
	}
	a.updateChecks++
	if !a.hasWindow() {
		return
	}
	a.updateBusy = true
	a.emit("update", "settings")
	go func() {
		defer a.onUI(func() {
			a.updateBusy = false
			a.emit("update", "settings")
		})
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
	auto := false
	busy := false
	a.syncUI(func() {
		auto = a.autoCheck
		busy = a.updateBusy
	})
	if !auto || busy {
		return
	}
	a.syncUI(func() { a.updateBusy = true })
	defer a.onUI(func() {
		a.updateBusy = false
		a.emit("update", "settings")
	})
	a.runUpdateCheck(false)
}

func (a *App) runUpdateCheck(manual bool) {
	current := PackagedVersion()
	if current == "" {
		if manual {
			a.onUI(func() {
				a.updateErr = "开发版本不支持应用内自动更新"
				a.emit("update", "settings")
			})
		}
		return
	}
	exe, err := update.RunningExecutable()
	if err != nil {
		if manual {
			a.onUI(func() {
				a.updateErr = err.Error()
				a.emit("update", "settings")
			})
		}
		return
	}
	mode := update.DetectMode(runtime.GOOS, exe)
	if mode == "" {
		if manual {
			a.onUI(func() {
				a.updateErr = "当前安装方式不支持应用内自动更新"
				a.emit("update", "settings")
			})
		}
		return
	}
	var mirror string
	a.syncUI(func() {
		mirror = a.updateMirror
		a.updateErr = ""
	})
	client := update.NewClient(current)
	client.ProxyURL = browser.CurrentProxyServer()
	client.ApplyMirrorSetting(mirror)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rel, err := client.Latest(ctx)
	if err != nil {
		if manual {
			a.onUI(func() {
				a.updateErr = "检查更新失败：" + err.Error()
				a.emit("update", "settings")
			})
		}
		return
	}
	ver, err := rel.Version()
	newer, nErr := update.Newer(current, ver)
	if err != nil || nErr != nil || !newer {
		a.onUI(func() {
			a.updateNotice = ""
			if manual {
				a.pushToast("info", "当前已是最新版本")
			}
			a.emit("update", "settings")
		})
		return
	}
	skipped := ""
	if a.store != nil {
		skipped = a.store.Get(update.KeySkipped)
	}
	if !manual && skipped == ver {
		return
	}
	a.onUI(func() {
		a.updateRel = rel
		a.updateNotice = ver
		a.updateNotes = strings.TrimSpace(rel.Body)
		a.updateHidden = false
		a.updateErr = ""
		a.emit("update", "settings")
	})
}

func (a *App) tasksRunning() bool {
	if a.running || a.dedup.busy || a.fileRename.busy || a.edit.running {
		return true
	}
	for _, t := range a.dlTasks {
		if t.Phase == dlActive {
			return true
		}
	}
	return false
}

func (a *App) applyUpdate() {
	if a.updateDownloading {
		return
	}
	go a.downloadAndInstall()
}

func (a *App) maybeInstallPending() {
	if !a.updatePending || !a.updateDeferred || a.tasksRunning() {
		return
	}
	a.updateDeferred = false
	go a.downloadAndInstall()
}

func (a *App) resetUpdateTransferState() {
	a.updatePhase = ""
	a.updateFraction = 0
	a.updateDone = 0
	a.updateTotal = 0
	a.updateSpeed = 0
	a.updateEta = 0
	a.updateSource = ""
	a.updateAssetName = ""
	a.updateLastAt = time.Time{}
	a.updateProgressText = ""
}

func (a *App) applyUpdateProgressAt(now time.Time, p update.Progress) {
	a.updateDownloading = true
	a.updateLastAt = now
	if p.Name != "" {
		a.updateAssetName = p.Name
	}
	if p.Source != "" {
		a.updateSource = p.Source
	}
	switch p.Phase {
	case update.PhaseChecksums:
		a.updatePhase = "checksums"
		a.updateFraction = -1
		a.updateDone = 0
		a.updateTotal = p.Total
		a.updateSpeed = 0
		a.updateEta = 0
		a.updateProgressText = "正在获取校验清单..."
	case update.PhaseProbing:
		a.updatePhase = "probing"
		a.updateFraction = -1
		a.updateDone = 0
		a.updateTotal = p.Total
		a.updateSpeed = 0
		a.updateEta = 0
		a.updateProgressText = "正在测速选择最快的下载源..."
	case update.PhaseVerifying:
		a.updatePhase = "verifying"
		a.updateFraction = 1
		if p.Total > 0 {
			a.updateDone = p.Total
			a.updateTotal = p.Total
		} else if p.Done > 0 {
			a.updateDone = p.Done
		}
		a.updateSpeed = 0
		a.updateEta = 0
		a.updateProgressText = "下载完成，正在校验 SHA-256 完整性..."
	default:
		a.updatePhase = "downloading"
		a.updateDone = p.Done
		a.updateTotal = p.Total
		if p.Total > 0 {
			frac := float64(p.Done) / float64(p.Total)
			if frac < 0 {
				frac = 0
			} else if frac > 1 {
				frac = 1
			}
			a.updateFraction = frac
		} else {
			a.updateFraction = -1
		}
		if p.BytesPerSec > 0 {
			instant := int64(math.Round(p.BytesPerSec))
			if a.updateSpeed <= 0 {
				a.updateSpeed = instant
			} else {
				a.updateSpeed = (a.updateSpeed*2 + instant) / 3
			}
		}
		if p.Total > 0 && p.Done >= p.Total {
			a.updateSpeed = 0
			a.updateEta = 0
		} else if p.Total > 0 && p.Done < p.Total && a.updateSpeed >= 1024 {
			eta := int(math.Ceil(float64(p.Total-p.Done) / float64(a.updateSpeed)))
			if eta < 1 {
				eta = 1
			}
			a.updateEta = eta
		} else {
			a.updateEta = 0
		}
		a.updateProgressText = formatUpdateProgress(p)
	}
}

func (a *App) tickUpdateProgress(now time.Time) bool {
	if !a.updateDownloading || a.updatePhase != "downloading" || a.updateLastAt.IsZero() {
		return false
	}
	if now.Sub(a.updateLastAt) >= 2500*time.Millisecond && (a.updateSpeed > 0 || a.updateEta > 0) {
		a.updateSpeed = 0
		a.updateEta = 0
		return true
	}
	return false
}

func (a *App) updateProgressTick(done <-chan struct{}) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case now := <-ticker.C:
			a.onUI(func() {
				if a.tickUpdateProgress(now) {
					a.emit("update")
				}
			})
		}
	}
}

func (a *App) cancelUpdate() {
	if a.updateCancel != nil {
		cancel := a.updateCancel
		a.updateCancel = nil
		a.updateCancelled = true
		cancel()
	}
	a.updateDownloading = false
	a.updatePending = false
	a.updateDeferred = false
	a.updateHidden = false
	a.updateErr = ""
	a.resetUpdateTransferState()
	a.emit("update", "settings")
}

func (a *App) downloadAndInstall() {
	var running bool
	var rel update.Release
	var asset, mirror, ver string
	a.syncUI(func() {
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
			a.resetUpdateTransferState()
			a.updatePhase = "deferred"
			a.updateFraction = -1
			a.updateProgressText = update.DeferMessage
			a.emit("update", "settings")
		})
		return
	}
	current := PackagedVersion()
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
		client.ProxyURL = browser.CurrentProxyServer()
		client.ApplyMirrorSetting(mirror)
		client.OnProgress = func(p update.Progress) {
			now := time.Now()
			a.onUI(func() {
				if a.updateCancelled {
					return
				}
				a.applyUpdateProgressAt(now, p)
				a.emit("update")
			})
		}
		a.syncUI(func() {
			a.updateDownloading = true
			a.updateDeferred = false
			a.updateHidden = false
			a.updateCancelled = false
			a.updateCancel = cancel
			a.updateErr = ""
			a.resetUpdateTransferState()
			a.updatePhase = "probing"
			a.updateFraction = -1
			a.updateProgressText = "正在测速选择最快的下载源..."
			a.emit("update", "settings")
		})
		tickDone := make(chan struct{})
		go a.updateProgressTick(tickDone)
		prepared, err := client.Download(ctx, rel, current, mode, runtime.GOARCH)
		close(tickDone)
		wasCancelled := false
		a.syncUI(func() {
			a.updateCancel = nil
			wasCancelled = a.updateCancelled
			a.updateCancelled = false
		})
		if err != nil {
			if wasCancelled && errors.Is(err, context.Canceled) {
				logging.Log.Info("已取消更新下载")
				return
			}
			logging.Log.Errorf("下载更新失败: %v", err)
			a.failUpdate(err)
			return
		}
		if prepared.UpToDate {
			a.onUI(func() {
				a.updateNotice = ""
				a.updatePending = false
				a.updateDownloading = false
				a.resetUpdateTransferState()
				a.emit("update", "settings")
			})
			return
		}
		asset = prepared.Path
		if v, err := prepared.Release.Version(); err == nil {
			ver = v
		}
		a.syncUI(func() {
			a.updateAsset = asset
			running = a.tasksRunning()
			a.emit("update")
		})
	}
	if running {
		a.onUI(func() {
			a.updatePending = true
			a.updateDeferred = true
			a.updateDownloading = false
			a.resetUpdateTransferState()
			a.updatePhase = "deferred"
			a.updateFraction = -1
			a.updateProgressText = update.DeferMessage
			a.emit("update", "settings")
		})
		return
	}
	a.onUI(func() {
		a.updatePending = true
		a.updateDownloading = true
		a.updatePhase = "installing"
		a.updateFraction = 1
		a.updateSpeed = 0
		a.updateEta = 0
		a.updateProgressText = "正在准备安装 v" + ver + "..."
		a.emit("update")
	})
	if mode == update.ModeWindowsSetup && !a.confirmUpdateQuit("将启动新版本安装程序并关闭当前窗口。") {
		a.onUI(func() {
			a.updateDownloading = false
			a.updatePending = false
			a.resetUpdateTransferState()
			a.emit("update", "settings")
		})
		return
	}
	outcome, err := update.Install(update.Options{
		Mode:       mode,
		Executable: exe,
		AssetPath:  asset,
		Version:    ver,
		PID:        os.Getpid(),
	})
	if err != nil {
		logging.Log.Errorf("安装更新失败: %v", err)
		a.failUpdate(err)
		return
	}
	if outcome.Helper != "" {
		if err := update.StartDetached(outcome.Helper, outcome.HelperArgs...); err != nil {
			logging.Log.Errorf("启动更新程序失败: %v", err)
			a.failUpdate(err)
			return
		}
	}
	if outcome.OpenPath != "" {
		_ = a.openPath(outcome.OpenPath)
	}
	if err := update.SyncWindowsInstalledVersion(exe, ver); err != nil {
		logging.Log.Warning("更新已安装，但同步 Windows 应用列表版本失败: %v", err)
	}
	a.syncUI(func() {
		a.updatePending = false
		a.updateDownloading = false
		a.updateInstalled = ver
		a.resetUpdateTransferState()
		a.emit("update", "settings")
	})
	if outcome.Quit {
		if a.host != nil {
			a.host.Quit()
		}
		return
	}
	if outcome.Relaunch {
		if a.confirmUpdateRestart(ver) && a.host != nil {
			a.host.Restart()
		}
	}
}

func (a *App) confirmUpdateQuit(detail string) bool {
	return a.confirmAlert("关闭 tulai 以完成更新", detail, "关闭并更新")
}

func (a *App) confirmUpdateRestart(ver string) bool {
	return a.confirmAlert("更新已安装", fmt.Sprintf("tulai %s 已就绪，重启应用即可使用新版本。", ver), "立即重启")
}

func (a *App) failUpdate(err error) {
	a.onUI(func() {
		a.updateCancel = nil
		a.updateCancelled = false
		a.updateDownloading = false
		a.updatePending = false
		a.updateDeferred = false
		a.resetUpdateTransferState()
		a.updateErr = err.Error()
		a.emit("update", "settings")
	})
}

func (a *App) dismissUpdate() {
	if a.updateDownloading {
		a.updateHidden = true
		a.emit("update")
		return
	}
	a.updateNotice = ""
	a.updateNotes = ""
	a.updateDetailsOpen = false
	a.updateNotesOpen = false
	a.updateErr = ""
	a.emit("update", "settings")
}

func (a *App) skipUpdate() {
	ver := strings.TrimSpace(a.updateNotice)
	if a.store != nil && ver != "" {
		a.store.Set(update.KeySkipped, ver)
	}
	a.dismissUpdate()
}

func formatUpdateProgress(p update.Progress) string {
	var b strings.Builder
	b.WriteString("正在下载")
	if p.Name != "" {
		b.WriteByte(' ')
		b.WriteString(p.Name)
	}
	var meta []string
	if p.Done > 0 && p.Total > 0 {
		pct := math.Min(100, math.Max(0, float64(p.Done)*100/float64(p.Total)))
		meta = append(meta, fmt.Sprintf("%.0f%%", pct))
		meta = append(meta, formatBytes(p.Done)+" / "+formatBytes(p.Total))
	} else if p.Done > 0 {
		meta = append(meta, formatBytes(p.Done))
	}
	if p.BytesPerSec > 0 {
		meta = append(meta, formatSpeed(int64(p.BytesPerSec)))
	}
	if p.Source != "" {
		meta = append(meta, p.Source)
	}
	if len(meta) > 0 {
		b.WriteString("（")
		b.WriteString(strings.Join(meta, " · "))
		b.WriteString("）")
	}
	return b.String()
}
