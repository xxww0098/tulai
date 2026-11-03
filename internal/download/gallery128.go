package download

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var (
	gallery128Path = regexp.MustCompile(`^/(gallery|view)/([1-9][0-9]{0,19})(?:/([1-9][0-9]{0,6}))?/?$`)
	gallery128Part = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// shortcut: stop after 1,000 pages and report that the gallery is incomplete.
const gallery128MaxPages = 1000

type gallery128Ref struct {
	kind, id string
	page     int
}

func gallery128PageRef(u *url.URL) gallery128Ref {
	if u == nil || u.User != nil ||
		!((u.Scheme == "https" && (u.Port() == "" || u.Port() == "443")) || (u.Scheme == "http" && (u.Port() == "" || u.Port() == "80"))) ||
		!strings.EqualFold(u.Hostname(), "imhentai.xxx") || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") ||
		strings.Contains(u.Path, "\\") || strings.Contains(u.Path, "..") {
		return gallery128Ref{}
	}
	match := gallery128Path.FindStringSubmatch(u.Path)
	if match == nil || (match[1] == "gallery" && match[3] != "") || (match[1] == "view" && match[3] == "") {
		return gallery128Ref{}
	}
	page := 0
	if match[3] != "" {
		page, _ = strconv.Atoi(match[3])
	}
	return gallery128Ref{kind: match[1], id: match[2], page: page}
}

func isGallery128Page(u *url.URL) bool { return gallery128PageRef(u).kind != "" }
func gallery128Key(u *url.URL) string  { return gallery128PageRef(u).id }

// Only these CDN names were present in archived first-party reader pages.
var gallery128Hosts = map[string]bool{
	"m1.imhentai.xxx": true, "m2.imhentai.xxx": true, "m3.imhentai.xxx": true,
	"m4.imhentai.xxx": true, "m5.imhentai.xxx": true, "m6.imhentai.xxx": true, "m7.imhentai.xxx": true,
	"m8.imhentai.xxx": true, "m9.imhentai.xxx": true, "m10.imhentai.xxx": true,
	"m11.imhentai.xxx": true,
}

func gallery128Image(raw, dir, id string) (string, bool) {
	if len(raw) == 0 || len(raw) > 8192 || !gallery128Part.MatchString(dir) || !gallery128Part.MatchString(id) {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") || u.Fragment != "" ||
		!gallery128Hosts[strings.ToLower(u.Hostname())] || strings.Contains(u.Path, "\\") || strings.Contains(u.Path, "..") ||
		strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != dir || parts[1] != id || parts[2] == "" {
		return "", false
	}
	switch strings.ToLower(path.Ext(parts[2])) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
	default:
		return "", false
	}
	return u.String(), true
}

type gallery128Reader struct {
	total      int
	dir, album string
	image      string
}

// Read the actual reader's identity and image attributes, without evaluating scripts.
func gallery128Read(ctx context.Context, doc *html.Node, ref gallery128Ref) (gallery128Reader, error) {
	var result gallery128Reader
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if doc == nil || ref.kind != "view" || ref.page < 1 {
		return result, errors.New("相册阅读页面无效")
	}
	values, counts := map[string]string{}, map[string]int{}
	var sources []string
	images := 0
	imagePage, imagePageClasses := false, 0
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		id := attribute(n, "id")
		if n.Data == "input" && (id == "u_id" || id == "pages" || id == "image_dir" || id == "gallery_id") {
			counts[id]++
			values[id] = attribute(n, "value")
		}
		if n.Data == "img" && id == "gimg" {
			images++
			for _, class := range strings.Fields(attribute(n, "class")) {
				if strings.HasPrefix(class, "image_") {
					imagePageClasses++
					if class == "image_"+strconv.Itoa(ref.page) {
						imagePage = true
					}
				}
			}
			sources = append(sources, attribute(n, "src"), attribute(n, "data-src"))
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	for _, key := range []string{"u_id", "pages", "image_dir", "gallery_id"} {
		if counts[key] != 1 {
			return result, errors.New("相册页面缺少明确的身份或分页信息，可能需要登录或验证")
		}
	}
	if values["u_id"] != ref.id || images != 1 || !imagePage || imagePageClasses != 1 {
		return result, errors.New("相册页面身份不匹配，或图片信息不明确")
	}
	total, err := strconv.Atoi(values["pages"])
	if err != nil || total < ref.page || total > 10000000 {
		return result, errors.New("相册页数无效")
	}
	result.total, result.dir, result.album = total, values["image_dir"], values["gallery_id"]
	for _, raw := range sources {
		if raw == "" || strings.HasPrefix(strings.ToLower(raw), "data:image/") {
			continue
		}
		asset, ok := gallery128Image(raw, result.dir, result.album)
		if !ok || (result.image != "" && result.image != asset) {
			return gallery128Reader{}, errors.New("相册图片地址不明确或不属于已验证图床")
		}
		result.image = asset
	}
	if result.image == "" {
		return gallery128Reader{}, errors.New("相册当前页没有可确认的图片，可能需要登录或验证")
	}
	return result, nil
}

type gallery128Root struct {
	dir, album string
	total      int
}

func gallery128ReadRoot(ctx context.Context, doc *html.Node, page *url.URL) (gallery128Root, error) {
	var root gallery128Root
	if err := ctx.Err(); err != nil {
		return root, err
	}
	ref := gallery128PageRef(page)
	if doc == nil || ref.kind != "gallery" {
		return root, errors.New("相册页面无效")
	}
	values, counts, conflicts := map[string]string{}, map[string]int{}, map[string]bool{}
	first := false
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		id := attribute(n, "id")
		if n.Data == "input" && (id == "gallery_id" || id == "load_id" || id == "load_dir" || id == "load_pages") {
			value := attribute(n, "value")
			if counts[id] != 0 && values[id] != value {
				conflicts[id] = true
			}
			counts[id]++
			values[id] = value
		}
		if n.Data == "a" {
			target, err := crawlURL(page, attribute(n, "href"))
			reader := gallery128PageRef(target)
			if err == nil && sameOrigin(page, target) && reader.kind == "view" && reader.id == ref.id && reader.page == 1 && target.RawQuery == "" {
				first = true
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return root, err
	}
	if !first || counts["gallery_id"] == 0 || conflicts["gallery_id"] || values["gallery_id"] != ref.id {
		return root, errors.New("相册身份或首页链接无法确认，可能需要登录或验证")
	}
	for _, key := range []string{"load_id", "load_dir", "load_pages"} {
		if conflicts[key] {
			return root, errors.New("相册分页信息不明确")
		}
	}
	if (counts["load_id"] == 0) != (counts["load_dir"] == 0) {
		return root, errors.New("相册图片目录信息不完整")
	}
	if counts["load_id"] > 0 {
		root.album, root.dir = values["load_id"], values["load_dir"]
		if !gallery128Part.MatchString(root.album) || !gallery128Part.MatchString(root.dir) {
			return gallery128Root{}, errors.New("相册图片目录无效")
		}
	}
	if counts["load_pages"] > 0 {
		total, err := strconv.Atoi(values["load_pages"])
		if err != nil || total < 1 || total > 10000000 {
			return gallery128Root{}, errors.New("相册页数无效")
		}
		root.total = total
	}
	return root, nil
}

func gallery128Fetch(ctx context.Context, getter Getter, page *url.URL) (*html.Node, error) {
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	want, got := gallery128PageRef(page), gallery128PageRef(final)
	if doc == nil || !sameOrigin(page, final) || want != got || final.RawQuery != "" || final.Fragment != "" {
		return nil, errors.New("相册页面已跳转，可能需要登录或验证")
	}
	return doc, nil
}

func discoverGallery128(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := gallery128PageRef(page)
	if getter == nil || ref.kind == "" {
		return result, errors.New("相册地址或下载客户端无效")
	}
	target := &url.URL{Scheme: "https", Host: "imhentai.xxx", Path: page.Path}
	doc, err := gallery128Fetch(ctx, getter, target)
	if err != nil {
		return result, fmt.Errorf("读取相册失败，可能需要登录或验证：%w", err)
	}
	return gallery128Collect(ctx, getter, target, doc)
}

func gallery128Collect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := gallery128PageRef(page)
	if getter == nil || doc == nil || ref.kind == "" {
		return result, errors.New("相册地址或页面无效")
	}
	if page.Scheme != "https" {
		return result, errors.New("请先在浏览器打开相册 HTTPS 页面")
	}
	var root gallery128Root
	var current gallery128Reader
	var err error
	if ref.kind == "gallery" {
		root, err = gallery128ReadRoot(ctx, doc, page)
		if err != nil {
			return result, err
		}
	} else {
		current, err = gallery128Read(ctx, doc, ref)
		if err != nil {
			return result, err
		}
	}
	items := make(map[int]Item)
	seen := make(map[string]bool)
	finish := func(cause error) (Discovery, error) {
		order := make([]int, 0, len(items))
		for n := range items {
			order = append(order, n)
		}
		sort.Ints(order)
		for _, n := range order {
			result.Items = append(result.Items, items[n])
		}
		if cause != nil && len(result.Items) > 0 && !errors.Is(cause, context.Canceled) && !errors.Is(cause, context.DeadlineExceeded) {
			return result, fmt.Errorf("相册部分页面未完成，结果不完整：%w", cause)
		}
		return result, cause
	}
	var baseline gallery128Reader
	add := func(n int, data gallery128Reader) error {
		if baseline.total == 0 {
			if (root.total != 0 && root.total != data.total) || (root.dir != "" && (root.dir != data.dir || root.album != data.album)) {
				return errors.New("相册首页和阅读页的身份或页数不一致")
			}
			baseline = data
		} else if data.total != baseline.total || data.dir != baseline.dir || data.album != baseline.album {
			return errors.New("相册分页的身份或页数不一致")
		}
		if seen[data.image] {
			return errors.New("相册分页返回重复图片")
		}
		seen[data.image] = true
		referer := fmt.Sprintf("https://imhentai.xxx/view/%s/%d/", ref.id, n)
		items[n] = Item{URL: data.image, Headers: map[string]string{"Referer": referer}, RequireImage: true, PublicOnly: true,
			Metadata: map[string]string{"gallery128_id": ref.id, "gallery128_page": strconv.Itoa(n)}}
		return nil
	}
	if ref.kind == "view" {
		if err := add(ref.page, current); err != nil {
			return finish(err)
		}
	}
	for n := 1; ; n++ {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		if baseline.total != 0 && n > baseline.total {
			break
		}
		if ref.kind == "view" && n == ref.page {
			continue
		}
		if len(items) >= gallery128MaxPages {
			return finish(errors.New("相册超过 1000 页采集上限"))
		}
		target := &url.URL{Scheme: "https", Host: "imhentai.xxx", Path: fmt.Sprintf("/view/%s/%d/", ref.id, n)}
		child, err := gallery128Fetch(ctx, getter, target)
		if err != nil {
			return finish(fmt.Errorf("读取第 %d 页失败，可能需要登录或验证：%w", n, err))
		}
		data, err := gallery128Read(ctx, child, gallery128Ref{kind: "view", id: ref.id, page: n})
		if err != nil {
			return finish(fmt.Errorf("读取第 %d 页失败：%w", n, err))
		}
		if err := add(n, data); err != nil {
			return finish(fmt.Errorf("第 %d 页：%w", n, err))
		}
	}
	return finish(nil)
}
