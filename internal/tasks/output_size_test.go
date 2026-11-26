package tasks

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/strategy"
)

func TestOutputSizeValidationAndPrompt(t *testing.T) {
	for _, tc := range []struct {
		size      OutputSize
		wantError bool
	}{
		{OutputSize{}, false},
		{OutputSize{Enabled: true, Ratio: "5：4"}, false},
		{OutputSize{Ratio: "12000:12000"}, false},
		{OutputSize{Enabled: true, Width: 1200, Height: 960}, false},
		{OutputSize{Enabled: true}, true},
		{OutputSize{Ratio: "5:0"}, true},
		{OutputSize{Ratio: "1:1", Width: 12, Height: 12}, true},
		{OutputSize{Width: 1200}, true},
		{OutputSize{Width: 10000, Height: 10000}, true},
	} {
		if got := tc.size.Validate(); (got != nil) != tc.wantError {
			t.Errorf("%+v: %v", tc.size, got)
		}
	}
	size := OutputSize{Width: 1200, Height: 960}
	for _, platform := range []string{"豆包", "千问", "千问国际"} {
		if got := size.prompt(platform, "蓝色商品"); !strings.Contains(got, "5:4") || !strings.Contains(got, "蓝色商品") {
			t.Fatalf("%s wrong ratio prompt: %q", platform, got)
		}
	}
	if got := size.prompt("其他", "蓝色商品"); got != "蓝色商品" {
		t.Fatalf("other platforms must keep their prompt: %q", got)
	}
}

func outputMarkerPNG(t *testing.T, marker color.NRGBA) []byte {
	t.Helper()
	decoded, err := png.Decode(bytes.NewReader(resultPNG(t, marker.R+marker.B)))
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(decoded.Bounds())
	draw.Draw(img, img.Bounds(), decoded, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(48, 48, 80, 80), &image.Uniform{marker}, image.Point{}, draw.Src)
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestOutputSizePixelsRatioAndFit(t *testing.T) {
	data := outputMarkerPNG(t, color.NRGBA{R: 220, A: 255})
	cfg := image.Config{Width: 128, Height: 128}
	for _, tc := range []struct {
		name      string
		size      OutputSize
		w, h      int
		whiteEdge bool
	}{
		{"1200x960", OutputSize{Width: 1200, Height: 960}, 1200, 960, true},
		{"pixels pad", OutputSize{Width: 200, Height: 100}, 200, 100, true},
		{"pixels crop", OutputSize{Width: 200, Height: 100, Crop: true}, 200, 100, false},
		{"ratio pad", OutputSize{Ratio: "5:4"}, 160, 128, true},
		{"ratio crop", OutputSize{Ratio: "5:4", Crop: true}, 125, 100, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tc.size.process(context.Background(), data, cfg)
			if err != nil {
				t.Fatal(err)
			}
			img, err := png.Decode(bytes.NewReader(out))
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds().Dx() != tc.w || img.Bounds().Dy() != tc.h {
				t.Fatalf("got dimensions %v", img.Bounds())
			}
			r, g, b, _ := img.At(tc.w/2, tc.h/2).RGBA()
			if r < 50000 || g > 1000 || b > 1000 {
				t.Fatalf("center subject changed: %d %d %d", r, g, b)
			}
			if tc.whiteEdge {
				r, g, b, a := img.At(0, tc.h/2).RGBA()
				if r != 65535 || g != 65535 || b != 65535 || a != 65535 {
					t.Fatalf("missing white padding: %d %d %d %d", r, g, b, a)
				}
			}
		})
	}
}

