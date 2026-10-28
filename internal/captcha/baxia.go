package captcha

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "golang.org/x/image/webp"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/logging"
)

var (
	errGapSize       = errors.New("拼图尺寸不够")
	errBaxiaDisabled = errors.New("已关闭自动滑块")
	errBaxiaNoCDP    = errors.New("没有连上 Chrome 远程调试端口")
	errBaxiaNoSlider = errors.New("页面上没有识别到阿里云滑块")
	errBaxiaRemain   = errors.New("拖动后滑块仍在")
	errBaxiaLow      = errors.New("缺口置信度不够")
)

// SolveQianwenSlider 附着到已经打开远程调试的 Chrome，用信任的鼠标事件拖过阿里云滑块。
// 拼图用本地缺口匹配；没有拼图图时把滑块拖到轨道尽头。页面脚本合成的点击是 isTrusted=false，这里不用那条路。
func SolveQianwenSlider(ctx context.Context) error {
	if strings.TrimSpace(os.Getenv("TULAI_BAXIA_SOLVE")) == "0" {
		return errBaxiaDisabled
	}
	endpoints := sliderEndpoints()
	if len(endpoints) == 0 {
		return errBaxiaNoCDP
	}
	connected := false
	var last error
	for _, endpoint := range endpoints {
		err := SolveQianwenSliderAt(ctx, endpoint)
		if err == nil {
			return nil
		}
		if errors.Is(err, errBaxiaRemain) || errors.Is(err, errBaxiaLow) || errors.Is(err, errGapSize) {
			return err
		}
		if errors.Is(err, errBaxiaNoSlider) {
			connected = true
		}
		last = err
	}
	if connected {
		return errBaxiaNoSlider
	}
	if last == nil {
		return errBaxiaNoCDP
	}
	return fmt.Errorf("%w: %v", errBaxiaNoCDP, last)
}

// SolveQianwenSliderAt 只连一个调试地址。测试和显式 TULAI_CDP_URL 走这里。
func SolveQianwenSliderAt(ctx context.Context, endpoint string) error {
	conn, err := cdp.DialSlider(ctx, endpoint)
	if err != nil {
		return err
	}
	defer conn.Close()
	var hit cdp.SliderHit
	deadline := time.Now().Add(4 * time.Second)
	for {
		look, err := conn.Look(ctx)
		if err != nil {
			return err
		}
		if look.Passed {
			logging.Log.Info("千问滑块已经通过")
			return nil
		}
		if look.Found {
			hit = look.Hit
			break
		}
		if !time.Now().Before(deadline) {
			return errBaxiaNoSlider
		}
		if err := sleepCtx(ctx, 200*time.Millisecond); err != nil {
			return err
		}
	}
	events, kind, dx, err := planSliderDrag(ctx, hit, time.Now().UnixNano())
	if err != nil {
		return err
	}
	logging.Log.Info("千问滑块 %s，计划拖动 %.1fpx", kind, dx)
	if x11DragEnabled() {
		logging.Log.Info("千问滑块改用 X11/xdotool 拖动")
		if err := playSliderX11(ctx, conn, hit, events); err != nil {
			logging.Log.Warning("X11 拖动失败，回退 CDP: %v", err)
			if err := conn.Play(ctx, hit.TargetID, events); err != nil {
				return err
			}
		}
	} else if err := conn.Play(ctx, hit.TargetID, events); err != nil {
		return err
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		look, err := conn.Look(ctx)
		if err != nil {
			return err
		}
		if look.Passed || !look.Found {
			logging.Log.Success("千问滑块已通过")
			return nil
		}
		if !time.Now().Before(deadline) {
			return errBaxiaRemain
		}
		if err := sleepCtx(ctx, 250*time.Millisecond); err != nil {
			return err
		}
	}
}

