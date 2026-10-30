package prompts

import (
	"strings"
	"testing"
	"unicode"
)

func TestBuiltinsKeepFullTextAfterLeadingTrim(t *testing.T) {
	oil, ok := Builtin(IDOil)
	if !ok || oil.Label != "数字油画" || !strings.HasPrefix(oil.Body, "每张图") {
		t.Fatalf("oil: %+v", oil)
	}
	if !strings.Contains(oil.Body, "反向提示词：") || !strings.Contains(oil.Body, "\n") {
		t.Fatal("oil lost the reverse prompt or the line break")
	}
	if strings.HasSuffix(strings.TrimRight(oil.Body, "\r\n"), "局部残缺") == false {
		t.Fatal("oil lost its ending")
	}
	art, ok := Builtin(IDArt)
	if !ok || art.Label != "数字画" || !strings.HasPrefix(art.Body, "反推图生文要求：") {
		t.Fatalf("art: %+v", art)
	}
	if !strings.Contains(art.Body, "重绘强度：0.8") {
		t.Fatal("art lost its ending")
	}
	circle, ok := Builtin(IDCircle)
	if !ok || circle.Label != "圆形印花" || !strings.HasPrefix(circle.Body, "每张图") {
		t.Fatalf("circle: %+v", circle)
	}
	if !strings.Contains(circle.Body, "1:1完美正圆形构图") || !strings.Contains(circle.Body, "重绘强度0.8") {
		t.Fatal("circle lost its instructions")
	}
	for _, item := range Builtins() {
		if item.Body == "" || strings.Contains(item.Body, "\uFEFF") {
			t.Fatalf("empty or BOM body: %s", item.ID)
		}
		r, _ := utf8DecodeFirst(item.Body)
		if unicode.IsSpace(r) {
			t.Fatalf("%s still starts with space %q", item.ID, r)
		}
	}
}

func utf8DecodeFirst(s string) (rune, int) {
	return []rune(s)[0], 1
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
	oil, _ := Builtin(IDOil)
	got := TaskPrompts(IDOil, oil.Body)
	if len(got) != 1 || !strings.Contains(got[0], "反向提示词：") || strings.Contains(got[0], "\n") == false {
		t.Fatalf("builtin was split: %#v", got)
	}
	lines := TaskPrompts("", "画一只猫\n\n画一条鱼")
	if len(lines) != 2 || lines[0] != "画一只猫" || lines[1] != "画一条鱼" {
		t.Fatalf("line mode: %#v", lines)
	}
	if TaskPrompts(IDArt, "  \n") != nil {
		t.Fatal("blank template should not send")
	}
}

func TestUserCopyDoesNotReplaceBuiltin(t *testing.T) {
	users := []UserPrompt{{
		ID: "u1", Label: UniqueLabel("数字油画", nil), Body: BuiltinBody(IDOil) + "\n加一句", From: IDOil,
	}}
	if users[0].Label != "数字油画 副本" {
		t.Fatalf("label %q", users[0].Label)
	}
	oil, _ := Builtin(IDOil)
	if strings.Contains(oil.Body, "加一句") {
		t.Fatal("builtin was overwritten")
	}
	if NormalizePick("u1", users) != "u1" || NormalizePick("missing", users) != "" {
		t.Fatal("pick normalization")
	}
	if LabelFor("u1", users) != "数字油画 副本" {
		t.Fatalf("label for user: %q", LabelFor("u1", users))
	}
	raw := FormatUser(users)
	again := ParseUser(raw)
	if len(again) != 1 || again[0].Body != users[0].Body || again[0].From != IDOil {
		t.Fatalf("round trip: %+v", again)
	}
	if ParseUser("{") != nil {
		t.Fatal("bad json kept")
	}
}

func BuiltinBody(id string) string {
	item, _ := Builtin(id)
	return item.Body
}

func TestDuplicateUserLabelGetsID(t *testing.T) {
	users := []UserPrompt{{ID: "u1", Label: "数字油画", Body: "自定义"}}
	labels := Labels(users)
	if labels[0] != LabelCustom || labels[1] != "数字油画" {
		t.Fatalf("labels: %#v", labels)
	}
	found := false
	for _, label := range labels {
		if label == "数字油画（u1）" {
			found = true
		}
	}
	if !found {
		t.Fatalf("user label not disambiguated: %#v", labels)
	}
	if IDForLabel("数字油画（u1）", users) != "u1" || IDForLabel("没有", users) != "" {
		t.Fatal("id for label")
	}
}
