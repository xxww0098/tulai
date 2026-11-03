package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

var (
	dianpingShopID     = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)
	dianpingPhotoID    = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	dianpingData       = regexp.MustCompile(`\bDP\s*\.\s*data\s*\(\s*`)
	dianpingShopKey    = regexp.MustCompile(`\bshopID\s*:\s*`)
	dianpingRecordsKey = regexp.MustCompile(`(?:'records'|"records"|\brecords\b)\s*:\s*`)
	dianpingPageKey    = regexp.MustCompile(`(?:'page'|"page"|\bpage\b)\s*:\s*`)
)

type dianpingRef struct{ shop, kind string }

func dianpingPageRef(u *url.URL) dianpingRef {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return dianpingRef{}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	host := strings.ToLower(u.Hostname())
	if host == "m.dianping.com" && len(parts) == 2 && parts[0] == "shopshare" && dianpingShopID.MatchString(parts[1]) {
		return dianpingRef{parts[1], "share"}
	}
	if host != "www.dianping.com" && host != "dianping.com" {
		return dianpingRef{}
	}
	if len(parts) < 2 || len(parts) > 4 || parts[0] != "shop" || !dianpingShopID.MatchString(parts[1]) {
		return dianpingRef{}
	}
	if len(parts) == 2 {
		return dianpingRef{parts[1], "shop"}
	}
	if len(parts) == 3 && parts[2] == "officialphotos" {
		return dianpingRef{parts[1], "official"}
	}
	if parts[2] != "photos" {
		return dianpingRef{}
	}
	if len(parts) == 3 {
		return dianpingRef{parts[1], "photos"}
	}
	if !strings.HasPrefix(parts[3], "tag-") || len(parts[3]) <= 4 || len(parts[3]) > 256 {
		return dianpingRef{}
	}
	for _, r := range parts[3] {
		if unicode.IsControl(r) {
			return dianpingRef{}
		}
	}
	return dianpingRef{parts[1], "photos"}
}

func isDianpingPage(u *url.URL) bool { return dianpingPageRef(u).shop != "" }

func dianpingPageNumber(u *url.URL) (int, bool) {
	if u == nil {
		return 0, false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return 0, false
	}
	v := q["pg"]
	if len(v) == 0 {
		return 1, true
	}
	if len(v) != 1 {
		return 0, false
	}
	n, err := strconv.Atoi(v[0])
	return n, err == nil && n > 0 && n <= 100000 && strconv.Itoa(n) == v[0]
}

func dianpingPhotoPage(u *url.URL) *url.URL {
	ref := dianpingPageRef(u)
	if ref.kind == "share" || ref.kind == "shop" {
		return &url.URL{Scheme: "https", Host: "www.dianping.com", Path: "/shop/" + ref.shop + "/photos"}
	}
	v := *u
	v.Fragment = ""
	return &v
}

func dianpingImage(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		u.RawQuery != "" || u.Fragment != "" || u.Path == "" || path.Clean(u.Path) != u.Path {
		return "", false
	}
	switch strings.ToLower(u.Hostname()) {
	case "qcloud.dpfile.com", "p0.meituan.net", "img.meituan.net":
	default:
		return "", false
	}
	return u.String(), true
}

func dianpingHasClass(n *html.Node, name string) bool {
	if n == nil || n.Type != html.ElementNode {
		return false
	}
	for _, c := range strings.Fields(attribute(n, "class")) {
		if c == name {
			return true
		}
	}
	return false
}

func dianpingGalleryPhoto(n *html.Node) bool {
	list, gallery := false, false
	for p := n; p != nil; p = p.Parent {
		if p.Data == "li" && dianpingHasClass(p, "J_list") {
			list = true
		}
		if dianpingHasClass(p, "page-gallery") {
			gallery = true
		}
	}
	return list && gallery
}

