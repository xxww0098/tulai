package captcha

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/xxww0098/tulai/internal/cdp"
)

// playSliderX11 用本机 DISPLAY 上的 xdotool 发真实鼠标事件。
// 阿里云对 CDP Input.dispatchMouseEvent 有时给 SP6GQr；OS 级拖动 isTrusted 且不带 CDP 指纹。
func playSliderX11(ctx context.Context, conn *cdp.SliderConn, hit cdp.SliderHit, events []cdp.SliderEvent) error {
	display := strings.TrimSpace(os.Getenv("DISPLAY"))
	if display == "" {
		return fmt.Errorf("没有 DISPLAY")
	}
	if _, err := exec.LookPath("xdotool"); err != nil {
		return fmt.Errorf("没有 xdotool")
	}
	screen, chromeTop, err := pageScreenOrigin(ctx, conn, hit.TargetID)
	if err != nil {
		return err
	}
	// 找千问窗口并前置
	_ = exec.CommandContext(ctx, "xdotool", "search", "--name", "千问").Run()
	widOut, _ := exec.CommandContext(ctx, "bash", "-c", "xdotool search --name '千问' | head -1").CombinedOutput()
	wid := strings.TrimSpace(string(widOut))
	if wid != "" {
		_ = exec.CommandContext(ctx, "xdotool", "windowactivate", "--sync", wid).Run()
	}
	for _, ev := range events {
		if ev.Wait > 0 {
			if err := sleepCtx(ctx, ev.Wait); err != nil {
				return err
			}
		}
		x := int(math.Round(screen.X + ev.X))
		y := int(math.Round(screen.Y + chromeTop + ev.Y))
		switch ev.Type {
		case "mouseMoved":
			if err := runXDo(ctx, "mousemove", "--sync", strconv.Itoa(x), strconv.Itoa(y)); err != nil {
				return err
			}
		case "mousePressed":
			if err := runXDo(ctx, "mousemove", "--sync", strconv.Itoa(x), strconv.Itoa(y)); err != nil {
				return err
			}
			if err := runXDo(ctx, "mousedown", "1"); err != nil {
				return err
			}
		case "mouseReleased":
			if err := runXDo(ctx, "mousemove", "--sync", strconv.Itoa(x), strconv.Itoa(y)); err != nil {
				return err
			}
			if err := runXDo(ctx, "mouseup", "1"); err != nil {
				return err
			}
		}
	}
	return nil
}

type screenOrigin struct {
	X float64
	Y float64
}

func pageScreenOrigin(ctx context.Context, conn *cdp.SliderConn, targetID string) (screenOrigin, float64, error) {
	// 通过 Look 已附着的会话拿 window.screenX/Y 与外壳高度。
	type metrics struct {
		ScreenX float64 `json:"screenX"`
		ScreenY float64 `json:"screenY"`
		OuterH  float64 `json:"outerH"`
		InnerH  float64 `json:"innerH"`
	}
	var m metrics
	err := conn.EvalJSON(ctx, targetID, `({
		screenX: window.screenX||0,
		screenY: window.screenY||0,
		outerH: window.outerHeight||0,
		innerH: window.innerHeight||0
	})`, &m)
	if err != nil {
		return screenOrigin{}, 0, err
	}
	top := m.OuterH - m.InnerH
	if top < 0 {
		top = 0
	}
	return screenOrigin{X: m.ScreenX, Y: m.ScreenY}, top, nil
}

func runXDo(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "xdotool", args...)
	cmd.Env = append(os.Environ(), "DISPLAY="+strings.TrimSpace(os.Getenv("DISPLAY")))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("xdotool %v: %w (%s)", args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// x11DragEnabled 默认在有 DISPLAY 时优先 OS 拖动；TULAI_BAXIA_X11=0 关掉。
func x11DragEnabled() bool {
	if strings.TrimSpace(os.Getenv("TULAI_BAXIA_X11")) == "0" {
		return false
	}
	if strings.TrimSpace(os.Getenv("DISPLAY")) == "" {
		return false
	}
	_, err := exec.LookPath("xdotool")
	return err == nil
}
