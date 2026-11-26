package strategy

import "testing"

func TestOriginalImageURLDropsThumbTemplate(t *testing.T) {
	thumb := "https://p3-flow-imagex-sign.byteimg.com/tos-cn-i-a9rns2rl98/abc.jpeg~tplv-a9rns2rl98-cthumb-lwm3.png?x-expires=1&x-signature=abc"
	if got, want := OriginalImageURL(thumb), "https://p3-flow-imagex-sign.byteimg.com/tos-cn-i-a9rns2rl98/abc.jpeg?x-expires=1&x-signature=abc"; got != want {
		t.Fatalf("original = %s, want %s", got, want)
	}
	plain := "https://p3-flow-imagex-sign.byteimg.com/tos-cn-i-a9rns2rl98/abc.png?x-signature=abc"
	if OriginalImageURL(plain) != plain {
		t.Fatalf("plain url changed: %s", OriginalImageURL(plain))
	}
	marked := "https://p9.byteimg.com/a.png?watermark=1&x-expires=1"
	if OriginalImageURL(marked) != "https://p9.byteimg.com/a.png" {
		t.Fatalf("watermark query stayed: %s", OriginalImageURL(marked))
	}
}

func TestPreferUnwatermarkedURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "doubao watermark and wm",
			in:   "https://p9-flow-imagex-sign.byteimg.com/ocean-cloud-tos/a.png?x-expires=1&watermark=1&wm=2",
			want: "https://p9-flow-imagex-sign.byteimg.com/ocean-cloud-tos/a.png",
		},
		{
			name: "wm only ignores case",
			in:   "https://p9.byteimg.com/a.png?x-expires=1&WM=1",
			want: "https://p9.byteimg.com/a.png",
		},
		{
			name: "fragment after watermark query is dropped with the query",
			in:   "https://cdn.example/a.png?watermark=1#frag",
			want: "https://cdn.example/a.png",
		},
		{
			name: "signed url without watermark params stays",
			in:   "https://p9.byteimg.com/a.png?x-expires=1&x-signature=abc",
			want: "https://p9.byteimg.com/a.png?x-expires=1&x-signature=abc",
		},
		{
			name: "qianwen oss process is not a watermark param",
			in:   "https://img.alicdn.com/a.png?x-oss-process=image/resize,w_1024",
			want: "https://img.alicdn.com/a.png?x-oss-process=image/resize,w_1024",
		},
		{
			name: "path containing watermark is kept",
			in:   "https://cdn.example/watermark/a.png",
			want: "https://cdn.example/watermark/a.png",
		},
		{
			name: "no query",
			in:   "https://cdn.example/a.png",
			want: "https://cdn.example/a.png",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PreferUnwatermarkedURL(tc.in); got != tc.want {
				t.Fatalf("PreferUnwatermarkedURL(%q)=%q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestComposerTextMatches(t *testing.T) {
	cases := []struct {
		name     string
		expected string
		actual   string
		want     bool
	}{
		{name: "exact single line", expected: "油画风景", actual: "油画风景", want: true},
		{name: "exact multiline", expected: "第一段\n\n第二段", actual: "第一段\n\n第二段", want: true},
		{name: "rich text blank line expansion", expected: "第一段\n第二段", actual: "第一段\n\n第二段", want: true},
		{name: "rich text blank line collapse", expected: "第一段\n\n第二段", actual: "第一段\n第二段", want: true},
		{name: "reordered lines rejected", expected: "第一段\n第二段", actual: "第二段\n第一段", want: false},
		{name: "truncated line rejected", expected: "第一段\n第二段", actual: "第一段\n第二", want: false},
		{name: "missing line rejected", expected: "第一段\n第二段", actual: "第一段", want: false},
		{name: "empty actual rejected", expected: "第一段", actual: "", want: false},
		{name: "both empty match", expected: "  \n ", actual: "", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := composerTextMatches(tc.expected, tc.actual); got != tc.want {
				t.Fatalf("composerTextMatches(%q, %q) = %v, want %v", tc.expected, tc.actual, got, tc.want)
			}
		})
	}
}

func TestQwenIntlFactoryAndURLHelpers(t *testing.T) {
	strat := Factory("千问国际")
	if strat == nil {
		t.Fatal("Factory(\"千问国际\") returned nil")
	}
	if _, ok := strat.(*QwenIntl); !ok {
		t.Fatalf("unexpected strategy type: %T", strat)
	}
	if strat.Platform() != "千问国际" {
		t.Fatalf("Platform() = %q, want 千问国际", strat.Platform())
	}
	if strat.HomeURL() != "https://chat.qwen.ai/" {
		t.Fatalf("HomeURL() = %q, want https://chat.qwen.ai/", strat.HomeURL())
	}
	if _, ok := strat.(HistoryCleaner); !ok {
		t.Fatal("QwenIntl must implement HistoryCleaner")
	}

	raw := "https://cdn.qwenlm.ai/output/img.png?x-oss-process=image/resize,w_1024&foo=bar"
	want := "https://cdn.qwenlm.ai/output/img.png?foo=bar"
	if got := stripOSSProcessURL(raw); got != want {
		t.Fatalf("stripOSSProcessURL(%q) = %q, want %q", raw, got, want)
	}
	plain := "https://cdn.qwenlm.ai/output/img.png?foo=bar"
	if got := stripOSSProcessURL(plain); got != plain {
		t.Fatalf("stripOSSProcessURL(%q) changed to %q", plain, got)
	}

	if got := qwenChatURLFor(nil, "千问国际", "uuid-123"); got != "https://chat.qwen.ai/c/uuid-123" {
		t.Fatalf("qwenChatURLFor(千问国际) = %q, want https://chat.qwen.ai/c/uuid-123", got)
	}
	if got := qwenChatURLFor(nil, "千问", "uuid-123"); got != "https://www.qianwen.com/chat/uuid-123" {
		t.Fatalf("qwenChatURLFor(千问) = %q, want https://www.qianwen.com/chat/uuid-123", got)
	}
	if got := Factory("千问").HomeURL(); got != "https://www.qianwen.com/" {
		t.Fatalf("Qianwen.HomeURL() = %q, want https://www.qianwen.com/", got)
	}
}
