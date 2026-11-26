package prompts

import (
	"strings"
	"testing"
)

func TestBuiltinsContainOnlyDoubaoWash(t *testing.T) {
	items := Builtins()
	want := []Template{
		{ID: IDDoubaoWash, Label: "洗图", Body: DoubaoWashPrompt, Kind: KindWash, Builtin: true},
	}
	if len(items) != len(want) {
		t.Fatalf("builtins: %+v", items)
	}
	for i, item := range items {
		if item != want[i] {
			t.Fatalf("builtins[%d] = %+v, want %+v", i, item, want[i])
		}
	}
	for _, id := range []string{"oil", "art", "circle", IDWash, IDDoubaoPixelWash} {
		if _, ok := Builtin(id); ok {
			t.Fatalf("retired builtin %q is still available", id)
		}
	}
}

func TestImportTextKeepsOnePromptPerLine(t *testing.T) {
	if got := ImportText("甲\r\n乙\n"); got != "甲\n乙" {
		t.Fatalf("lines = %q", got)
	}
	if got := ImportText("\uFEFF  甲 \n\n  乙\r丙\n\n丁  "); got != "甲\n乙\n丙\n丁" {
		t.Fatalf("blank lines and cr/crlf = %q", got)
	}
	if got := ImportText("  \n\r\n "); got != "" {
		t.Fatalf("empty = %q", got)
	}
	lines := TaskPrompts("", ImportText("画一只猫\n\n画一条鱼"))
	if len(lines) != 2 || lines[0] != "画一只猫" || lines[1] != "画一条鱼" {
		t.Fatalf("task prompts = %#v", lines)
	}
}

func TestTemplateIsOnePromptAndLinesStaySplit(t *testing.T) {
	wash, _ := Builtin(IDDoubaoWash)
	got := TaskPrompts(IDDoubaoWash, wash.Body)
	if len(got) != 1 || got[0] != wash.Body || !strings.Contains(got[0], "\n") {
		t.Fatalf("builtin was split: %#v", got)
	}
	lines := TaskPrompts("", "画一只猫\n\n画一条鱼")
	if len(lines) != 2 || lines[0] != "画一只猫" || lines[1] != "画一条鱼" {
		t.Fatalf("line mode: %#v", lines)
	}
	if TaskPrompts(IDDoubaoWash, "  \n") != nil {
		t.Fatal("blank template should not send")
	}
}

func TestUserCopyDoesNotReplaceBuiltin(t *testing.T) {
	users := []UserPrompt{{
		ID: "u1", Label: UniqueLabel("洗图", nil), Body: DoubaoWashPrompt + "\n加一句", From: IDDoubaoWash,
	}}
	if users[0].Label != "洗图 副本" {
		t.Fatalf("label %q", users[0].Label)
	}
	wash, _ := Builtin(IDDoubaoWash)
	if strings.Contains(wash.Body, "加一句") {
		t.Fatal("builtin was overwritten")
	}
	if NormalizePick("u1", users) != "u1" || NormalizePick("missing", users) != "" {
		t.Fatal("pick normalization")
	}
	if LabelFor("u1", users) != "洗图 副本" {
		t.Fatalf("label for user: %q", LabelFor("u1", users))
	}
	raw := FormatUser(users)
	again := ParseUser(raw)
	if len(again) != 1 || again[0].Body != users[0].Body || again[0].From != IDDoubaoWash {
		t.Fatalf("round trip: %+v", again)
	}
	if ParseUser("{") != nil {
		t.Fatal("bad json kept")
	}
}

func TestPromptKindsKeepLegacyEntriesShared(t *testing.T) {
	users := ParseUser(`[{"id":"u1","label":"旧模板","body":"旧正文"},{"id":"u2","label":"洗图模板","body":"洗图正文","kind":"wash"},{"id":"u3","label":"裂变模板","body":"裂变正文","kind":"fission"}]`)
	if len(users) != 3 || users[0].Kind != "" || users[1].Kind != KindWash || users[2].Kind != KindFission {
		t.Fatalf("legacy or typed prompts: %+v", users)
	}
	again := ParseUser(FormatUser(users))
	if len(again) != 3 || again[0].Kind != "" || again[1].Kind != KindWash || again[2].Kind != KindFission {
		t.Fatalf("kind round trip: %+v", again)
	}
	for _, item := range Catalog(again) {
		if item.ID == "u2" && item.Kind != KindWash {
			t.Fatalf("catalog lost wash kind: %+v", item)
		}
		if item.ID == "u3" && item.Kind != KindFission {
			t.Fatalf("catalog lost fission kind: %+v", item)
		}
	}
}