type dianpingPhoto struct {
	Full string      `json:"full"`
	ID   json.Number `json:"picId"`
}
type dianpingRecords struct {
	Images []dianpingPhoto `json:"img"`
}
type dianpingParsed struct {
	result Discovery
	next   *url.URL
}

// The gallery embeds JSON in a string-valued DP.data property. Read it as
// data, and bind every full-size URL to the photo IDs in the gallery DOM.
func dianpingParse(ctx context.Context, page *url.URL, doc *html.Node) (dianpingParsed, error) {
	out := dianpingParsed{result: Discovery{IsPage: true}}
	ref := dianpingPageRef(page)
	pageNum, validPage := dianpingPageNumber(page)
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if ref.shop == "" || (ref.kind != "photos" && ref.kind != "official") || !validPage || doc == nil {
		return out, errors.New("大众点评相册页面无效")
	}
	var scripts []string
	ids := map[string]string{}
	var nextNodes []*html.Node
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && out.result.Title == "" {
			out.result.Title = nodeText(n)
		}
		if n.Data == "script" {
			var s strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					s.WriteString(c.Data)
				}
			}
			scripts = append(scripts, s.String())
		}
		if n.Data != "a" {
			return
		}
		if dianpingHasClass(n, "NextPage") {
			nextNodes = append(nextNodes, n)
		}
		if !dianpingHasClass(n, "J_entry") || !dianpingGalleryPhoto(n) {
			return
		}
		u, err := url.Parse(attribute(n, "href"))
		if err != nil || u.User != nil || u.Host != "" || u.RawQuery != "" || u.Fragment != "" {
			return
		}
		id := strings.TrimPrefix(u.Path, "/photos/")
		if u.Path != "/photos/"+id || !dianpingPhotoID.MatchString(id) {
			return
		}
		if label := attribute(n, "title"); ids[id] == "" && label != "点击看大图" {
			ids[id] = label
		} else if _, ok := ids[id]; !ok {
			ids[id] = ""
		}
	})
	if err := ctx.Err(); err != nil {
		return out, err
	}
	var shopSeen bool
	var data []dianpingRecords
	bad := false
	for _, script := range scripts {
		for _, match := range offer1688CodeMatches(script, dianpingData) {
			obj, ok := offer1688Object(script[match[1]:])
			if !ok {
				continue
			}
			for _, k := range offer1688CodeMatches(obj, dianpingShopKey) {
				s := strings.TrimSpace(obj[k[1]:])
				value, _, ok := offer1688String(s)
				if !ok || value != ref.shop {
					bad = true
				} else {
					shopSeen = true
				}
			}
			for _, k := range offer1688CodeMatches(obj, dianpingRecordsKey) {
				s := strings.TrimSpace(obj[k[1]:])
				value, used, ok := offer1688String(s)
				if !ok || !offer1688LiteralEnd(s[used:]) {
					bad = true
					continue
				}
				var records dianpingRecords
				if json.Unmarshal([]byte(value), &records) != nil {
					bad = true
					continue
				}
				pages := offer1688CodeMatches(obj, dianpingPageKey)
				if len(pages) != 1 {
					bad = true
					continue
				}
				p := strings.TrimSpace(obj[pages[0][1]:])
				end := 0
				for end < len(p) && p[end] >= '0' && p[end] <= '9' {
					end++
				}
				n, err := strconv.Atoi(p[:end])
				if err != nil || n != pageNum || !offer1688LiteralEnd(p[end:]) {
					bad = true
					continue
				}
				data = append(data, records)
			}
		}
	}
	if !shopSeen || bad || len(data) != 1 {
		return out, errors.New("大众点评相册身份或图片记录无法确认，可能需要登录或验证")
	}
	seen := map[string]bool{}
	for _, photo := range data[0].Images {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		id := photo.ID.String()
		label, matched := ids[id]
		image, ok := dianpingImage(photo.Full)
		if !matched || !ok || !dianpingPhotoID.MatchString(id) {
			bad = true
			continue
		}
		if seen[id] {
			bad = true
			continue
		}
		seen[id] = true
		item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"dianping_shop_id": ref.shop, "dianping_photo_id": id, "dianping_title": label}}
		if r := imageReferer(page.Scheme+"://"+page.Host+"/", image); r != "" {
			item.Headers = map[string]string{"Referer": r}
		}
		out.result.Items = append(out.result.Items, item)
	}
	if len(data[0].Images) != len(ids) {
		bad = true
	}
	if len(nextNodes) > 1 {
		bad = true
	}
	for _, n := range nextNodes {
		href, err := url.Parse(attribute(n, "href"))
		if err != nil || href.User != nil {
			bad = true
			continue
		}
		next := page.ResolveReference(href)
		next.Fragment = ""
		q, e := url.ParseQuery(next.RawQuery)
		nextPage, ok := dianpingPageNumber(next)
		if e != nil || len(q) != 1 || len(q["pg"]) != 1 || !sameOrigin(page, next) || next.Path != page.Path || !ok || nextPage != pageNum+1 || attribute(n, "data-pg") != strconv.Itoa(nextPage) {
			bad = true
			continue
		}
		out.next = next
	}
	if len(out.result.Items) == 0 {
		return out, errors.New("大众点评未返回可确认的相册图片，可能需要登录或验证")
	}
	if bad {
		return out, errors.New("大众点评相册中有无法确认的图片或分页，已保留部分结果")
	}
	return out, nil
}

