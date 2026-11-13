package ui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

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
				label = "打开填写的网址"
			}
			ui.Button(c.Key("dl-open-browser"), label).Shrink(0).Disabled(a.dlBrowserBusy).OnClick(a.openDownloadBrowser)
			ui.Text(c, "专用环境：可先登录，填写网址后打开并下载。").FontSize(12).TextColor(tok.muted).Grow(1).MinWidth(0)
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
	ui.Row(c.Key("download-browser-environment")).FillWidth().Padding(8, 10).Gap(8).
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
	if len(urls) != 1 && strings.TrimSpace(a.dlURLs) != "" {
		a.dlBrowserErr = "浏览器模式每次需要一个网址"
		return
	}
	target := ""
	if len(urls) == 1 {
		target = urls[0]
	}
	a.openDownloadBrowserURL(target)
}

func (a *App) openDownloadBrowserURL(target string) {
	if a.dlBrowserBusy {
		return
	}
	if hasQzoneCredentials(target) {
		a.dlBrowserErr = "QQ 相册网址包含登录令牌，请移除令牌后再打开"
		return
	}
	for _, task := range a.dlTasks {
		if task.Rendered && (task.Phase == dlActive || task.Phase == dlQueued) {
			a.dlBrowserErr = "请等当前浏览器下载结束或暂停后再打开其他网址"
			return
		}
	}
	proxy, err := browser.ResolveCurrentProxy()
	if err != nil {
		a.dlBrowserErr = err.Error()
		return
	}
	if proxy != "" {
		a.dlBrowserErr = "下载浏览器暂不支持代理，请先关闭全局或系统代理后重试"
		return
	}
	if session := a.dlBrowser; session != nil && session.Alive() {
		if target == "" {
			a.dlBrowserErr = "浏览器已打开，请填写一个网址后点击打开"
			return
		}
		a.dlBrowserBusy, a.dlBrowserErr, a.dlBrowserURL = true, "", ""
		go func() {
			err := session.OpenTarget(context.Background(), target)
			a.postUI(func() {
				if a.dlBrowser != session {
					return
				}
				a.dlBrowserBusy = false
				if err != nil {
					a.dlBrowserErr = err.Error()
					return
				}
				a.dlBrowserURL = target
			})
		}()
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
		session, err := download.OpenRenderedAssisted(ctx, filepath.Join(root, "DownloadBrowser"), target, proxy, false,
			func(ctx context.Context, session *download.RenderedSession, rawURL, marker string) error {
				return a.downloadBrowserRequest(ctx, session, rawURL, marker)
			})
		if session != nil {
			context.AfterFunc(ctx, session.Close)
			go func() {
				<-session.Done()
				session.Close()
				a.postUI(func() {
					if a.dlBrowser == session {
						a.dlBrowser, a.dlBrowserURL = nil, ""
						a.dlBrowserBusy = false
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
			a.dlBrowser, a.dlBrowserURL = session, target
		})
	}()
}

func (a *App) downloadBrowserRequest(ctx context.Context, session downloadBrowser, rawURL, marker string) error {
	result := make(chan error, 1)
	a.postUI(func() {
		if err := ctx.Err(); err != nil {
			result <- err
			return
		}
		if a.dlBrowser != session || !session.Alive() || a.dlBrowserBusy {
			result <- errors.New("下载浏览器尚未就绪，请稍后重试")
			return
		}
		var existing *downloadTask
		for _, task := range a.dlTasks {
			if task.Rendered && (task.Phase == dlActive || task.Phase == dlQueued) {
				result <- errors.New("请等待当前浏览器下载结束或暂停")
				return
			}
			if task.URL == rawURL {
				if !task.Rendered || (!task.Phase.terminal() && task.Phase != dlPaused) {
					result <- errors.New("该页面已有下载任务，请在 tulai 中查看")
					return
				}
				existing = task
			}
		}
		if err := a.downloadImageFilter().Validate(); err != nil {
			result <- err
			return
		}
		if !ensureDownloadDir(a.downloadDir()) {
			result <- errors.New("无法创建保存目录")
			return
		}
		a.dlBrowserBusy, a.dlBrowserURL, a.dlBrowserErr = true, "", ""
		go func() {
			err := session.BindCurrent(ctx, rawURL, marker)
			a.postUI(func() {
				if a.dlBrowser != session {
					result <- errors.New("下载浏览器已关闭，请重新打开")
					return
				}
				a.dlBrowserBusy = false
				if err == nil {
					err = ctx.Err()
				}
				if err != nil {
					a.dlBrowserErr = err.Error()
					result <- err
					return
				}
				a.dlBrowserURL = rawURL
				if existing != nil {
					found := false
					for _, task := range a.dlTasks {
						if task == existing {
							found = true
						}
						if task.Rendered && (task.Phase == dlActive || task.Phase == dlQueued) {
							result <- errors.New("请等待当前浏览器下载结束或暂停")
							return
						}
					}
					if !found || (!existing.Phase.terminal() && existing.Phase != dlPaused) {
						result <- errors.New("下载任务已变化，请重新点击")
						return
					}
					a.dlFilter = dlFilterOpen
					if existing.Phase == dlPaused {
						a.resumeTask(existing)
					} else {
						a.requeueDownloadTask(existing, false)
					}
					result <- nil
					return
				}
				oldMode := a.dlBrowserMode
				a.dlBrowserMode = true
				added, _ := a.addDownloadURLs(rawURL)
				a.dlBrowserMode = oldMode
				if added == 0 {
					result <- errors.New("无法加入下载列表，请检查当前网址")
					return
				}
				result <- nil
			})
		}()
	})
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