func TestDuplicateUserLabelGetsID(t *testing.T) {
	users := []UserPrompt{{ID: "u1", Label: "洗图", Body: "自定义"}}
	labels := Labels(users)
	if labels[0] != LabelCustom || labels[1] != "洗图" {
		t.Fatalf("labels: %#v", labels)
	}
	found := false
	for _, label := range labels {
		if label == "洗图（u1）" {
			found = true
		}
	}
	if !found {
		t.Fatalf("user label not disambiguated: %#v", labels)
	}
	if IDForLabel("洗图（u1）", users) != "u1" || IDForLabel("没有", users) != "" {
		t.Fatal("id for label")
	}
}

func TestFissionTextPrompts(t *testing.T) {
	if got := FissionTextPrompts(""); got != nil {
		t.Fatalf("blank text fission should return nil, got %v", got)
	}
	if got := FissionTextPrompts(DefaultFissionPrompt); got != nil {
		t.Fatalf("mother-image default prompt should return nil for text-only fission, got %v", got)
	}

	// Single theme auto-expands to 4 distinct text-only directions without attachment wording
	theme := FissionTextPrompts("复古飞行员橘猫，水彩插画")
	if len(theme) != 4 {
		t.Fatalf("theme fission len = %d, want 4", len(theme))
	}
	seen := map[string]bool{}
	for i, p := range theme {
		if !strings.Contains(p, "复古飞行员橘猫，水彩插画") || strings.Contains(p, "附件母图") || seen[p] {
			t.Fatalf("theme direction #%d invalid: %q", i+1, p)
		}
		seen[p] = true
	}

	// Exactly 4 non-empty lines preserve each custom direction verbatim in order
	customLines := FissionTextPrompts("清晨森林特写\n\n雪山远景航拍\n咖啡馆俯视平铺\n霓虹夜景仰视")
	if len(customLines) != 4 {
		t.Fatalf("4-line fission len = %d, want 4", len(customLines))
	}
	wantPrefixes := []string{"清晨森林特写", "雪山远景航拍", "咖啡馆俯视平铺", "霓虹夜景仰视"}
	for i, want := range wantPrefixes {
		if !strings.HasPrefix(customLines[i], want) || !strings.Contains(customLines[i], "不超过 60%") {
			t.Fatalf("4-line direction #%d = %q, want prefix %q", i+1, customLines[i], want)
		}
	}
}

func TestBuiltinPromptsContainNoSizeOrRatioDirectives(t *testing.T) {
	candidates := []struct {
		name string
		text string
	}{
		{"DefaultWashPrompt", DefaultWashPrompt},
		{"DoubaoWashPrompt", DoubaoWashPrompt},
		{"DefaultFissionPrompt", DefaultFissionPrompt},
	}
	for i, p := range FissionPrompts("") {
		candidates = append(candidates, struct{ name, text string }{name: "FissionPrompt#" + itoa(i+1), text: p})
	}
	for i, p := range FissionTextPrompts("海边橘猫") {
		candidates = append(candidates, struct{ name, text string }{name: "FissionTextPrompt#" + itoa(i+1), text: p})
	}
	forbidden := []string{"比例", "尺寸", "宽高", "4:5", "5:4", "16:9", "9:16", "3:4", "4:3", "1:1", "3:2", "2:3", "1024", "2048", "48×60"}
	for _, c := range candidates {
		for _, bad := range forbidden {
			if strings.Contains(c.text, bad) {
				t.Errorf("%s must not contain size/ratio wording %q", c.name, bad)
			}
		}
	}
}
