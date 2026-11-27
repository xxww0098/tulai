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

func TestOutputSizeValidationAndNativeRatio(t *testing.T) {
	for _, tc := range []struct {
		size      OutputSize
		wantError bool
	}{
		{OutputSize{}, false},
		{OutputSize{Ratio: "1:1"}, false},
		{OutputSize{Ratio: "4：3"}, false},
		{OutputSize{Ratio: "3:4"}, false},
		{OutputSize{Ratio: "16:9"}, false},
		{OutputSize{Ratio: "9:16"}, false},
		{OutputSize{Ratio: "3:2"}, false},
		{OutputSize{Ratio: "2:3"}, false},
		{OutputSize{Ratio: "5:4"}, true},
		{OutputSize{Ratio: "4:5"}, true},
		{OutputSize{Ratio: "5:0"}, true},
		{OutputSize{Ratio: "1024x1024"}, true},
		{OutputSize{Ratio: "自定义尺寸"}, true},
	} {
		if got := tc.size.Validate(); (got != nil) != tc.wantError {
			t.Errorf("%+v: Validate()=%v wantError=%v", tc.size, got, tc.wantError)
		}
	}

	for _, tc := range []struct {
		name     string
		platform string
		size     OutputSize
		want     string
	}{
		{"doubao native", "豆包", OutputSize{Ratio: "4:3"}, "4:3"},
		{"doubao fullwidth colon", "豆包", OutputSize{Ratio: "16：9"}, "16:9"},
		{"doubao not native", "豆包", OutputSize{Ratio: "5:4"}, ""},
		{"doubao empty", "豆包", OutputSize{}, ""},
		{"qianwen native", "千问", OutputSize{Ratio: "4:3"}, "4:3"},
		{"qianwen not native", "千问", OutputSize{Ratio: "5:4"}, ""},
		{"qianwen empty", "千问", OutputSize{}, ""},
		{"qwen_intl native", "千问国际", OutputSize{Ratio: "16:9"}, "16:9"},
		{"qwen_intl not native", "千问国际", OutputSize{Ratio: "5:4"}, ""},
		{"qwen_intl empty", "千问国际", OutputSize{}, ""},
		{"gemini native 1:1", "Gemini", OutputSize{Ratio: "1:1"}, "1:1"},
		{"gemini native 9:16", "Gemini", OutputSize{Ratio: "9:16"}, "9:16"},
		{"gemini native 3:4", "Gemini", OutputSize{Ratio: "3:4"}, "3:4"},
		{"gemini native 4:3", "Gemini", OutputSize{Ratio: "4:3"}, "4:3"},
		{"gemini native 16:9", "Gemini", OutputSize{Ratio: "16:9"}, "16:9"},
		{"gemini rejects 2:3", "Gemini", OutputSize{Ratio: "2:3"}, ""},
		{"gemini rejects 3:2", "Gemini", OutputSize{Ratio: "3:2"}, ""},
		{"gemini empty", "Gemini", OutputSize{}, ""},
		{"jimeng native 3:4", "即梦", OutputSize{Ratio: "3:4"}, "3:4"},
		{"jimeng native 16:9", "即梦", OutputSize{Ratio: "16:9"}, "16:9"},
		{"jimeng native 2:3", "即梦", OutputSize{Ratio: "2:3"}, "2:3"},
		{"jimeng native 21:9", "即梦", OutputSize{Ratio: "21:9"}, "21:9"},
		{"jimeng not native", "即梦", OutputSize{Ratio: "5:4"}, ""},
		{"jimeng dynamic live ratio", "即梦", OutputSize{Ratio: "5:4", AllowedRatios: []string{"1:1", "5:4", "21:9"}}, "5:4"},
		{"jimeng empty", "即梦", OutputSize{}, ""},
		{"other platform", "其他", OutputSize{Ratio: "4:3"}, ""},
	} {
		if got := tc.size.NativeRatio(tc.platform); got != tc.want {
			t.Errorf("%s: NativeRatio=%q want %q", tc.name, got, tc.want)
		}
	}
	if err := (OutputSize{Ratio: "16:9"}).ValidateFor("Gemini"); err != nil {
		t.Fatalf("Gemini 16:9 should be valid: %v", err)
	}
	if err := (OutputSize{Ratio: "3:2"}).ValidateFor("Gemini"); err == nil {
		t.Fatal("Gemini 3:2 should be rejected under defaults")
	}
	if err := (OutputSize{Ratio: "3:2", AllowedRatios: []string{"1:1", "3:2", "16:9"}}).ValidateFor("Gemini"); err != nil {
		t.Fatalf("Gemini 3:2 should be accepted when live webpage caps include it: %v", err)
	}
	if err := (OutputSize{Ratio: "2:3"}).ValidateFor("Gemini"); err == nil {
		t.Fatal("Gemini 2:3 should be rejected under defaults")
	}
	if err := (OutputSize{Ratio: "21:9", Resolution: "4K"}).ValidateFor("即梦"); err != nil {
		t.Fatalf("Jimeng 21:9 + 4K should be valid: %v", err)
	}
	if err := (OutputSize{Resolution: "8K"}).ValidateFor("即梦"); err == nil {
		t.Fatal("Jimeng 8K should be rejected")
	}
	if err := (OutputSize{Resolution: "4K"}).ValidateFor("豆包"); err == nil {
		t.Fatal("Doubao should reject resolution when webpage exposes none")
	}

	if w, h, err := ParseRatioLine("4:3"); w != 4 || h != 3 || err != nil {
		t.Fatalf("ParseRatioLine: %d %d %v", w, h, err)
	}
	if _, _, err := ParseRatioLine("5:0"); err == nil {
		t.Fatal("ParseRatioLine should reject zero ratio")
	}
	if _, _, err := ParseRatioLine("宽高比"); err == nil {
		t.Fatal("ParseRatioLine should reject malformed ratio")
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

func TestOutputSizeKeepsShuffledWashResultsMatchedWithoutResizing(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{Wash: true, OutputPath: dir, OutputSize: OutputSize{Ratio: "16:9"}}
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
		// 生图保存阶段不得二次缩放、裁剪或补白，必须保留网页生成原尺寸（128x128）。
		if img.Bounds().Dx() != 128 || img.Bounds().Dy() != 128 {
			t.Fatalf("generated image dimensions were altered during save: %v", img.Bounds())
		}
		r, _, b, _ := img.At(64, 64).RGBA()
		if r != uint32(20*(i+1))*257 || b != uint32(255-20*(i+1))*257 {
			t.Fatalf("wrong output for source %d: red=%d blue=%d", i, r, b)
		}
	}
}

func TestSaveResultsNeverAltersGeneratedDimensionsAcrossPlatforms(t *testing.T) {
	data := outputMarkerPNG(t, color.NRGBA{R: 220, A: 255})
	for _, platform := range []string{"豆包", "千问", "千问国际", "Gemini", "即梦", "其他"} {
		t.Run(platform, func(t *testing.T) {
			e := NewEngine(nil, nil, nil, "")
			e.opts = Options{OutputPath: t.TempDir(), OutputSize: OutputSize{Ratio: "16:9"}}
			task := Task{Platform: platform, Prompt: "商品图"}
			saved, err := e.saveResults(context.Background(), &task, strategy.Factory(platform), nil, []strategy.Result{{Data: data, WatermarkRemoved: true}})
			if err != nil || len(saved) != 1 {
				t.Fatalf("saved=%v err=%v", saved, err)
			}
			got, err := os.ReadFile(saved[0])
			if err != nil {
				t.Fatalf("read saved result: %v", err)
			}
			if !bytes.Equal(got, data) {
				t.Fatalf("%s output image bytes were altered during save", platform)
			}
		})
	}
}

func TestOutputSizeRejectsInvalidSettingsBeforeStarting(t *testing.T) {
	e := NewEngine(nil, nil, nil, "")
	for _, tc := range []struct {
		platform string
		ratio    string
		wantErr  string
	}{
		{"豆包", "5:4", "豆包输出尺寸"},
		{"千问", "5:4", "千问输出尺寸"},
		{"千问国际", "5:4", "千问国际输出尺寸"},
		{"Gemini", "3:2", "Gemini输出尺寸"},
		{"Gemini", "2:3", "Gemini输出尺寸"},
		{"即梦", "5:4", "即梦输出尺寸"},
	} {
		runs := 0
		err := e.RunObserved(context.Background(), Options{
			Platforms: []string{tc.platform}, Prompts: []string{"商品图"},
			OutputSize: OutputSize{Ratio: tc.ratio},
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

type stubRatioStrategy struct {
	flowStrategy
	ratios        []string
	ensureCalls   int
	uploadCalls   int
	sentPrompt    string
	sendCalls     int
	failFirstSet  bool
	failSecondSet bool
}

func (s *stubRatioStrategy) Platform() string { return "千问" }
func (s *stubRatioStrategy) EnsureImageMode(*cdp.Session) error {
	s.ensureCalls++
	return nil
}
func (s *stubRatioStrategy) SetOutputRatio(_ *cdp.Session, ratio string) (bool, string) {
	s.ratios = append(s.ratios, ratio)
	if s.failFirstSet && len(s.ratios) == 1 {
		return false, "未找到比例入口"
	}
	if s.failSecondSet && len(s.ratios) == 2 {
		return false, "上传后比例未恢复"
	}
	return true, ""
}
func (s *stubRatioStrategy) UploadFiles(*cdp.Session, []string) error {
	s.uploadCalls++
	return nil
}
func (s *stubRatioStrategy) SendPrompt(_ *cdp.Session, prompt string) error {
	s.sendCalls++
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

func TestSubmitOnceAppliesNativeRatioByClickOnlyWithoutPromptSuffix(t *testing.T) {
	wideSrc := writeRectPNG(t, 1920, 1080)

	t.Run("explicit native ratio keeps prompt clean and reconfirms after upload", func(t *testing.T) {
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

	t.Run("explicit native ratio click failure fails before upload and never injects prompt text", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		e.opts = Options{OutputSize: OutputSize{Ratio: "4:3"}}
		strat := &stubRatioStrategy{failFirstSet: true}
		task := &Task{Index: 2, Platform: "千问", Prompt: "油画风景", Images: []string{wideSrc}}
		err := e.submitOnce(task, strat, nil)
		if err == nil || !strings.Contains(err.Error(), "输出比例") {
			t.Fatalf("expected explicit ratio click failure, got %v", err)
		}
		if strat.uploadCalls != 0 || strat.sendCalls != 0 {
			t.Fatalf("must abort before upload/send: upload=%d send=%d", strat.uploadCalls, strat.sendCalls)
		}
		hint := ClassifyFailure(StatusFailed, err.Error())
		if hint.Sentence != "网页端输出比例设置失败，请重试" {
			t.Fatalf("unexpected failure hint: %+v", hint)
		}
	})

	t.Run("explicit native ratio post-upload reconfirm failure aborts before send", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		e.opts = Options{OutputSize: OutputSize{Ratio: "16:9"}}
		strat := &stubRatioStrategy{failSecondSet: true}
		task := &Task{Index: 3, Platform: "豆包", Prompt: "油画风景", Images: []string{wideSrc}}
		err := e.submitOnce(task, strat, nil)
		if err == nil || !strings.Contains(err.Error(), "输出比例") {
			t.Fatalf("expected post-upload ratio failure, got %v", err)
		}
		if strat.uploadCalls != 1 || strat.sendCalls != 0 {
			t.Fatalf("must abort after upload before send: upload=%d send=%d", strat.uploadCalls, strat.sendCalls)
		}
	})

	t.Run("original ratio auto-matches source image aspect ratio", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		strat := &stubRatioStrategy{}
		task := &Task{Index: 4, Platform: "千问", Prompt: "洗图", Images: []string{wideSrc}}
		if err := e.submitOnce(task, strat, nil); err != nil {
			t.Fatalf("submitOnce failed: %v", err)
		}
		if strat.sentPrompt != "洗图" {
			t.Fatalf("prompt altered: %q", strat.sentPrompt)
		}
		if len(strat.ratios) != 2 || strat.ratios[0] != "16:9" || strat.ratios[1] != "16:9" {
			t.Fatalf("ratios = %v, want [16:9 16:9]", strat.ratios)
		}
	})

	t.Run("text-to-image default resets to 3:4 on 千问", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		strat := &stubRatioStrategy{}
		task := &Task{Index: 5, Platform: "千问", Prompt: "橘猫"}
		if err := e.submitOnce(task, strat, nil); err != nil {
			t.Fatalf("submitOnce failed: %v", err)
		}
		if strat.sentPrompt != "橘猫" {
			t.Fatalf("prompt altered: %q", strat.sentPrompt)
		}
		if len(strat.ratios) != 1 || strat.ratios[0] != "3:4" {
			t.Fatalf("ratios = %v, want [3:4]", strat.ratios)
		}
	})

	t.Run("qwen_intl text-to-image default resets to 16:9", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		strat := &stubRatioStrategy{}
		task := &Task{Index: 6, Platform: "千问国际", Prompt: "orange cat"}
		if err := e.submitOnce(task, strat, nil); err != nil {
			t.Fatalf("submitOnce failed: %v", err)
		}
		if strat.sentPrompt != "orange cat" {
			t.Fatalf("prompt altered: %q", strat.sentPrompt)
		}
		if len(strat.ratios) != 1 || strat.ratios[0] != "16:9" {
			t.Fatalf("ratios = %v, want [16:9]", strat.ratios)
		}
	})

	t.Run("qwen_intl image task reconfirms ratio after upload", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		e.opts = Options{OutputSize: OutputSize{Ratio: "1:1"}}
		strat := &stubRatioStrategy{}
		task := &Task{Index: 7, Platform: "千问国际", Prompt: "wash", Images: []string{wideSrc}}
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

	t.Run("doubao explicit native ratio keeps prompt clean and reconfirms after upload", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		e.opts = Options{OutputSize: OutputSize{Ratio: "16:9"}}
		strat := &stubRatioStrategy{}
		task := &Task{Index: 8, Platform: "豆包", Prompt: "油画风景", Images: []string{wideSrc}}
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

	t.Run("doubao original ratio confirms 自动 before and after upload", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		strat := &stubRatioStrategy{}
		task := &Task{Index: 9, Platform: "豆包", Prompt: "洗图", Images: []string{wideSrc}}
		if err := e.submitOnce(task, strat, nil); err != nil {
			t.Fatalf("submitOnce failed: %v", err)
		}
		if strat.sentPrompt != "洗图" {
			t.Fatalf("prompt altered: %q", strat.sentPrompt)
		}
		if len(strat.ratios) != 2 || strat.ratios[0] != "自动" || strat.ratios[1] != "自动" {
			t.Fatalf("ratios = %v, want [自动 自动]", strat.ratios)
		}
	})

	t.Run("gemini explicit native ratio sets and reconfirms after upload", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		e.opts = Options{OutputSize: OutputSize{Ratio: "9:16"}}
		strat := &stubRatioStrategy{}
		task := &Task{Index: 11, Platform: "Gemini", Prompt: "studio portrait", Images: []string{wideSrc}}
		if err := e.submitOnce(task, strat, nil); err != nil {
			t.Fatalf("submitOnce failed: %v", err)
		}
		if strat.sentPrompt != "studio portrait" || strat.uploadCalls != 1 || strat.ensureCalls != 1 {
			t.Fatalf("prompt=%q upload=%d ensure=%d", strat.sentPrompt, strat.uploadCalls, strat.ensureCalls)
		}
		if len(strat.ratios) != 2 || strat.ratios[0] != "9:16" || strat.ratios[1] != "9:16" {
			t.Fatalf("ratios = %v, want [9:16 9:16]", strat.ratios)
		}
	})

	t.Run("gemini default ratio leaves native aspect ratio unset", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		strat := &stubRatioStrategy{}
		task := &Task{Index: 12, Platform: "Gemini", Prompt: "orange cat"}
		if err := e.submitOnce(task, strat, nil); err != nil {
			t.Fatalf("submitOnce failed: %v", err)
		}
		if len(strat.ratios) != 0 {
			t.Fatalf("ratios = %v, want empty for Gemini default ratio", strat.ratios)
		}
	})

	t.Run("image task without readable dimensions still verifies image mode before and after upload", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		strat := &stubRatioStrategy{}
		missingSrc := filepath.Join(t.TempDir(), "unreadable.png")
		task := &Task{Index: 10, Platform: "千问", Prompt: "洗图", Images: []string{missingSrc}}
		if err := e.submitOnce(task, strat, nil); err != nil {
			t.Fatalf("submitOnce failed: %v", err)
		}
		if strat.uploadCalls != 1 || strat.ensureCalls != 2 {
			t.Fatalf("uploadCalls=%d ensureCalls=%d, want 1/2 (before and after upload when targetRatio is empty)", strat.uploadCalls, strat.ensureCalls)
		}
	})

	t.Run("jimeng smart ratio and explicit resolution apply via SetOutputSize before and after upload", func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		e.opts = Options{OutputSize: OutputSize{AutoValue: "智能", Resolution: "4K"}}
		strat := &stubSizeStrategy{}
		task := &Task{Index: 13, Platform: "即梦", Prompt: "洗图", Images: []string{wideSrc}}
		if err := e.submitOnce(task, strat, nil); err != nil {
			t.Fatalf("submitOnce failed: %v", err)
		}
		if len(strat.calls) != 2 || strat.calls[0] != "智能|4K" || strat.calls[1] != "智能|4K" {
			t.Fatalf("jimeng SetOutputSize calls = %v, want [智能|4K 智能|4K]", strat.calls)
		}
	})
}

type stubSizeStrategy struct {
	stubRatioStrategy
	calls []string
}

func (s *stubSizeStrategy) Platform() string { return "即梦" }

func (s *stubSizeStrategy) SetOutputSize(_ *cdp.Session, ratio, resolution string) (bool, string) {
	s.calls = append(s.calls, ratio+"|"+resolution)
	return true, ""
}
