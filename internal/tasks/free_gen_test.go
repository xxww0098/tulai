package tasks

import "testing"

func TestBuildQueueRepeatCountTextAndImageModes(t *testing.T) {
	browsers := map[string][]int64{"豆包": {1, 2}}

	// 文生图: 2 prompts × 3 repeats = 6 tasks
	textQueue := BuildQueue(Options{
		Platforms:   []string{"豆包"},
		Prompts:     []string{"多啦A梦", "蜡笔小新"},
		RepeatCount: 3,
	}, browsers)
	if len(textQueue) != 6 {
		t.Fatalf("text RepeatCount=3 len=%d, want 6", len(textQueue))
	}
	wantText := []string{"多啦A梦", "多啦A梦", "多啦A梦", "蜡笔小新", "蜡笔小新", "蜡笔小新"}
	for i, task := range textQueue {
		if task.Prompt != wantText[i] || len(task.Images) != 0 {
			t.Fatalf("#%d got prompt=%q images=%v, want %q []", task.Index, task.Prompt, task.Images, wantText[i])
		}
	}

	// 图生图 (逐行配对 + RepeatCount=2): each image keeps its paired prompt across repeats
	imgQueue := BuildQueue(Options{
		Platforms:   []string{"豆包"},
		Prompts:     []string{"水彩风", "油画风"},
		Images:      []string{"a.png", "b.png", "c.png"},
		RepeatCount: 2,
	}, browsers)
	if len(imgQueue) != 6 {
		t.Fatalf("image RepeatCount=2 len=%d, want 6", len(imgQueue))
	}
	wantPairs := []struct {
		img    string
		prompt string
	}{
		{"a.png", "水彩风"},
		{"a.png", "水彩风"},
		{"b.png", "油画风"},
		{"b.png", "油画风"},
		{"c.png", "水彩风"},
		{"c.png", "水彩风"},
	}
	for i, want := range wantPairs {
		if len(imgQueue[i].Images) != 1 || imgQueue[i].Images[0] != want.img || imgQueue[i].Prompt != want.prompt {
			t.Fatalf("#%d got (%v, %q), want (%q, %q)", i+1, imgQueue[i].Images, imgQueue[i].Prompt, want.img, want.prompt)
		}
	}

	// 洗图与裂变不受 RepeatCount 影响
	washQueue := BuildQueue(Options{
		Platforms:   []string{"豆包"},
		Prompts:     []string{"洗图词"},
		Images:      []string{"a.png"},
		Wash:        true,
		RepeatCount: 4,
	}, browsers)
	if len(washQueue) != 1 {
		t.Fatalf("wash should ignore RepeatCount, got len=%d", len(washQueue))
	}
}

func TestResultStemImageToImageFallbackWithoutPrompt(t *testing.T) {
	gen := Options{}
	if got := gen.resultStem("", "/in/product_hero.png"); got != "product_hero" {
		t.Fatalf("img2img empty prompt stem = %q, want product_hero", got)
	}
	if got := gen.resultStem("   ", ""); got != "文生图" {
		t.Fatalf("txt2img empty prompt stem = %q, want 文生图", got)
	}
	if got := gen.resultStem("多啦A梦\n第二行", "/in/product_hero.png"); got != "多啦A梦" {
		t.Fatalf("img2img with prompt stem = %q, want 多啦A梦", got)
	}
}