func dianpingFetch(ctx context.Context, getter Getter, page *url.URL, depth int) (*html.Node, *url.URL, error) {
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: depth})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		var status *HTTPStatusError
		if errors.As(err, &status) {
			return nil, nil, fmt.Errorf("大众点评页面读取失败，HTTP %d；可能需要登录或验证", status.StatusCode)
		}
		return nil, nil, errors.New("大众点评页面读取失败，可能需要登录或验证")
	}
	if doc == nil || final == nil || !sameOrigin(page, final) || final.Path != page.Path || (page.Scheme == "https" && final.Scheme != "https") {
		return nil, nil, errors.New("大众点评页面发生跳转或需要登录、验证")
	}
	want, a := dianpingPageNumber(page)
	got, b := dianpingPageNumber(final)
	if !a || !b || want != got {
		return nil, nil, errors.New("大众点评相册分页发生跳转")
	}
	return doc, final, nil
}

func discoverDianping(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isDianpingPage(root) {
		return result, errors.New("大众点评地址或下载客户端无效")
	}
	page := dianpingPhotoPage(root)
	doc, final, err := dianpingFetch(ctx, getter, page, 1)
	if err != nil {
		return result, err
	}
	return dianpingCollect(ctx, getter, final, doc)
}

// The caller may provide an authorized rendered DOM and same-origin getter.
func dianpingCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isDianpingPage(page) {
		return result, errors.New("大众点评地址或下载客户端无效")
	}
	ref := dianpingPageRef(page)
	if ref.kind == "shop" || ref.kind == "share" {
		return discoverDianping(ctx, getter, page)
	}
	seen := map[string]bool{}
	current := page
	for count := 0; ; count++ {
		parsed, err := dianpingParse(ctx, current, doc)
		if result.Title == "" {
			result.Title = parsed.result.Title
		}
		for _, item := range parsed.result.Items {
			id := item.Metadata["dianping_photo_id"]
			if !seen[id] {
				seen[id] = true
				result.Items = append(result.Items, item)
			}
		}
		if err != nil {
			return result, err
		}
		if parsed.next == nil {
			return result, nil
		}
		if count+1 >= maxCrawlPages {
			return result, errors.New("大众点评相册分页达到 100 页采集上限，已保留部分结果")
		}
		current = parsed.next
		doc, current, err = dianpingFetch(ctx, getter, current, 2)
		if err != nil {
			return result, err
		}
	}
}
