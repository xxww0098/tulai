package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/camoufox"
)

// applyCamoufoxProgress 把安装回调写进界面状态。回调来自后台安装协程，
// 调用方要先用 syncUI 送到窗口线程。
func (a *App) applyCamoufoxProgress(p camoufox.Progress) {
	a.applyCamoufoxProgressAt(time.Now(), p)
}

func (a *App) applyCamoufoxProgressAt(now time.Time, p camoufox.Progress) {
	if p.Text != "" {
		a.camoufoxStatus = p.Text
	}
	if p.Stage != "" && p.Stage != a.camoufoxStage {
		a.camoufoxStage = p.Stage
		a.camoufoxPercent = -1
		a.camoufoxDone, a.camoufoxTotal = 0, 0
		a.camoufoxSpeed, a.camoufoxLast = 0, 0
		a.camoufoxAt = time.Time{}
	}
	if p.Done > 0 || p.Total > 0 {
		a.camoufoxDone, a.camoufoxTotal = p.Done, p.Total
		if p.Total > 0 {
			a.camoufoxPercent = float64(p.Done) / float64(p.Total)
			if a.camoufoxPercent > 1 {
				a.camoufoxPercent = 1
			}
		} else {
			a.camoufoxPercent = -1
		}
		a.trackCamoufoxSpeed(now, p.Done)
	}
	if p.Total > 0 && p.Done >= p.Total {
		a.camoufoxSpeed = 0
	}
}

// trackCamoufoxSpeed 用两次进度之间的字节差算瞬时速度，再和上一次平滑。
func (a *App) trackCamoufoxSpeed(now time.Time, done int64) {
	if a.camoufoxAt.IsZero() {
		a.camoufoxAt, a.camoufoxLast = now, done
		return
	}
	dt := now.Sub(a.camoufoxAt)
	if dt < 300*time.Millisecond {
		return
	}
	if delta := done - a.camoufoxLast; delta >= 0 {
		instant := int64(float64(delta) / dt.Seconds())
		if a.camoufoxSpeed == 0 {
			a.camoufoxSpeed = instant
		} else {
			a.camoufoxSpeed = (a.camoufoxSpeed*2 + instant) / 3
		}
	}
	a.camoufoxAt, a.camoufoxLast = now, done
}

// tickCamoufoxProgress 在长时间没有新字节时把速度归零，界面不会一直显示旧速度。
func (a *App) tickCamoufoxProgress(now time.Time) {
	if !a.camoufoxBusy || a.camoufoxSpeed <= 0 || a.camoufoxAt.IsZero() {
		return
	}
	if now.Sub(a.camoufoxAt) >= 2*time.Second {
		a.camoufoxSpeed = 0
	}
}

// camoufoxTick 在安装期间每秒检查一次速度是否该归零，装完就退出。
func (a *App) camoufoxTick(done <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case now := <-ticker.C:
			a.syncUI(func() { a.tickCamoufoxProgress(now) })
		}
	}
}

func (a *App) camoufoxPercentLabel() string {
	if !a.camoufoxBusy || a.camoufoxPercent < 0 || a.camoufoxTotal <= 0 {
		return ""
	}
	p := int(float64(a.camoufoxDone) / float64(a.camoufoxTotal) * 100)
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	return fmt.Sprintf("%d%%", p)
}

func (a *App) camoufoxFraction() float64 {
	if a.camoufoxPercent < 0 {
		return 0
	}
	if a.camoufoxPercent > 1 {
		return 1
	}
	return a.camoufoxPercent
}

// camoufoxProgressDetail 是进度条下面的速度、已下载量和剩余时间。
func (a *App) camoufoxProgressDetail() string {
	if !a.camoufoxBusy || (a.camoufoxDone <= 0 && a.camoufoxTotal <= 0) {
		return ""
	}
	parts := []string{formatBytes(a.camoufoxSpeed) + "/s"}
	if a.camoufoxTotal > 0 {
		parts = append(parts, fmt.Sprintf("已下载 %s / %s", formatBytes(a.camoufoxDone), formatBytes(a.camoufoxTotal)))
		if a.camoufoxSpeed > 0 && a.camoufoxDone < a.camoufoxTotal {
			if seconds := (a.camoufoxTotal - a.camoufoxDone) / a.camoufoxSpeed; seconds >= 1 {
				parts = append(parts, "剩余 "+formatETA(seconds))
			}
		}
	} else {
		parts = append(parts, "已下载 "+formatBytes(a.camoufoxDone))
	}
	return strings.Join(parts, " · ")
}

// camoufoxCompactDetail 是账号页窄卡片里的百分比和速度。
func (a *App) camoufoxCompactDetail() string {
	if !a.camoufoxBusy {
		return ""
	}
	speed := formatBytes(a.camoufoxSpeed) + "/s"
	if label := a.camoufoxPercentLabel(); label != "" {
		return label + " · " + speed
	}
	if a.camoufoxDone <= 0 {
		return ""
	}
	return "已下载 " + formatBytes(a.camoufoxDone) + " · " + speed
}

// camoufoxProgressBlock 是向导里的完整进度：状态和百分比、进度条、速度和剩余时间。
func (a *App) camoufoxProgressBlock(c *ui.Context, key string) {
	tok := a.look
	ui.Column(c.Key(key + "-box")).FillWidth().Gap(5).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, a.camoufoxStatus).FontSize(tok.body).TextColor(tok.muted).Grow(1).MinWidth(0)
			if label := a.camoufoxPercentLabel(); label != "" {
				ui.Text(c, label).FontSize(tok.body).Bold().TextColor(tok.accent).Shrink(0)
			}
		})
		motionProgress(c, key, a.camoufoxFraction(), a.camoufoxPercent < 0)
		if detail := a.camoufoxProgressDetail(); detail != "" {
			ui.Text(c, detail).FontSize(tok.caption).TextColor(tok.muted).SingleLine()
		}
	})
}

// camoufoxCompactProgress 是账号页里的进度条和一行百分比、速度。
func (a *App) camoufoxCompactProgress(c *ui.Context, key string) {
	tok := a.look
	motionProgress(c, key, a.camoufoxFraction(), a.camoufoxPercent < 0)
	if detail := a.camoufoxCompactDetail(); detail != "" {
		ui.Text(c, detail).FontSize(tok.caption).TextColor(tok.muted).SingleLine()
	}
}
