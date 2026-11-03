package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

func isBingImageSearch(u *url.URL) bool {
	if u == nil || u.User != nil || u.Path != "/images/search" || strings.TrimSpace(u.Query().Get("q")) == "" || u.Query().Has("view") {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "bing.com", "www.bing.com", "cn.bing.com":
	default:
		return false
	}
	return u.Port() == "" || (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80")
}

type bingPage struct {
	url     *url.URL
	referer string
}

// shortcut: 搜索页最多遍历 100 页。
func discoverBingImages(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true, Title: root.Query().Get("q") + " - Bing 图片"}
	queue := []bingPage{{url: root}}
	seenPages := map[string]bool{bingPageKey(root): true}
	seenImages := map[string]bool{}
	var origin *url.URL
	var dynamic *bingDynamic
	var failures []error
	missing := 0
	finish := func(err error) (Discovery, error) {
		if missing > 0 {
			failures = append(failures, fmt.Errorf("Bing 图片有 %d 条结果缺少有效原图地址", missing))
		}
		return result, errors.Join(append(failures, err)...)
	}
	for count := 0; len(queue) > 0; count++ {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		if count >= maxCrawlPages {
			return finish(fmt.Errorf("Bing 图片已达到 %d 页的扫描上限，结果可能不完整", maxCrawlPages))
		}
		page := queue[0]
		queue = queue[1:]
		doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.url.String(), referer: page.referer, depth: 1})
		if response != nil {
			response.Body.Close()
		}
		if err != nil {
			if ctx.Err() != nil {
				return finish(ctx.Err())
			}
			return finish(fmt.Errorf("读取 Bing 图片搜索页失败，结果可能不完整: %w", err))
		}
		if doc == nil || final.User != nil || (origin != nil && !sameOrigin(origin, final)) || (origin == nil && !bingSearchRedirect(root, final)) {
			return finish(errors.New("Bing 图片搜索页不可用或跳转到了其他站点，结果可能不完整"))
		}
		if origin == nil {
			origin = final
			if dynamic = bingDynamicConfig(doc, final); dynamic != nil {
				// The page advertises the async endpoint; keep the user's search filters.
				dynamic.query = root.Query()
			}
		}
		pageCount, added := 0, 0
		var next []string
		grid, noResults, blocked := false, false, false
		walkHTML(doc, func(n *html.Node) {
			if n.Type == html.TextNode && n.Parent != nil && n.Parent.Data != "script" && n.Parent.Data != "style" {
				value := strings.ToLower(n.Data)
				noResults = noResults || strings.Contains(value, "no results found") || strings.Contains(value, "there are no results") || strings.Contains(value, "没有结果") || strings.Contains(value, "找不到任何结果")
				blocked = blocked || strings.Contains(value, "verify you are human") || strings.Contains(value, "verify you are a human") || strings.Contains(value, "unusual traffic") || strings.Contains(value, "验证码")
			}
			if n.Type != html.ElementNode {
				return
			}
			if count == 0 && n.Data == "title" && nodeText(n) != "" {
				result.Title = nodeText(n)
			}
			class := " " + strings.Join(strings.Fields(attribute(n, "class")), " ") + " "
			grid = grid || strings.Contains(class, " dgControl ")
			for _, key := range []string{"data-nexturl", "data-nurl"} {
				if value := attribute(n, key); value != "" {
					next = append(next, strings.ReplaceAll(value, "%SFX%", "1"))
				}
			}
			if n.Data != "a" || !strings.Contains(class, " iusc ") {
				return
			}
			pageCount++
			var data struct {
				Image  string `json:"murl"`
				Source string `json:"purl"`
			}
			if json.Unmarshal([]byte(attribute(n, "m")), &data) != nil {
				missing++
				return
			}
			image, err := crawlURL(nil, data.Image)
			if err != nil || image.User != nil {
				missing++
				return
			}
			if seenImages[image.String()] {
				return
			}
			seenImages[image.String()] = true
			added++
			from := pageReferer(final)
			if source, err := crawlURL(nil, data.Source); err == nil {
				from = pageReferer(source)
			}
			item := Item{URL: image.String(), PublicOnly: true}
			if referer := imageReferer(from, item.URL); referer != "" {
				item.Headers = map[string]string{"Referer": referer}
			}
			result.Items = append(result.Items, item)
		})
		if blocked {
			return finish(errors.New("Bing 图片搜索要求验证，结果可能不完整"))
		}
		if pageCount == 0 {
			if noResults || (count > 0 && grid) {
				continue
			}
			return finish(errors.New("Bing 图片搜索页未返回可识别的结果，结果可能不完整"))
		}
		if count > 0 && added == 0 {
			return finish(errors.New("Bing 图片分页没有返回新图片，结果可能不完整"))
		}
		if len(next) == 0 && dynamic != nil {
			first, err := bingFirst(page.url)
			if err != nil || first > int(^uint(0)>>1)-pageCount {
				return finish(errors.New("Bing 图片分页位置无效，结果可能不完整"))
			}
			next = append(next, dynamic.next(first+pageCount))
		}
		queued := false
		for _, raw := range next {
			u, err := crawlURL(final, raw)
			if err != nil || u.User != nil || !sameOrigin(origin, u) || (u.Path != "/images/search" && u.Path != "/images/async") {
				failures = append(failures, errors.New("Bing 图片分页地址无效或不属于搜索站点，结果可能不完整"))
				continue
			}
			query := u.Query()
			for key, values := range root.Query() {
				if key != "first" && key != "count" && key != "async" && !query.Has(key) {
					query[key] = append([]string(nil), values...)
				}
			}
			u.RawQuery = query.Encode()
			key := bingPageKey(u)
			if seenPages[key] {
				continue
			}
			seenPages[key] = true
			queue = append(queue, bingPage{url: u, referer: pageReferer(final)})
			queued = true
		}
		if len(next) > 0 && !queued && len(queue) == 0 {
			return finish(errors.New("Bing 图片分页重复，结果可能不完整"))
		}
	}
	return finish(nil)
}

