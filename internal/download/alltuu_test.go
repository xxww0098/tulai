package download

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestAlltuuAlbumDoesNotScanUnverifiedCover(t *testing.T) {
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("unverified album must not be scanned as a generic page")
		return nil, nil
	})
	got, err := Discover(context.Background(), getter, "https://m.alltuu.com/album/12345678/?menu=live", 1)
	if err == nil || !strings.Contains(err.Error(), "权限") || len(got.Items) != 0 {
		t.Fatalf("album=%+v err=%v", got, err)
	}
	for _, raw := range []string{"https://alltuu.com.evil.test/album/123", "https://www.alltuu.com:444/album/123", "https://www.alltuu.com/album/../123"} {
		u, _ := url.Parse(raw)
		if isAlltuuAlbum(u) {
			t.Fatalf("accepted %s", raw)
		}
	}
}
