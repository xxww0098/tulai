package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

// The native fallback accepts exactly this legacy goods path. The original
// suffix check allowed digits, '+', '-', and '.'; a slash is never allowed.
var mujiLegacyID = regexp.MustCompile(`^[0-9+.-]{1,64}$`)

func mujiProductID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		!strings.EqualFold(u.Hostname(), "www.muji.com.cn") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") || strings.Contains(u.Path, "\\") || path.Clean(u.Path) != u.Path {
		return ""
	}
	const prefix = "/cn/store/goods/"
	id := strings.TrimPrefix(u.Path, prefix)
	if id == u.Path || !mujiLegacyID.MatchString(id) {
		return ""
	}
	return id
}

func isMujiProduct(u *url.URL) bool { return mujiProductID(u) != "" }

// Images are public, on a different origin from the product page. Do not send
// site credentials and do not follow a redirect away from the fixed image URL.
func mujiImageGetter(getter Getter) Getter {
	if session, ok := getter.(*RenderedSession); ok {
		getter = session.getter
	}
	g, ok := getter.(*HTTPGetter)
	if !ok || g == nil || g.client == nil {
		return getter
	}
	copyGetter, client := *g, *g.client
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("无印良品图片发生跳转")
	}
	copyGetter.client = &client
	return &copyGetter
}

func mujiPageGetter(getter Getter, page *url.URL) Getter {
	g, ok := getter.(*HTTPGetter)
	if !ok || g == nil || g.client == nil {
		return getter
	}
	copyGetter, client := *g, *g.client
	previous := client.CheckRedirect
	id := mujiProductID(page)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || mujiProductID(req.URL) != id {
			return errors.New("无印良品商品页面跳转到其他地址")
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return errors.New("无印良品商品页面重定向次数过多")
		}
		return nil
	}
	copyGetter.client = &client
	return &copyGetter
}

func discoverMuji(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := mujiProductID(root)
	if id == "" || getter == nil {
		return result, errors.New("无印良品旧商品地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, mujiPageGetter(getter, root), crawlPage{url: root.String(), depth: 1})
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		var status *HTTPStatusError
		if errors.As(err, &status) {
			return result, fmt.Errorf("无印良品商品页面读取失败：HTTP %d", status.StatusCode)
		}
		return result, errors.New("无印良品商品页面读取失败，请检查网络或授权状态")
	}
	if doc == nil || final == nil || mujiProductID(final) != id || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("无印良品商品页面不可用或跳转到其他商品")
	}
	return mujiCollect(ctx, getter, final, doc)
}

func mujiImageURL(id string, index int) string {
	return (&url.URL{Scheme: "https", Host: "img.muji.com.cn", Path: fmt.Sprintf("/img/item/%s_%02d_400.jpg", id, index)}).String()
}

type mujiProbeResult struct {
	ok     bool
	status int
}

// Read at most three bytes even when the server ignores Range. A 403 or other
// unsuccessful response is unresolved, never evidence that an image is absent.
func mujiProbe(ctx context.Context, getter Getter, raw string) mujiProbeResult {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	probeCtx = context.WithValue(probeCtx, publicDownloadKey{}, true)
	response, err := getter.Get(probeCtx, raw, func(req *http.Request) {
		req.Header.Set("Accept", "image/jpeg")
		req.Header.Set("Accept-Encoding", "identity")
		req.Header.Set("Range", "bytes=0-2")
	})
	if err != nil || response == nil {
		return mujiProbeResult{}
	}
	if response.Body != nil {
		defer response.Body.Close()
	}
	res := mujiProbeResult{status: response.StatusCode}
	if response.Body == nil || (response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent) {
		return res
	}
	final, err := url.Parse(firstNonEmpty(response.FinalURL, raw))
	if err != nil || final.String() != raw {
		return res
	}
	if response.StatusCode == http.StatusPartialContent && !strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Range")), "bytes 0-") {
		return res
	}
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(media, "image/jpeg") {
		return res
	}
	var prefix [3]byte
	if _, err := io.ReadFull(response.Body, prefix[:]); err != nil || prefix != [3]byte{0xff, 0xd8, 0xff} {
		return res
	}
	res.ok = true
	return res
}

// The original native handler synthesizes 01..20 from the goods number; it
// does not establish that this list or the 400px variants are complete.
func mujiCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := mujiProductID(page)
	if id == "" || getter == nil || doc == nil {
		return result, errors.New("无印良品旧商品页面无效")
	}
	walkHTML(doc, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
	})
	for _, word := range []string{"安全验证", "访问受限", "请登录"} {
		if strings.Contains(result.Title, word) {
			return result, errors.New("无印良品页面需要登录或安全验证")
		}
	}
	if session, ok := getter.(*RenderedSession); ok {
		if session == nil || session.tab == nil || session.getter == nil || session.tab.Err() != nil {
			return result, errors.New("无印良品下载浏览器已关闭")
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		stop := context.AfterFunc(session.tab, cancel)
		defer func() { stop(); cancel() }()
	}
	probeGetter := mujiImageGetter(getter)
	var probes [20]mujiProbeResult
	jobs := make(chan int, len(probes))
	for i := range probes {
		jobs <- i
	}
	close(jobs)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					return
				}
				probes[index] = mujiProbe(ctx, probeGetter, mujiImageURL(id, index+1))
			}
		}()
	}
	wg.Wait()
	var unresolved, forbidden int
	for i, probe := range probes {
		if !probe.ok {
			unresolved++
			if probe.status == http.StatusForbidden {
				forbidden++
			}
			continue
		}
		image := mujiImageURL(id, i+1)
		item := Item{URL: image, RequireImage: true, PublicOnly: true,
			Headers:  map[string]string{"Accept": "image/jpeg"},
			Metadata: map[string]string{"muji_product_id": id, "muji_sequence": fmt.Sprintf("%02d", i+1)}}
		if ref := imageReferer(page.Scheme+"://"+page.Host+"/", image); ref != "" {
			item.Headers["Referer"] = ref
		}
		result.Items = append(result.Items, item)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if unresolved > 0 {
		return result, fmt.Errorf("无印良品已保留 %d 张可访问图片；%d 个地址未能确认（其中 %d 个返回 HTTP 403）。仅尝试旧商品 01–20 号 400 像素图片", len(result.Items), unresolved, forbidden)
	}
	return result, errors.New("无印良品仅尝试旧商品 01–20 号 400 像素图片，其他图片和原图尚未确认")
}