func bingPageKey(u *url.URL) string {
	copy := *u
	copy.RawQuery = u.Query().Encode()
	return copy.String()
}

func bingSearchRedirect(from, to *url.URL) bool {
	if to == nil || to.User != nil || (from.Scheme == "https" && to.Scheme != "https") {
		return false
	}
	return sameOrigin(from, to) || (to.Scheme == "https" && isBingImageSearch(to))
}

func bingFirst(u *url.URL) (int, error) {
	if u.Query().Get("first") == "" {
		return 1, nil
	}
	first, err := strconv.Atoi(u.Query().Get("first"))
	if err != nil || first < 0 {
		return 0, errors.New("invalid first")
	}
	if first == 0 {
		first = 1
	}
	return first, nil
}

type bingDynamic struct {
	base     *url.URL
	template string
	query    url.Values
	count    int
}

var bingConfigAssignment = regexp.MustCompile(`(?i)(?:_w|window)\s*\[\s*["']DgConfig["']\s*\]\s*=\s*`)
var bingPlaceholder = regexp.MustCompile(`%[A-Z]+%`)

func bingDynamicConfig(doc *html.Node, base *url.URL) *bingDynamic {
	var result *bingDynamic
	walkHTML(doc, func(n *html.Node) {
		if result != nil || n.Type != html.ElementNode || n.Data != "script" {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			loc := bingConfigAssignment.FindStringIndex(child.Data)
			if loc == nil {
				continue
			}
			var conf struct {
				URL   string `json:"aurl"`
				Count int    `json:"ac"`
			}
			if json.NewDecoder(strings.NewReader(child.Data[loc[1]:])).Decode(&conf) != nil || conf.Count < 1 || conf.Count > 100 || !strings.Contains(conf.URL, "%QUERY%") || !strings.Contains(conf.URL, "%FIRST%") || !strings.Contains(conf.URL, "%COUNT%") {
				continue
			}
			valid := true
			for _, part := range bingPlaceholder.FindAllString(conf.URL, -1) {
				switch part {
				case "%QUERY%", "%FIRST%", "%COUNT%", "%DGSTATE%", "%IID%", "%SFX%", "%IG%", "%CW%", "%CH%", "%CT%":
				default:
					valid = false
				}
			}
			if !valid {
				continue
			}
			sample := bingPlaceholder.ReplaceAllString(conf.URL, "1")
			u, err := crawlURL(base, sample)
			if err == nil && u.User == nil && sameOrigin(base, u) && u.Path == "/images/async" &&
				u.Query().Get("async") == "content" && u.Query().Get("q") == "1" && u.Query().Get("first") == "1" && u.Query().Get("count") == "1" {
				result = &bingDynamic{base: base, template: conf.URL, count: conf.Count}
			}
		}
	})
	return result
}