func planSliderDrag(ctx context.Context, hit cdp.SliderHit, seed int64) ([]cdp.SliderEvent, string, float64, error) {
	cx := hit.Handle.X + hit.Handle.W/2
	cy := hit.Handle.Y + hit.Handle.H/2
	if hit.Bg != nil && hit.Piece != nil {
		bgImg, err := decodeSliderImage(ctx, hit.Bg)
		pieceImg, pieceErr := decodeSliderImage(ctx, hit.Piece)
		if err == nil && pieceErr == nil {
			gap, gapErr := FindPuzzleGap(bgImg, pieceImg)
			if gapErr != nil {
				return nil, "", 0, gapErr
			}
			if gap.Confidence < 0.25 {
				return nil, "", 0, fmt.Errorf("%w（%.2f）", errBaxiaLow, gap.Confidence)
			}
			naturalW := float64(hit.Bg.NaturalW)
			if naturalW <= 1 {
				naturalW = float64(bgImg.Bounds().Dx())
			}
			displayW := hit.Bg.W
			if displayW <= 1 {
				displayW = naturalW
			}
			pieceW := hit.Piece.W
			if pieceW <= 1 {
				pieceW = float64(pieceImg.Bounds().Dx()) * displayW / naturalW
			}
			dx := PuzzleTravel(float64(gap.X), naturalW, displayW, hit.PieceLeft, displayW-pieceW, SlideTravel(hit.Track, hit.Handle))
			if dx < 4 || dx > 700 {
				return nil, "", 0, fmt.Errorf("%w: 距离 %.1f", errBaxiaLow, dx)
			}
			return DragPlan(cx, cy, dx, seed), "puzzle", dx, nil
		}
	}
	dx := SlideTravel(hit.Track, hit.Handle)
	if dx < 30 || dx > 800 {
		return nil, "", 0, fmt.Errorf("%w: 轨道距离 %.1f", errBaxiaNoSlider, dx)
	}
	return DragPlan(cx, cy, dx, seed), "slide", dx, nil
}

func decodeSliderImage(ctx context.Context, img *cdp.SliderImage) (image.Image, error) {
	if img == nil {
		return nil, errors.New("没有图片")
	}
	if len(img.PNG) > 0 {
		decoded, _, err := image.Decode(bytes.NewReader(img.PNG))
		return decoded, err
	}
	src := strings.TrimSpace(img.Src)
	if strings.HasPrefix(src, "data:image") {
		comma := strings.Index(src, ",")
		if comma < 0 {
			return nil, errors.New("图片 data URL 不完整")
		}
		raw, err := decodeData(src[comma+1:])
		if err != nil {
			return nil, err
		}
		decoded, _, err := image.Decode(bytes.NewReader(raw))
		return decoded, err
	}
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
		if err != nil {
			return nil, err
		}
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if err != nil {
			return nil, err
		}
		decoded, _, err := image.Decode(bytes.NewReader(raw))
		return decoded, err
	}
	return nil, errors.New("拼图没有可解码的图片")
}

func decodeData(s string) ([]byte, error) {
	if raw, err := base64.StdEncoding.DecodeString(s); err == nil {
		return raw, nil
	}
	if raw, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return raw, nil
	}
	return nil, errors.New("图片 data URL 无法解码")
}

func sliderEndpoints() []string {
	if raw := strings.TrimSpace(os.Getenv("TULAI_CDP_URL")); raw != "" {
		return []string{raw}
	}
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		for _, item := range out {
			if item == v {
				return
			}
		}
		out = append(out, v)
	}
	if port := strings.TrimSpace(os.Getenv("TULAI_CDP_PORT")); port != "" {
		add("http://127.0.0.1:" + port)
	}
	if ws := devtoolsEndpoint(os.Getenv("TULAI_CHROME_PROFILE")); ws != "" {
		add(ws)
	}
	if ws := devtoolsEndpoint("/tmp/tulai-chrome-profile"); ws != "" {
		add(ws)
	}
	add("http://127.0.0.1:9222")
	return out
}

func devtoolsEndpoint(profile string) string {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return ""
	}
	port := strings.TrimSpace(lines[0])
	if _, err := strconv.Atoi(port); err != nil {
		return ""
	}
	if len(lines) >= 2 && strings.HasPrefix(strings.TrimSpace(lines[1]), "/") {
		return "ws://127.0.0.1:" + port + strings.TrimSpace(lines[1])
	}
	return "http://127.0.0.1:" + port
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
