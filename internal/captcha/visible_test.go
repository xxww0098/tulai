package captcha

import "testing"

func TestReadBlocked(t *testing.T) {
	for _, tc := range []struct {
		out  string
		want bool
	}{
		{out: "true", want: true},
		{out: "false", want: false},
		{out: `"true"`, want: true},
		{out: "null", want: false},
		{out: "", want: false},
	} {
		if got := readBlocked(tc.out); got != tc.want {
			t.Errorf("readBlocked(%q) = %v, want %v", tc.out, got, tc.want)
		}
	}
}