func (b *bingDynamic) next(first int) string {
	raw := strings.NewReplacer(
		"%QUERY%", url.QueryEscape(b.query.Get("q")),
		"%FIRST%", strconv.Itoa(first), "%COUNT%", strconv.Itoa(b.count),
		"%DGSTATE%", "", "%IID%", "", "%SFX%", "0", "%IG%", "", "%CW%", "", "%CH%", "", "%CT%", "",
	).Replace(b.template)
	u, err := crawlURL(b.base, raw)
	if err != nil {
		return ""
	}
	q := u.Query()
	for key, values := range b.query {
		if key != "first" && key != "count" && key != "async" {
			q[key] = append([]string(nil), values...)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

var bingDetailID = regexp.MustCompile(`(?i)^[a-f0-9]{40}$`)

func isBingImageDetail(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Path != "/images/search" && u.Path != "/images/async") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "bing.com", "www.bing.com", "cn.bing.com":
	default:
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	return err == nil && len(q["view"]) == 1 && strings.EqualFold(q.Get("view"), "detailv2")
}
func bingDetailKey(u *url.URL) string {
	if !isBingImageDetail(u) {
		return ""
	}
	q := u.Query()
	if len(q["id"]) != 1 || !bingDetailID.MatchString(q.Get("id")) {
		return ""
	}
	return strings.ToUpper(q.Get("id"))
}

type bingDetailTarget struct{ id, thumb, query string }

func bingDetailTargetOf(u *url.URL) (bingDetailTarget, error) {
	var out bingDetailTarget
	id := bingDetailKey(u)
	if id == "" {
		return out, errors.New("Bing 单图详情缺少可确认的图片 ID")
	}
	q := u.Query()
	if len(q["q"]) != 1 || strings.TrimSpace(q.Get("q")) == "" || len(q.Get("q")) > 2048 {
		return out, errors.New("Bing 单图详情缺少可用于验证的搜索词")
	}
	if len(q["thid"]) > 1 || len(q.Get("thid")) > 256 || strings.ContainsAny(q.Get("thid"), "\r\n\x00") {
		return out, errors.New("Bing 单图详情缩略图标识无效")
	}
	return bingDetailTarget{id: id, thumb: q.Get("thid"), query: q.Get("q")}, nil
}
func bingDetailSameRequest(a, b *url.URL) bool {
	x, err := bingDetailTargetOf(a)
	if err != nil {
		return false
	}
	y, err := bingDetailTargetOf(b)
	return err == nil && x == y && !(a.Scheme == "https" && b.Scheme != "https") &&
		bingDetailSearchURL(a).Query().Encode() == bingDetailSearchURL(b).Query().Encode()
}
func bingDetailSameTarget(target bingDetailTarget, id, thumb string) bool {
	return strings.EqualFold(target.id, id) && (target.thumb == "" || target.thumb == thumb)
}
func bingDetailPageCheck(doc *html.Node, target bingDetailTarget) error {
	if doc == nil {
		return errors.New("Bing 单图页面不可用")
	}
	found, blocked := false, false
	walkHTML(doc, func(n *html.Node) {
		if n.Type == html.TextNode && n.Parent != nil && n.Parent.Data != "script" && n.Parent.Data != "style" {
			text := strings.ToLower(n.Data)
			blocked = blocked || strings.Contains(text, "verify you are human") || strings.Contains(text, "verify you are a human") || strings.Contains(text, "unusual traffic") || strings.Contains(text, "验证码")
		}
		if n.Type != html.ElementNode {
			return
		}
		if attribute(n, "id") == "b_idpdata" {
			var data struct {
				ID    string `json:"id"`
				Thumb string `json:"thumbId"`
			}
			if json.Unmarshal([]byte(attribute(n, "data-firstimg")), &data) == nil && bingDetailSameTarget(target, data.ID, data.Thumb) {
				found = true
			}
		}
		if encoded := attribute(n, "json-data"); encoded != "" {
			if decoded, err := url.QueryUnescape(encoded); err == nil {
				var data struct {
					Web map[string]struct {
						ID    string `json:"mid"`
						Thumb string `json:"thId"`
					} `json:"web"`
				}
				if json.Unmarshal([]byte(decoded), &data) == nil {
					for _, item := range data.Web {
						if bingDetailSameTarget(target, item.ID, item.Thumb) {
							found = true
						}
					}
				}
			}
		}
	})
	if blocked {
		return errors.New("Bing 图片要求安全验证，请先完成验证")
	}
	if !found {
		return errors.New("Bing 单图页面未返回可确认的图片身份")
	}
	return nil
}
func bingDetailGetter(getter Getter) Getter {
	g, ok := getter.(*HTTPGetter)
	if !ok || g == nil || g.client == nil {
		return getter
	}
	copyGetter, client := *g, *g.client
	previous := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		u := req.URL
		if u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") {
			return errors.New("Bing 页面发生不安全跳转")
		}
		switch strings.ToLower(u.Hostname()) {
		case "bing.com", "www.bing.com", "cn.bing.com":
		default:
			return errors.New("Bing 页面跳转到其他站点")
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return errors.New("Bing 页面重定向次数过多")
		}
		return nil
	}
	copyGetter.client = &client
	return &copyGetter
}
func discoverBingDetail(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	_, err := bingDetailTargetOf(root)
	if err != nil {
		return result, err
	}
	if getter == nil {
		return result, errors.New("没有配置 Bing 下载客户端")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, bingDetailGetter(getter), crawlPage{url: root.String(), depth: 1})
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		var status *HTTPStatusError
		if errors.As(err, &status) {
			return result, fmt.Errorf("Bing 单图页面读取失败：HTTP %d", status.StatusCode)
		}
		return result, errors.New("Bing 单图页面读取失败，请检查网络或验证状态")
	}
	if !bingDetailSameRequest(root, final) {
		return result, errors.New("Bing 单图页面已跳转到其他图片")
	}
	return bingDetailCollect(ctx, getter, final, doc)
}

