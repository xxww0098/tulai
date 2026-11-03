package download

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDiscoverImageURLInCustomAttributes(t *testing.T) {
	markup := `<html>
		<img src="/placeholder.png" data-full="../original.JPG?size=2#view" data-thumb="/thumb.gif?x=1">
		<img src="/tiny.png" srcset="/small.jpg 1x, /large.jpg 2x">
		<img src="/valid.png" alt="not-alt.jpg" title="not-title.png" class="not-class.jpg" id="not-id.gif" aria-label="not-aria.jpg">
		<img src="/fallback.png" data-invalid="javascript:alert.jpg" data-other="not an image.jpg" data-video="/movie.mp4">
		<img src="/tiny2.png" data-srcset="/small2.webp 1x, /large2.webp 2x">
		<img src="/placeholder3.png" data-asset="/image.TIFF?x=1#part">
		<img src="/placeholder4.png" alt="https://cdn.example.test/original.png?x=1#frag">
		<img src="/fallback2.png" data-image="photo.jpg">
		<object data="../objects/diagram.jpg?x=1#view"></object>
		<img src="/fallback3.png" data-full="/full.jpg" srcset="/small3.jpg 1x, /large3.jpg 2x">
	</html>`
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(markup))}, nil
	})
	got, err := Discover(context.Background(), getter, "https://example.test/gallery/page", 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"https://example.test/original.JPG?size=2",
		"https://example.test/thumb.gif?x=1",
		"https://example.test/large.jpg",
		"https://example.test/valid.png",
		"https://example.test/fallback.png",
		"https://example.test/large2.webp",
		"https://example.test/image.TIFF?x=1",
		"https://cdn.example.test/original.png?x=1",
		"https://example.test/fallback2.png",
		"https://example.test/objects/diagram.jpg?x=1",
		"https://example.test/full.jpg",
		"https://example.test/large3.jpg",
	}
	if len(got.Items) != len(want) {
		t.Fatalf("images = %+v", got.Items)
	}
	for i, item := range got.Items {
		if item.URL != want[i] {
			t.Errorf("image %d = %q, want %q", i, item.URL, want[i])
		}
	}
}
