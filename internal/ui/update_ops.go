package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"

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
	a.emit("update")
	go func() {
		defer a.onUI(func() {
			a.updateBusy = false
			a.emit("update")
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
	a.onUI(func() {
		auto = a.autoCheck
		busy = a.updateBusy
	})
	if !auto || busy {
		return
	}
	a.onUI(func() { a.updateBusy = true })
	defer a.onUI(func() {
		a.updateBusy = false
		a.emit("update")
	})
	a.runUpdateCheck(false)
}

func (a *App) runUpdateCheck(manual bool) {
	current := strings.TrimSpace(core.App().Version)
	if current == "" {
		if manual {
			a.onUI(func() {
				a.updateErr = "开发版本不支持应用内自动更新"
				a.emit("update")
			})
		}
		return
	}
	exe, err := update.RunningExecutable()
	if err != nil {
		if manual {
			a.onUI(func() {
				a.updateErr = err.Error()
				a.emit("update")
			})
		}
		return
	}
	mode := update.DetectMode(runtime.GOOS, exe)
	if mode == "" {
		if manual {
			a.onUI(func() {
				a.updateErr = "当前安装方式不支持应用内自动更新"
				a.emit("update")
			})
		}
		return
	}
	var mirror string
	a.onUI(func() {
		mirror = a.updateMirror
		a.updateErr = ""
	})
	client := update.NewClient(current)
	client.ApplyMirrorSetting(mirror)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rel, err := client.Latest(ctx)
	if err != nil {
		if manual {
			a.onUI(func() {
				a.updateErr = "检查更新失败：" + err.Error()
				a.emit("update")
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
			a.emit("update")
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
		a.emit("update")
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
			a.emit("update")
		})
		return
	}
	current := strings.TrimSpace(core.App().Version)
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
				a.emit("update")
			})
		}
		a.onUI(func() {
			a.updateDownloading = true
			a.updateProgressText = "正在下载 " + ver
			a.updateFraction = 0
			a.updateErr = ""
			a.emit("update")
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
				a.emit("update")
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
			a.emit("update")
		})
	}
	if running {
		a.onUI(func() {
			a.updatePending = true
			a.updateDeferred = true
			a.updateDownloading = false
			a.updateProgressText = update.DeferMessage
			a.emit("update")
		})
		return
	}
	a.onUI(func() {
		a.updatePending = true
		a.updateProgressText = "正在准备安装 " + ver
		a.emit("update")
	})
	if mode == update.ModeWindowsSetup && !a.confirmUpdateQuit("将启动新版本安装程序并关闭当前窗口。") {
		a.onUI(func() {
			a.updateDownloading = false
			a.updateProgressText = ""
			a.updatePending = false
			a.emit("update")
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
	a.onUI(func() {
		a.updatePending = false
		a.updateDownloading = false
		a.updateInstalled = ver
		a.emit("update")
		if outcome.Quit {
			if a.scope != nil {
				a.scope.Quit()
			}
			return
		}
		if outcome.Relaunch {
			if a.confirmUpdateRestart(ver) && a.scope != nil {
				a.scope.Restart()
			}
		}
	})
}

func (a *App) confirmUpdateQuit(detail string) bool {
	return a.confirmAlert("关闭 tulai 以完成更新", detail, "关闭并更新")
}

func (a *App) confirmUpdateRestart(ver string) bool {
	return a.confirmAlert("更新已安装", fmt.Sprintf("tulai %s 已就绪，重启应用即可使用新版本。", ver), "立即重启")
}

func (a *App) failUpdate(err error) {
	a.onUI(func() {
		a.updateDownloading = false
		a.updatePending = false
		a.updateDeferred = false
		a.updateProgressText = ""
		a.updateErr = err.Error()
		a.emit("update")
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
	a.emit("update")
}

func (a *App) skipUpdate() {
	ver := strings.TrimSpace(a.updateNotice)
	if a.store != nil && ver != "" {
		a.store.Set(update.KeySkipped, ver)
	}
	a.dismissUpdate()
}
