package prompts

import (
	"strings"
	"testing"
)

func TestBuiltinsContainOnlyWash(t *testing.T) {
	items := Builtins()
	want := []Template{
		{ID: IDWash, Label: "默认洗图", Body: DefaultWashPrompt, Kind: KindWash, Builtin: true},
		{ID: IDDoubaoWash, Label: "豆包洗图", Body: DoubaoWashPrompt, Kind: KindWash, Builtin: true},
	}
	if len(items) != len(want) {
		t.Fatalf("builtins: %+v", items)
	}
	for i, item := range items {
		if item != want[i] {
			t.Fatalf("builtins[%d] = %+v, want %+v", i, item, want[i])
		}
	}
	for _, id := range []string{"oil", "art", "circle"} {
		if _, ok := Builtin(id); ok {
			t.Fatalf("retired builtin %q is still available", id)
		}
	}
}

func TestImportTextKeepsLinesAndBlankParagraphs(t *testing.T) {
	if got := ImportText("甲\r\n乙\n"); got != "甲\n乙" {
		t.Fatalf("lines = %q", got)
	}
	if got := ImportText("\uFEFF甲\n\n乙\n"); got != "甲\n乙" {
		t.Fatalf("blank lines = %q", got)
	}
	if got := ImportText("甲\n\n乙\n丙\n\n丁"); got != "甲\n乙 丙\n丁" {
		t.Fatalf("paragraphs = %q", got)
	}
	if got := ImportText("  \n"); got != "" {
		t.Fatalf("empty = %q", got)
	}
	lines := TaskPrompts("", ImportText("画一只猫\n\n画一条鱼"))
	if len(lines) != 2 || lines[0] != "画一只猫" || lines[1] != "画一条鱼" {
		t.Fatalf("task prompts = %#v", lines)
	}
}

func TestTemplateIsOnePromptAndLinesStaySplit(t *testing.T) {
	wash, _ := Builtin(IDWash)
	got := TaskPrompts(IDWash, wash.Body)
	if len(got) != 1 || got[0] != wash.Body || !strings.Contains(got[0], "\n") {
		t.Fatalf("builtin was split: %#v", got)
	}
	lines := TaskPrompts("", "画一只猫\n\n画一条鱼")
	if len(lines) != 2 || lines[0] != "画一只猫" || lines[1] != "画一条鱼" {
		t.Fatalf("line mode: %#v", lines)
	}
	if TaskPrompts(IDWash, "  \n") != nil {
		t.Fatal("blank template should not send")
	}
}

func TestUserCopyDoesNotReplaceBuiltin(t *testing.T) {
	users := []UserPrompt{{
		ID: "u1", Label: UniqueLabel("默认洗图", nil), Body: DefaultWashPrompt + "\n加一句", From: IDWash,
	}}
	if users[0].Label != "默认洗图 副本" {
		t.Fatalf("label %q", users[0].Label)
	}
	wash, _ := Builtin(IDWash)
	if strings.Contains(wash.Body, "加一句") {
		t.Fatal("builtin was overwritten")
	}
	if NormalizePick("u1", users) != "u1" || NormalizePick("missing", users) != "" {
		t.Fatal("pick normalization")
	}
	if LabelFor("u1", users) != "默认洗图 副本" {
		t.Fatalf("label for user: %q", LabelFor("u1", users))
	}
	raw := FormatUser(users)
	again := ParseUser(raw)
	if len(again) != 1 || again[0].Body != users[0].Body || again[0].From != IDWash {
		t.Fatalf("round trip: %+v", again)
	}
	if ParseUser("{") != nil {
		t.Fatal("bad json kept")
	}
}

func TestPromptKindsKeepLegacyEntriesShared(t *testing.T) {
	users := ParseUser(`[{"id":"u1","label":"旧模板","body":"旧正文"},{"id":"u2","label":"洗图模板","body":"洗图正文","kind":"wash"}]`)
	if len(users) != 2 || users[0].Kind != "" || users[1].Kind != KindWash {
		t.Fatalf("legacy or typed prompts: %+v", users)
	}
	again := ParseUser(FormatUser(users))
	if len(again) != 2 || again[0].Kind != "" || again[1].Kind != KindWash {
		t.Fatalf("kind round trip: %+v", again)
	}
	for _, item := range Catalog(again) {
		if item.ID == "u2" && item.Kind != KindWash {
			t.Fatalf("catalog lost kind: %+v", item)
		}
	}
}

func TestDuplicateUserLabelGetsID(t *testing.T) {
	users := []UserPrompt{{ID: "u1", Label: "默认洗图", Body: "自定义"}}
	labels := Labels(users)
	if labels[0] != LabelCustom || labels[1] != "默认洗图" {
		t.Fatalf("labels: %#v", labels)
	}
	found := false
	for _, label := range labels {
		if label == "默认洗图（u1）" {
			found = true
		}
	}
	if !found {
		t.Fatalf("user label not disambiguated: %#v", labels)
	}
	if IDForLabel("默认洗图（u1）", users) != "u1" || IDForLabel("没有", users) != "" {
		t.Fatal("id for label")
	}
}
