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
