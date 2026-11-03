package ui

import (
	"context"
	"path/filepath"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/download"
)

func (a *App) downloadBrowserControls(c *ui.Context, tok tokens) {
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			label := "打开下载浏览器"
			if a.dlBrowserBusy {
				label = "正在打开…"
			} else if a.dlBrowser != nil && a.dlBrowser.Alive() {
				label = "重新打开浏览器"
			}
			ui.Button(c.Key("dl-open-browser"), label).Disabled(a.dlBrowserBusy).OnClick(a.openDownloadBrowser)
			ui.Text(c, "专用指纹环境 · 登录并加载页面后开始下载。").FontSize(12).TextColor(tok.muted)
		})
		if a.dlBrowserErr != "" {
			ui.Text(c, a.dlBrowserErr).FontSize(12).TextColor(tok.danger)
		}
	})
}

func (a *App) downloadBrowserEnvironment(c *ui.Context) {
	status := "未打开"
	if a.dlBrowserBusy {
		status = "正在打开"
	} else if a.dlBrowser != nil && a.dlBrowser.Alive() {
		status = "已打开"
	}
	ui.Row(c.Key("download-browser-environment")).FillWidth().Padding(10, 14).Gap(12).
		AlignItems(ui.Center).Background(c.Theme().Background).Border(1, c.Theme().Border).Radius(8).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, "网页图片下载专用环境").FontSize(13).Bold()
			ui.Text(c, "独立保存指纹和登录状态；下载浏览器暂不支持代理。").FontSize(12).
				TextColor(c.Theme().TextMuted).SingleLine()
		})
		ui.Text(c, status).FontSize(12).TextColor(c.Theme().TextMuted).Shrink(0)
		ui.Button(c.Key("download-browser-use"), "使用下载环境").FontSize(12).Shrink(0).OnClick(func() {
			a.tab = pageDownload
			a.dlComposerOn, a.dlBrowserMode, a.dlGuideOn = true, true, false
			a.dlComposerErr, a.dlBrowserErr = "", ""
		})
	})
}

func (a *App) openDownloadBrowser() {
	if a.dlBrowserBusy {
		return
	}
	if hasQzoneCredentials(a.dlURLs) {
		a.dlBrowserErr = "QQ 相册网址包含登录令牌，请移除令牌后再打开"
		return
	}
	urls := parseDownloadURLs(a.dlURLs)
	if len(urls) != 1 {
		a.dlBrowserErr = "浏览器模式每次需要一个网址"
		return
	}
	for _, task := range a.dlTasks {
		if task.Rendered && (task.Phase == dlActive || task.Phase == dlQueued) {
			a.dlBrowserErr = "请等当前浏览器下载结束或暂停后再打开其他网址"
			return
		}
	}
	proxy := browser.CurrentProxyServer()
	if proxy != "" {
		a.dlBrowserErr = "下载浏览器暂不支持代理，请先关闭全局或系统代理后重试"
		return
	}
	root := browser.ConfigDir()
	if root == "" || a.window == nil {
		a.dlBrowserErr = "无法打开下载浏览器"
		return
	}
	old, oldCancel := a.dlBrowser, a.dlBrowserCancel
	a.dlBrowser, a.dlBrowserURL = nil, ""
	a.dlBrowserBusy, a.dlBrowserErr = true, ""
	ctx, cancel := context.WithCancel(context.Background())
	a.dlBrowserCancel = cancel
	go func() {
		if oldCancel != nil {
			oldCancel()
		}
		if old != nil {
			old.Close()
		}
		session, err := download.OpenRendered(ctx, filepath.Join(root, "DownloadBrowser"), urls[0], proxy, false)
		if session != nil {
			context.AfterFunc(ctx, session.Close)
			go func() {
				<-session.Done()
				session.Close()
				a.postUI(func() {
					if a.dlBrowser == session {
						a.dlBrowser, a.dlBrowserURL = nil, ""
						if a.dlBrowserCancel != nil {
							a.dlBrowserCancel()
							a.dlBrowserCancel = nil
						}
						a.dlBrowserErr = "下载专用浏览器已关闭，请重新打开"
					}
				})
			}()
		}
		if ctx.Err() != nil {
			return
		}
		a.dlRun(func() {
			if ctx.Err() != nil {
				return
			}
			a.dlBrowserBusy = false
			if err != nil {
				a.dlBrowserErr = err.Error()
				a.dlBrowserCancel = nil
				cancel()
				return
			}
			if !session.Alive() {
				a.dlBrowserErr = "下载专用浏览器已关闭，请重新打开"
				a.dlBrowserCancel = nil
				cancel()
				return
			}
			a.dlBrowser, a.dlBrowserURL = session, urls[0]
		})
	}()
}