func TestOutputSizeNativeRatio(t *testing.T) {
	for _, tc := range []struct {
		name     string
		platform string
		size     OutputSize
		want     string
	}{
		{"doubao native", "豆包", OutputSize{Ratio: "4:3"}, "4:3"},
		{"doubao native pixels", "豆包", OutputSize{Width: 1200, Height: 1200}, "1:1"},
		{"doubao not native", "豆包", OutputSize{Ratio: "5:4"}, ""},
		{"doubao disabled", "豆包", OutputSize{}, ""},
		{"qianwen native", "千问", OutputSize{Ratio: "4:3"}, "4:3"},
		{"qianwen native pixels", "千问", OutputSize{Width: 1200, Height: 900}, "4:3"},
		{"qianwen not native", "千问", OutputSize{Ratio: "5:4"}, ""},
		{"qianwen disabled", "千问", OutputSize{}, ""},
		{"qwen_intl native", "千问国际", OutputSize{Ratio: "16:9"}, "16:9"},
		{"qwen_intl native pixels", "千问国际", OutputSize{Width: 1200, Height: 900}, "4:3"},
		{"qwen_intl not native", "千问国际", OutputSize{Ratio: "5:4"}, ""},
		{"qwen_intl disabled", "千问国际", OutputSize{}, ""},
		{"other platform", "其他", OutputSize{Ratio: "4:3"}, ""},
	} {
		if got := tc.size.NativeRatio(tc.platform); got != tc.want {
			t.Errorf("%s: NativeRatio=%q want %q", tc.name, got, tc.want)
		}
	}
	if w, h, err := ParseRatioLine("4:3"); w != 4 || h != 3 || err != nil {
		t.Fatalf("ParseRatioLine: %d %d %v", w, h, err)
	}
	if _, _, err := ParseRatioLine("宽高比"); err == nil {
		t.Fatal("ParseRatioLine should reject malformed ratio")
	}
	if got := AppendRatioLine("商品图", 4, 3); got != sizePromptLine("商品图", 4, 3) {
		t.Fatalf("AppendRatioLine: %q", got)
	}
}

func sizePromptLine(prompt string, w, h int) string {
	return strings.TrimSpace(fmt.Sprintf("%s\n请让每张输出图片的画面宽高比为 %d:%d，完整呈现主体。", prompt, w, h))
}

func TestOutputSizeKeepsShuffledWashResultsMatched(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{Wash: true, OutputPath: dir, OutputSize: OutputSize{Width: 200, Height: 100}}
	batch := Task{Platform: "豆包"}
	var results []strategy.Result
	var indices []int
	for i := 0; i < 10; i++ {
		batch.batch = append(batch.batch, Task{Platform: "豆包", Images: []string{fmt.Sprintf("source-%02d.png", i)}})
	}
	for i := 9; i >= 0; i-- {
		marker := color.NRGBA{R: uint8(20 * (i + 1)), B: uint8(255 - 20*(i+1)), A: 255}
		results = append(results, strategy.Result{Data: outputMarkerPNG(t, marker), WatermarkRemoved: true})
		indices = append(indices, i+1)
	}
	if err := e.saveWashBatch(context.Background(), &batch, results, indices); err != nil {
		t.Fatal(err)
	}
	for i, item := range batch.batch {
		if len(item.Results) != 1 || filepath.Base(item.Results[0]) != fmt.Sprintf("source-%02d_01.png", i) {
			t.Fatalf("wrong source association: %+v", item)
		}
		data, err := os.ReadFile(item.Results[0])
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() != 200 || img.Bounds().Dy() != 100 {
			t.Fatalf("wrong output dimensions: %v", img.Bounds())
		}
		r, _, b, _ := img.At(100, 50).RGBA()
		if r != uint32(20*(i+1))*257 || b != uint32(255-20*(i+1))*257 {
			t.Fatalf("wrong output for source %d: red=%d blue=%d", i, r, b)
		}
	}
}

func TestOutputSizeFailurePreservesUnadjustedImage(t *testing.T) {
	data := outputMarkerPNG(t, color.NRGBA{R: 220, A: 255})
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{Wash: true, OutputPath: t.TempDir(), OutputSize: OutputSize{Ratio: "65535:1"}}
	task := Task{Platform: "豆包", Images: []string{"source.png"}}
	result := strategy.Result{Data: data, WatermarkRemoved: true}
	saved, err := e.saveResults(context.Background(), &task, strategy.Factory("豆包"), nil, []strategy.Result{result})
	if err == nil || len(saved) != 0 {
		t.Fatalf("expected failure without claiming success: %v %v", saved, err)
	}
	files, _ := filepath.Glob(filepath.Join(e.opts.OutputPath, "待核对", "洗图", "*", "*.png"))
	if len(files) != 1 {
		t.Fatalf("unadjusted output missing: %v", files)
	}
	got, readErr := os.ReadFile(files[0])
	if readErr != nil || !bytes.Equal(got, data) {
		t.Fatalf("unadjusted output changed: %v", readErr)
	}
}

