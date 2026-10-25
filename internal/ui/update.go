package ui

import (
	"context"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"xbs/internal/logging"
	"xbs/internal/update"
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

// checkForUpdates asks the public GitHub release API and, after confirmation,
// downloads the asset for this system.
func (a *App) checkForUpdates() {
	if a.updateBusy {
		return
	}
	a.updateBusy = true
	go func() {
		defer a.onUI(func() { a.updateBusy = false })
		a.runUpdateCheck()
	}()
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

func (a *App) runUpdateCheck() {
	current := mygo.App.Version()
	if current == "" {
		a.tellUpdate(mygo.MessageError, "检查更新失败", "没有当前版本")
		return
	}
	logging.Log.Info("正在检查更新（当前 %s）", current)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	client := update.NewClient(current)
	rel, err := client.Latest(ctx)
	if err != nil {
		logging.Log.Errorf("检查更新失败: %v", err)
		a.tellUpdate(mygo.MessageError, "检查更新失败", err.Error())
		return
	}
	latest, err := rel.Version()
	if err != nil {
		logging.Log.Errorf("检查更新失败: %v", err)
		a.tellUpdate(mygo.MessageError, "检查更新失败", err.Error())
		return
	}
	newer, err := update.Newer(current, latest)
	if err != nil {
		logging.Log.Errorf("检查更新失败: %v", err)
		a.tellUpdate(mygo.MessageError, "检查更新失败", err.Error())
		return
	}
	if !newer {
		logging.Log.Info("已是最新版本 %s", current)
		a.tellUpdate(mygo.MessageInfo, "已是最新版本", "当前版本 "+current)
		return
	}
	var running bool
	a.onUI(func() { running = a.running })
	if running {
		logging.Log.Info("发现新版本 %s，任务进行中，未安装", latest)
		a.tellUpdate(mygo.MessageInfo, "发现新版本 "+latest, "请先停止任务，再从「帮助」菜单或「检查更新」安装。")
		return
	}
	notes := strings.TrimSpace(rel.Body)
	if notes != "" {
		notes = trimRunes(notes, 1200) + "\n\n"
	}
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:       a.window,
		Type:         mygo.MessageQuestion,
		Title:        "检查更新",
		Message:      "发现新版本 " + latest,
		Detail:       notes + update.UnsignedWarning,
		Buttons:      []string{"下载并安装", "稍后"},
		CancelButton: 1,
	})
	if err != nil || res.Button != 0 {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		a.tellUpdate(mygo.MessageError, "安装失败", err.Error())
		return
	}
	mode := update.DetectMode(runtime.GOOS, exe)
	logging.Log.Info("正在下载 %s", latest)
	prepared, err := client.Download(ctx, rel, current, mode, runtime.GOARCH)
	if err != nil {
		logging.Log.Errorf("下载更新失败: %v", err)
		a.tellUpdate(mygo.MessageError, "下载更新失败", err.Error())
		return
	}
	if prepared.UpToDate {
		a.tellUpdate(mygo.MessageInfo, "已是最新版本", "当前版本 "+current)
		return
	}
	out, err := update.Install(update.Options{
		Mode:       mode,
		Executable: exe,
		AssetPath:  prepared.Path,
		Version:    latest,
	})
	if err != nil {
		logging.Log.Errorf("安装更新失败: %v", err)
		a.tellUpdate(mygo.MessageError, "安装更新失败", err.Error())
		return
	}
	logging.Log.Success("%s", firstLine(out.Message))
	buttons := []string{"好"}
	if out.Relaunch {
		buttons = []string{"重新启动", "稍后"}
	} else if out.Quit {
		buttons = []string{"退出并完成", "取消"}
	}
	done, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:       a.window,
		Type:         mygo.MessageInfo,
		Title:        "检查更新",
		Message:      "版本 " + latest,
		Detail:       out.Message,
		Buttons:      buttons,
		CancelButton: cancelIndex(buttons),
	})
	if err != nil || done.Button != 0 {
		return
	}
	if out.Helper != "" {
		if err := update.StartDetached(out.Helper, out.HelperArgs...); err != nil {
			a.tellUpdate(mygo.MessageError, "安装失败", err.Error())
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

func trimRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func cancelIndex(buttons []string) int {
	if len(buttons) > 1 {
		return len(buttons) - 1
	}
	return 0
}
