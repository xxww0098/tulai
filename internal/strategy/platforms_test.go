package strategy

import "testing"

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