func TestOutputSizeAppliesToQianwenAndLeavesUnsupportedUntouched(t *testing.T) {
	data := outputMarkerPNG(t, color.NRGBA{R: 220, A: 255})
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{OutputPath: t.TempDir(), OutputSize: OutputSize{Width: 200, Height: 100}}
	task := Task{Platform: "千问", Prompt: "商品图"}
	saved, err := e.saveResults(context.Background(), &task, strategy.Factory("千问"), nil, []strategy.Result{{Data: data, WatermarkRemoved: true}})
	if err != nil || len(saved) != 1 {
		t.Fatalf("saved=%v err=%v", saved, err)
	}
	got, err := os.ReadFile(saved[0])
	if err != nil {
		t.Fatalf("read saved qianwen result: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(got))
	if err != nil || img.Bounds().Dx() != 200 || img.Bounds().Dy() != 100 {
		t.Fatalf("qianwen output size not applied: bounds=%v err=%v", img.Bounds(), err)
	}

	other := Task{Platform: "其他", Prompt: "商品图"}
	otherSaved, err := e.saveResults(context.Background(), &other, strategy.Factory("其他"), nil, []strategy.Result{{Data: data, WatermarkRemoved: true}})
	if err != nil || len(otherSaved) != 1 {
		t.Fatalf("other saved=%v err=%v", otherSaved, err)
	}
	otherBytes, err := os.ReadFile(otherSaved[0])
	if err != nil || !bytes.Equal(otherBytes, data) {
		t.Fatalf("unsupported platform output changed: %v", err)
	}
}

func TestOutputSizeRejectsInvalidSettingsBeforeStarting(t *testing.T) {
	e := NewEngine(nil, nil, nil, "")
	for _, tc := range []struct {
		platform string
		wantErr  string
	}{
		{"豆包", "豆包输出尺寸"},
		{"千问", "千问输出尺寸"},
		{"千问国际", "千问国际输出尺寸"},
	} {
		runs := 0
		err := e.RunObserved(context.Background(), Options{
			Platforms: []string{tc.platform}, Prompts: []string{"商品图"},
			OutputSize: OutputSize{Enabled: true, Ratio: "5:0"},
		}, nil, nil, func(context.Context, *Task) error { runs++; return nil })
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) || runs != 0 {
			t.Fatalf("%s invalid task was not rejected: runs=%d err=%v", tc.platform, runs, err)
		}
	}
}

func TestClosestNativeRatio(t *testing.T) {
	for _, tc := range []struct {
		w, h int
		want string
	}{
		{800, 800, "1:1"},
		{750, 1000, "3:4"},
		{1000, 750, "4:3"},
		{1920, 1080, "16:9"},
		{1080, 1920, "9:16"},
		{1200, 800, "3:2"},
		{800, 1200, "2:3"},
		{1000, 960, "1:1"},
		{0, 800, ""},
		{800, -1, ""},
	} {
		if got := ClosestNativeRatio(tc.w, tc.h); got != tc.want {
			t.Errorf("ClosestNativeRatio(%d,%d) = %q, want %q", tc.w, tc.h, got, tc.want)
		}
	}
}

func TestOutputSizeNativeRatioTolerancePreservesFullBleed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		size     OutputSize
		cfg      image.Config
		wantNeed bool
	}{
		{"qianwen native 3:4 1760x2368", OutputSize{Ratio: "3:4"}, image.Config{Width: 1760, Height: 2368}, false},
		{"qianwen native 3:4 1140x1472", OutputSize{Ratio: "3:4"}, image.Config{Width: 1140, Height: 1472}, false},
		{"qianwen native 4:3 1472x1140", OutputSize{Ratio: "4:3"}, image.Config{Width: 1472, Height: 1140}, false},
		{"qianwen native 16:9 1664x928", OutputSize{Ratio: "16:9"}, image.Config{Width: 1664, Height: 928}, false},
		{"qianwen native 16:9 2688x1536", OutputSize{Ratio: "16:9"}, image.Config{Width: 2688, Height: 1536}, false},
		{"wrong aspect 1:1 for 3:4 still adjusts", OutputSize{Ratio: "3:4"}, image.Config{Width: 1024, Height: 1024}, true},
		{"adjacent aspect 2:3 for 3:4 still adjusts", OutputSize{Ratio: "3:4"}, image.Config{Width: 1056, Height: 1584}, true},
		{"non-native 5:4 still adjusts", OutputSize{Ratio: "5:4"}, image.Config{Width: 1472, Height: 1140}, true},
		{"fixed pixels always enforce exact size", OutputSize{Width: 1024, Height: 1024}, image.Config{Width: 1000, Height: 1000}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, need, err := tc.size.plan(tc.cfg)
			if err != nil {
				t.Fatalf("plan error: %v", err)
			}
			if need != tc.wantNeed {
				t.Fatalf("plan(%+v, %+v) need=%v, want %v", tc.size, tc.cfg, need, tc.wantNeed)
			}
		})
	}
}