// Search parameters are retained while detail-only identity and media URL
// parameters are omitted. The original image comes from an independently
// returned Bing search record with the same media ID and detail link.
func bingDetailSearchURL(page *url.URL) *url.URL {
	u := *page
	u.Path, u.RawPath, u.Fragment = "/images/search", "", ""
	q := u.Query()
	for key := range q {
		switch strings.ToLower(key) {
		case "view", "id", "thid", "mediaurl", "cdnurl", "ccid", "ck", "exph", "expw", "selectedindex", "itb", "first", "count", "async":
			delete(q, key)
		}
	}
	u.RawQuery = q.Encode()
	return &u
}
func bingDetailCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	target, err := bingDetailTargetOf(page)
	if err != nil {
		return result, err
	}
	if getter == nil {
		return result, errors.New("没有配置 Bing 下载客户端")
	}
	if err := bingDetailPageCheck(doc, target); err != nil {
		return result, err
	}
	result.Title = target.query + " - Bing 图片"
	search := bingDetailSearchURL(page)
	current := search
	seen := map[string]bool{}
	var dynamic *bingDynamic
	for step := 0; step < 10; step++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		key := bingPageKey(current)
		if seen[key] {
			return result, errors.New("Bing 单图匹配分页重复，无法确认图片")
		}
		seen[key] = true
		d, final, _, response, err := fetchCrawlPage(ctx, bingDetailGetter(getter), crawlPage{url: current.String(), depth: 1})
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			var status *HTTPStatusError
			if errors.As(err, &status) {
				return result, fmt.Errorf("Bing 单图匹配失败：HTTP %d", status.StatusCode)
			}
			return result, errors.New("Bing 单图匹配搜索失败")
		}
		if d == nil || final == nil || final.User != nil || (search.Scheme == "https" && final.Scheme != "https") ||
			(final.Path != "/images/search" && final.Path != "/images/async") || final.Query().Get("q") != target.query {
			return result, errors.New("Bing 单图匹配搜索发生跳转")
		}
		switch strings.ToLower(final.Hostname()) {
		case "bing.com", "www.bing.com", "cn.bing.com":
		default:
			return result, errors.New("Bing 单图匹配搜索离开站点")
		}
		if step == 0 {
			dynamic = bingDynamicConfig(d, final)
			if dynamic != nil {
				dynamic.query = search.Query()
			}
		}
		var found *Item
		var conflict, blocked bool
		pageCount := 0
		var next []string
		walkHTML(d, func(n *html.Node) {
			if n.Type == html.TextNode && n.Parent != nil && n.Parent.Data != "script" && n.Parent.Data != "style" {
				text := strings.ToLower(n.Data)
				blocked = blocked || strings.Contains(text, "verify you are human") || strings.Contains(text, "verify you are a human") || strings.Contains(text, "unusual traffic") || strings.Contains(text, "验证码")
			}
			if n.Type != html.ElementNode {
				return
			}
			for _, key := range []string{"data-nexturl", "data-nurl"} {
				if raw := attribute(n, key); raw != "" {
					next = append(next, strings.ReplaceAll(raw, "%SFX%", "1"))
				}
			}
			if n.Data != "a" || !strings.Contains(" "+strings.Join(strings.Fields(attribute(n, "class")), " ")+" ", " iusc ") {
				return
			}
			pageCount++
			var data struct {
				ID     string `json:"mid"`
				Image  string `json:"murl"`
				Source string `json:"purl"`
			}
			if json.Unmarshal([]byte(attribute(n, "m")), &data) != nil || !strings.EqualFold(data.ID, target.id) {
				return
			}
			href, err := crawlURL(final, attribute(n, "href"))
			if err != nil || !isBingImageDetail(href) || bingDetailKey(href) != target.id {
				return
			}
			if len(href.Query()["q"]) != 1 || href.Query().Get("q") != target.query {
				return
			}
			if target.thumb != "" && href.Query().Get("thid") != target.thumb {
				return
			}
			image, err := publicImageURL(data.Image)
			if err != nil {
				return
			}
			item := Item{URL: image.String(), RequireImage: true, PublicOnly: true, Metadata: map[string]string{"bing_image_id": target.id}}
			if source, err := publicImageURL(data.Source); err == nil {
				origin := source.Scheme + "://" + source.Host + "/"
				if ref := imageReferer(origin, item.URL); ref != "" {
					item.Headers = map[string]string{"Referer": ref}
				}
			}
			if found != nil && found.URL != item.URL {
				conflict = true
				return
			}
			found = &item
		})
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if blocked {
			return result, errors.New("Bing 图片要求安全验证，请先完成验证")
		}
		if conflict {
			return result, errors.New("Bing 单图搜索存在冲突的图片地址")
		}
		if found != nil {
			result.Items = []Item{*found}
			return result, nil
		}
		if pageCount == 0 {
			return result, errors.New("Bing 未返回可用于核对单图的搜索结果")
		}
		if len(next) == 0 && dynamic != nil {
			first, err := bingFirst(final)
			if err != nil || first > int(^uint(0)>>1)-pageCount {
				return result, errors.New("Bing 图片分页位置无效")
			}
			next = append(next, dynamic.next(first+pageCount))
		}
		var chosen *url.URL
		for _, raw := range next {
			u, err := crawlURL(final, raw)
			if err != nil || u.User != nil || !sameOrigin(final, u) || (u.Path != "/images/search" && u.Path != "/images/async") || u.Query().Has("view") {
				continue
			}
			q := u.Query()
			for key, values := range search.Query() {
				if key != "first" && key != "count" && key != "async" && !q.Has(key) {
					q[key] = append([]string(nil), values...)
				}
			}
			u.RawQuery = q.Encode()
			if q.Get("q") != target.query || seen[bingPageKey(u)] {
				continue
			}
			chosen = u
			break
		}
		if chosen == nil {
			return result, errors.New("Bing 当前搜索结果中未找到与单图 ID 对应的原图")
		}
		current = chosen
	}
	return result, errors.New("Bing 已核对前 10 页搜索结果，仍未找到对应原图")
}
