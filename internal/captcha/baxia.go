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
	"strings"
	"time"

	_ "golang.org/x/image/webp"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/logging"
)

var (
	errGapSize       = errors.New("拼图尺寸不够")
	errBaxiaDisabled = errors.New("已关闭自动滑块")
	errBaxiaNoCDP    = errors.New("没有打开 Camoufox 账号窗口")
	errBaxiaNoSlider = errors.New("页面上没有识别到阿里云滑块")
	errBaxiaRemain   = errors.New("拖动后滑块仍在")
	errBaxiaLow      = errors.New("缺口置信度不够")
)

// SolveQianwenSlider 附着到应用内已打开的 Camoufox 账号，用信任的鼠标事件拖过阿里云滑块。
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

// SolveQianwenSliderAt 只连接指定的 Camoufox 账号。
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
	for _, img := range []*cdp.SliderImage{hit.Bg, hit.Piece} {
		if err := conn.CaptureImage(ctx, hit.TargetID, img); err != nil {
			return fmt.Errorf("读取验证码图片: %w", err)
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
			logging.Log.Warning("X11 拖动失败，回退浏览器输入: %v", err)
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
	raw := strings.TrimSpace(os.Getenv("TULAI_CAMOUFOX_ACCOUNT"))
	if raw != "" {
		return []string{"camoufox://" + raw}
	}
	return nil
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
