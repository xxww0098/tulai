package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/extbridge"
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
	if got := size.prompt("豆包", "蓝色商品"); !strings.Contains(got, "5:4") || !strings.Contains(got, "蓝色商品") {
		t.Fatalf("wrong ratio prompt: %q", got)
	}
	if got := size.prompt("千问", "蓝色商品"); got != "蓝色商品" {
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
		{"other platform", "千问", OutputSize{Ratio: "4:3"}, ""},
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

func TestOutputSizeReachesDoubaoBridgeAndSavedImage(t *testing.T) {
	st := openTaskStore(t)
	port := freePort(t)
	st.Set(extbridge.SettingBrowserBackend, "extension")
	st.Set("extBridgePort", port)
	st.Set("extBridgeToken", "dev-token")
	dir := t.TempDir()
	data := outputMarkerPNG(t, color.NRGBA{R: 220, A: 255})
	prompts := make(chan string, 1)
	go serveFakeExt("127.0.0.1:"+port, "dev-token", extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		switch name {
		case extbridge.CmdSetPrompt:
			var payload struct {
				Prompt string `json:"prompt"`
			}
			_ = json.Unmarshal(env.Payload, &payload)
			select {
			case prompts <- payload.Prompt:
			default:
			}
			return ackOK(env, nil)
		case extbridge.CmdCaptchaAssist, extbridge.CmdSend:
			return ackOK(env, nil)
		case extbridge.CmdWaitResult:
			return ackOK(env, map[string]any{"images": []map[string]string{{"url": "https://cdn.example/output.png"}}})
		case extbridge.CmdDownload:
			return chunkAck(env, data, 0)
		default:
			return ackFail(env, extbridge.EProtocol, name)
		}
	})
	e := NewEngine(st, nil, nil, dir)
	defer e.CloseSessions()
	var last Task
	err := e.Run(context.Background(), Options{
		Platforms: []string{"豆包"}, Prompts: []string{"商品图"}, OutputPath: dir,
		WaitResult: true, WaitResultSeconds: 30, Concurrency: 1,
		OutputSize: OutputSize{Width: 200, Height: 100},
	}, func(task Task) { last = task }, func(Progress) {})
	if err != nil || last.Status != StatusSuccess || len(last.Results) != 1 {
		t.Fatalf("task=%+v err=%v", last, err)
	}
	select {
	case prompt := <-prompts:
		if !strings.Contains(prompt, "2:1") || !strings.Contains(prompt, "商品图") {
			t.Fatalf("bridge prompt: %q", prompt)
		}
	case <-time.After(time.Second):
		t.Fatal("bridge did not receive a prompt")
	}
	if filepath.Base(last.Results[0]) != "商品图_01.png" {
		t.Fatalf("prompt suffix changed output name: %s", last.Results[0])
	}
	saved, err := os.ReadFile(last.Results[0])
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(saved))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 200 || img.Bounds().Dy() != 100 {
		t.Fatalf("saved dimensions: %v", img.Bounds())
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

func TestOutputSizeDoesNotChangeOtherPlatforms(t *testing.T) {
	data := outputMarkerPNG(t, color.NRGBA{R: 220, A: 255})
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{OutputPath: t.TempDir(), OutputSize: OutputSize{Width: 200, Height: 100}}
	task := Task{Platform: "千问", Prompt: "商品图"}
	saved, err := e.saveResults(context.Background(), &task, strategy.Factory("千问"), nil, []strategy.Result{{Data: data, WatermarkRemoved: true}})
	if err != nil || len(saved) != 1 {
		t.Fatalf("saved=%v err=%v", saved, err)
	}
	got, err := os.ReadFile(saved[0])
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("other platform output changed: %v", err)
	}
}

func TestOutputSizeRejectsInvalidSettingsBeforeStarting(t *testing.T) {
	e := NewEngine(nil, nil, nil, "")
	runs := 0
	err := e.RunObserved(context.Background(), Options{
		Platforms: []string{"豆包"}, Prompts: []string{"商品图"},
		OutputSize: OutputSize{Enabled: true, Ratio: "5:0"},
	}, nil, nil, func(context.Context, *Task) error { runs++; return nil })
	if err == nil || !strings.Contains(err.Error(), "豆包输出尺寸") || runs != 0 {
		t.Fatalf("invalid task was not rejected: runs=%d err=%v", runs, err)
	}
}