type stubRatioStrategy struct {
	flowStrategy
	ratios       []string
	ensureCalls  int
	uploadCalls  int
	sentPrompt   string
	failFirstSet bool
}

func (s *stubRatioStrategy) Platform() string { return "千问" }
func (s *stubRatioStrategy) EnsureImageMode(*cdp.Session) error {
	s.ensureCalls++
	return nil
}
func (s *stubRatioStrategy) SetOutputRatio(_ *cdp.Session, ratio string) (bool, string) {
	s.ratios = append(s.ratios, ratio)
	if s.failFirstSet && len(s.ratios) == 1 {
		return false, "未找到千问比例入口"
	}
	return true, ""
}
func (s *stubRatioStrategy) UploadFiles(*cdp.Session, []string) error {
	s.uploadCalls++
	return nil
}
func (s *stubRatioStrategy) SendPrompt(_ *cdp.Session, prompt string) error {
	s.sentPrompt = prompt
	return nil
}

func writeRectPNG(t *testing.T, w, h int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), fmt.Sprintf("src_%dx%d.png", w, h))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestQianwenSubmitOnceAppliesAndReconfirmsNativeRatio(t *testing.T) {
	wideSrc := writeRectPNG(t, 1920, 1080)

	t.Run("explicit native ratio strips prompt suffix and reconfirms after upload", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		e.opts = Options{OutputSize: OutputSize{Ratio: "4:3"}}
		strat := &stubRatioStrategy{}
		task := &Task{Index: 1, Platform: "千问", Prompt: "油画风景", Images: []string{wideSrc}}
		if err := e.submitOnce(task, strat, nil); err != nil {
			t.Fatalf("submitOnce failed: %v", err)
		}
		if strat.sentPrompt != "油画风景" {
			t.Fatalf("prompt = %q, want clean prompt without ratio suffix", strat.sentPrompt)
		}
		if strat.uploadCalls != 1 || strat.ensureCalls != 1 {
			t.Fatalf("uploadCalls=%d ensureCalls=%d, want 1/1", strat.uploadCalls, strat.ensureCalls)
		}
		if len(strat.ratios) != 2 || strat.ratios[0] != "4:3" || strat.ratios[1] != "4:3" {
			t.Fatalf("ratios = %v, want [4:3 4:3] (before and after upload)", strat.ratios)
		}
	})

	t.Run("original ratio auto-matches source image aspect ratio", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		strat := &stubRatioStrategy{}
		task := &Task{Index: 2, Platform: "千问", Prompt: "洗图", Images: []string{wideSrc}}
		if err := e.submitOnce(task, strat, nil); err != nil {
			t.Fatalf("submitOnce failed: %v", err)
		}
		if len(strat.ratios) != 2 || strat.ratios[0] != "16:9" || strat.ratios[1] != "16:9" {
			t.Fatalf("ratios = %v, want [16:9 16:9]", strat.ratios)
		}
	})

	t.Run("non-native custom size picks closest native ratio and keeps prompt ratio hint", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		e.opts = Options{OutputSize: OutputSize{Width: 1200, Height: 960}} // 5:4 -> closest 4:3
		strat := &stubRatioStrategy{}
		task := &Task{Index: 3, Platform: "千问", Prompt: "海边日落"}
		if err := e.submitOnce(task, strat, nil); err != nil {
			t.Fatalf("submitOnce failed: %v", err)
		}
		if !strings.Contains(strat.sentPrompt, "5:4") {
			t.Fatalf("expected non-native 5:4 hint in prompt, got %q", strat.sentPrompt)
		}
		if len(strat.ratios) != 1 || strat.ratios[0] != "4:3" {
			t.Fatalf("ratios = %v, want [4:3]", strat.ratios)
		}
	})

	t.Run("text-to-image default resets to 3:4", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		strat := &stubRatioStrategy{}
		task := &Task{Index: 4, Platform: "千问", Prompt: "橘猫"}
		if err := e.submitOnce(task, strat, nil); err != nil {
			t.Fatalf("submitOnce failed: %v", err)
		}
		if len(strat.ratios) != 1 || strat.ratios[0] != "3:4" {
			t.Fatalf("ratios = %v, want [3:4]", strat.ratios)
		}
	})

	t.Run("qwen_intl text-to-image default resets to 16:9 and ignores domestic QianwenOutputRatio", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		e.opts = Options{QianwenOutputRatio: "3:4"}
		strat := &stubRatioStrategy{}
		task := &Task{Index: 5, Platform: "千问国际", Prompt: "orange cat"}
		if err := e.submitOnce(task, strat, nil); err != nil {
			t.Fatalf("submitOnce failed: %v", err)
		}
		if len(strat.ratios) != 1 || strat.ratios[0] != "16:9" {
			t.Fatalf("ratios = %v, want [16:9]", strat.ratios)
		}
	})

	t.Run("qwen_intl image task reconfirms ratio after upload", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		e.opts = Options{OutputSize: OutputSize{Ratio: "1:1"}}
		strat := &stubRatioStrategy{}
		task := &Task{Index: 6, Platform: "千问国际", Prompt: "wash", Images: []string{wideSrc}}
		if err := e.submitOnce(task, strat, nil); err != nil {
			t.Fatalf("submitOnce failed: %v", err)
		}
		if strat.sentPrompt != "wash" || strat.uploadCalls != 1 || strat.ensureCalls != 1 {
			t.Fatalf("prompt=%q upload=%d ensure=%d", strat.sentPrompt, strat.uploadCalls, strat.ensureCalls)
		}
		if len(strat.ratios) != 2 || strat.ratios[0] != "1:1" || strat.ratios[1] != "1:1" {
			t.Fatalf("ratios = %v, want [1:1 1:1]", strat.ratios)
		}
	})

	t.Run("doubao explicit native ratio strips prompt suffix and reconfirms after upload", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		e.opts = Options{OutputSize: OutputSize{Ratio: "16:9"}}
		strat := &stubRatioStrategy{}
		task := &Task{Index: 7, Platform: "豆包", Prompt: "油画风景", Images: []string{wideSrc}}
		if err := e.submitOnce(task, strat, nil); err != nil {
			t.Fatalf("submitOnce failed: %v", err)
		}
		if strat.sentPrompt != "油画风景" || strat.uploadCalls != 1 || strat.ensureCalls != 1 {
			t.Fatalf("prompt=%q upload=%d ensure=%d", strat.sentPrompt, strat.uploadCalls, strat.ensureCalls)
		}
		if len(strat.ratios) != 2 || strat.ratios[0] != "16:9" || strat.ratios[1] != "16:9" {
			t.Fatalf("ratios = %v, want [16:9 16:9]", strat.ratios)
		}
	})

	t.Run("doubao non-native custom size selects closest official ratio and keeps prompt hint", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		e.opts = Options{OutputSize: OutputSize{Width: 1200, Height: 960}} // 5:4 -> closest 4:3
		strat := &stubRatioStrategy{}
		task := &Task{Index: 8, Platform: "豆包", Prompt: "海边日落", Images: []string{wideSrc}}
		if err := e.submitOnce(task, strat, nil); err != nil {
			t.Fatalf("submitOnce failed: %v", err)
		}
		if !strings.Contains(strat.sentPrompt, "5:4") {
			t.Fatalf("expected non-native 5:4 hint in prompt, got %q", strat.sentPrompt)
		}
		if len(strat.ratios) != 2 || strat.ratios[0] != "4:3" || strat.ratios[1] != "4:3" {
			t.Fatalf("ratios = %v, want [4:3 4:3]", strat.ratios)
		}
	})

	t.Run("doubao original ratio confirms 自动 before and after upload", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		strat := &stubRatioStrategy{}
		task := &Task{Index: 9, Platform: "豆包", Prompt: "洗图", Images: []string{wideSrc}}
		if err := e.submitOnce(task, strat, nil); err != nil {
			t.Fatalf("submitOnce failed: %v", err)
		}
		if len(strat.ratios) != 2 || strat.ratios[0] != "自动" || strat.ratios[1] != "自动" {
			t.Fatalf("ratios = %v, want [自动 自动]", strat.ratios)
		}
	})
}
