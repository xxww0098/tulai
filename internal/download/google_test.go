package download

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type googleTestRoundTrip func(*http.Request) (*http.Response, error)

func (f googleTestRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGoogleImageSearchReportsJavaScriptChallenge(t *testing.T) {
	for _, tc := range []struct {
		page, body string
		blocked    bool
	}{
		{"https://www.google.com/search?q=cat&tbm=isch", `<a href="/httpservice/retry/enablejs">continue</a>`, true},
		{"https://www.google.com/search?q=cat&udm=2", `<a href="/httpservice/retry/enablejs">continue</a><img src="https://example.test/photo.jpg">`, true},
		{"https://www.google.com.evil.test/search?q=cat&tbm=isch", `<a href="/httpservice/retry/enablejs">continue</a>`, false},
	} {
		called := false
		getter := NewHTTPGetter("")
		getter.client.Transport = googleTestRoundTrip(func(r *http.Request) (*http.Response, error) {
			called = true
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
		})
		_, err := Discover(context.Background(), getter, tc.page, 1)
		if got := err != nil && strings.Contains(err.Error(), "验证"); got != tc.blocked || !called {
			t.Errorf("%s: challenge=%v called=%v err=%v", tc.page, got, called, err)
		}
	}
}
