package rename

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreviewRulesPreserveExtensionsAndDirectories(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "猫01.JPG"), filepath.Join(dir, ".env"), filepath.Join(dir, "目录.v1")}
	for _, path := range paths[:2] {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(paths[2], 0o700); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		opts Options
		want []string
	}{
		{"replace", Options{Replacements: []Replacement{{Find: "猫", With: "狗"}, {Find: "/([0-9]+)/g", With: "[$1]"}}}, []string{"狗[01].JPG", ".env", "目录.v[1]"}},
		{"insert", Options{Mode: Insert, Position: 1, Index: 1, Text: "_"}, []string{"猫_01.JPG", "._env", "目_录.v1"}},
		{"number", Options{Mode: Number, Numbering: Numbering{Start: 1, Width: 3, Prefix: "图"}}, []string{"图001.JPG", "图002", "图003"}},
		{"manual", Options{Mode: Manual, Names: []string{"a.png", "b", "c"}}, []string{"a.png", "b", "c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changes, err := Preview(paths, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			for i, change := range changes {
				if got := filepath.Base(change.To); got != tc.want[i] {
					t.Fatalf("item %d = %q, want %q", i, got, tc.want[i])
				}
			}
		})
	}
	if _, err := Preview(paths, Options{Mode: Manual, Names: []string{"../escape", "b", "c"}}); err == nil {
		t.Fatal("accepted a name outside the parent directory")
	}
}

func TestNumberFormats(t *testing.T) {
	for _, tc := range []struct {
		n            int64
		format, want string
	}{
		{0, "中文小写", "零"}, {10, "中文小写", "十"}, {101, "中文小写", "一百零一"},
		{10001, "中文小写", "一万零一"}, {100010001, "中文小写", "一亿零一万零一"},
		{10, "中文大写", "壹拾"}, {101, "中文大写", "壹佰零壹"},
		{0, "英文大写", ""}, {26, "英文大写", "Z"}, {27, "英文大写", "AA"}, {53, "英文小写", "ba"},
	} {
		if got := number(Numbering{Start: tc.n, Format: tc.format}, 0); got != tc.want {
			t.Errorf("%s %d = %q, want %q", tc.format, tc.n, got, tc.want)
		}
	}
}

func TestRegexReplacement(t *testing.T) {
	for _, tc := range []struct{ find, with, want string }{
		{"/a/", "x", "x a"}, {"/a/g", "x", "x x"}, {"/(a)/g", "<$1:$&>", "<a:a> <a:a>"},
	} {
		got, err := replace("a a", Replacement{Find: tc.find, With: tc.with})
		if err != nil || got != tc.want {
			t.Errorf("%q: got %q, %v, want %q", tc.find, got, err, tc.want)
		}
	}
	if _, err := replace("a", Replacement{Find: "/(?<=x)a/g"}); err == nil {
		t.Fatal("unsupported regular expression was accepted")
	}
}

func TestFindTemplates(t *testing.T) {
	for _, tc := range []struct {
		rule Replacement
		want string
	}{
		{Replacement{Template: 1, Count: 3, With: "[]"}, "[]-123-xyz"},
		{Replacement{Template: 2, Count: 3, With: "[]"}, "abc-123-[]"},
		{Replacement{Template: 3, Offset: 4, Count: 3, With: "[]"}, "abc-[]-xyz"},
		{Replacement{Template: 4, Text: "-", With: "[]"}, "abc-[]"},
		{Replacement{Template: 5, Text: "-", With: "[]"}, "[]-123-xyz"},
		{Replacement{Template: 6, Text: "-", Count: 3, With: "[]"}, "abc-[]-xyz"},
		{Replacement{Template: 7, Text: "-", Count: 2, With: "[]"}, "a[]-123-xyz"},
		{Replacement{Template: 1, Count: 0, With: "X"}, "Xabc-123-xyz"},
	} {
		got, err := replace("abc-123-xyz", tc.rule)
		if err != nil || got != tc.want {
			t.Errorf("template %d = %q, %v; want %q", tc.rule.Template, got, err, tc.want)
		}
	}
	got, err := replace("ab.c", Replacement{Template: 4, Text: ".", With: "X"})
	if err != nil || got != "ab.X" {
		t.Fatalf("literal dot = %q, %v", got, err)
	}
}
