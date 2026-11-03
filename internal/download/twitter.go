package download

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/chromedp/chromedp"
	"golang.org/x/net/html"
)

var twitterMediaPath = regexp.MustCompile(`(?i)^/media/([a-z0-9_-]+)(?:\.(jpg|jpeg|png|webp))?(?::(small|medium|large|orig))?$`)

var twitterStatusPath = regexp.MustCompile(`^/([A-Za-z0-9_]{1,15})/status/([1-9][0-9]{0,19})(?:/photo/([1-9][0-9]?))?/?$`)
var twitterShortStatusPath = regexp.MustCompile(`^/i/(?:web/)?status/([1-9][0-9]{0,19})/?$`)
var twitterProfilePath = regexp.MustCompile(`^/([A-Za-z0-9_]{1,15})/?$`)
var twitterProfileMediaPath = regexp.MustCompile(`^/([A-Za-z0-9_]{1,15})/media/?$`)

type twitterStatusRef struct {
	author, id string
	photo      int
}

// Short status URLs identify a post but have no author until X resolves them.
func twitterStatus(u *url.URL) twitterStatusRef {
	if u == nil || len(u.String()) > 8192 || u.User != nil || u.RawPath != "" ||
		(u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "http" && u.Port() == "80") && !(u.Scheme == "https" && u.Port() == "443")) {
		return twitterStatusRef{}
	}
	switch strings.ToLower(u.Hostname()) {
	case "x.com", "www.x.com", "twitter.com", "www.twitter.com", "mobile.twitter.com":
	default:
		return twitterStatusRef{}
	}
	if short := twitterShortStatusPath.FindStringSubmatch(u.Path); short != nil {
		return twitterStatusRef{id: short[1]}
	}
	m := twitterStatusPath.FindStringSubmatch(u.Path)
	if m == nil || strings.EqualFold(m[1], "i") {
		return twitterStatusRef{}
	}
	photo := 0
	if m[3] != "" {
		photo, _ = strconv.Atoi(m[3])
		if photo < 1 || photo > 20 {
			return twitterStatusRef{}
		}
	}
	return twitterStatusRef{author: strings.ToLower(m[1]), id: m[2], photo: photo}
}

func isTwitterStatus(u *url.URL) bool { return twitterStatus(u).id != "" }

func twitterProfile(u *url.URL) string {
	if u == nil || u.RawPath != "" {
		return ""
	}
	m := twitterProfilePath.FindStringSubmatch(u.Path)
	if m == nil {
		return ""
	}
	author := strings.ToLower(m[1])
	switch author {
	case "i", "home", "explore", "search", "notifications", "messages", "bookmarks", "lists", "profile", "settings", "login", "logout", "signup", "tos", "privacy", "help", "about", "download", "share", "intent", "compose", "account", "jobs", "premium", "communities", "hashtag":
		return ""
	}
	copy := *u
	copy.Path = "/" + author + "/status/1"
	if twitterStatus(&copy).author != author {
		return ""
	}
	return author
}

func isTwitterProfile(u *url.URL) bool { return twitterProfile(u) != "" }

func isTwitterProfileMedia(u *url.URL) bool {
	if u == nil || u.RawPath != "" {
		return false
	}
	m := twitterProfileMediaPath.FindStringSubmatch(u.Path)
	if m == nil {
		return false
	}
	copy := *u
	copy.Path = "/" + m[1]
	return twitterProfile(&copy) != ""
}

func twitterSameProfile(start, final *url.URL) bool {
	author := twitterProfile(start)
	return author != "" && author == twitterProfile(final) && final.Scheme == "https"
}

// X and Twitter are official URL aliases. A navigation may upgrade HTTP and
// drop a /photo/N suffix, but may never change the author or post ID.
func twitterSameStatus(a, b *url.URL) bool {
	x, y := twitterStatus(a), twitterStatus(b)
	if x.id == "" || y.id == "" || x.id != y.id || y.author == "" || (x.author != "" && x.author != y.author) || b.Scheme != "https" {
		return false
	}
	return x.photo == 0 || y.photo == 0 || x.photo == y.photo
}

// Retain an explicitly requested photo index; a short URL can only be used
// after X resolves it to an author-bound URL for the same status.
func twitterCapturePage(start, final *url.URL) *url.URL {
	if !twitterSameStatus(start, final) {
		return nil
	}
	if twitterStatus(start).author != "" {
		return start
	}
	return final
}

// The ordinary downloader follows redirects automatically. Clone its client
// so every destination is checked before the request is issued.
func twitterGuardedGetter(getter Getter, allowed func(*http.Request, []*http.Request) bool) (Getter, error) {
	source, ok := getter.(*HTTPGetter)
	if !ok || source == nil || source.client == nil {
		return nil, errors.New("X 采集需要支持安全重定向的下载客户端")
	}
	clone, client := *source, *source.client
	previous := client.CheckRedirect
	client.Jar = nil
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 6 {
			return errors.New("X 页面重定向次数过多")
		}
		if !allowed(req, via) {
			return errors.New("X 页面跳转到不受支持的地址")
		}
		if previous != nil {
			if err := previous(req, via); err != nil {
				return err
			}
		}
		if !allowed(req, via) {
			return errors.New("X 页面跳转到不受支持的地址")
		}
		if len(via) > 0 && !sameOrigin(via[0].URL, req.URL) {
			req.Header.Del("Cookie")
			req.Header.Del("Authorization")
			if ref, err := url.Parse(req.Header.Get("Referer")); err == nil && ref.Host != "" {
				req.Header.Set("Referer", ref.Scheme+"://"+ref.Host+"/")
			}
		}
		return nil
	}
	clone.client = &client
	return &clone, nil
}

func twitterPageGetter(getter Getter, start *url.URL) (Getter, error) {
	initial := twitterStatus(start)
	return twitterGuardedGetter(getter, func(req *http.Request, via []*http.Request) bool {
		next := twitterStatus(req.URL)
		if req.URL.Scheme != "https" || next.id == "" || next.id != initial.id {
			return false
		}
		// Once an author is known, subsequent redirects may not discard or change it.
		for _, r := range via {
			prior := twitterStatus(r.URL)
			if prior.author != "" && (next.author != prior.author || (prior.photo > 0 && next.photo > 0 && prior.photo != next.photo)) {
				return false
			}
		}
		return true
	})
}

func twitterAsset(raw string) (string, string, bool) {
	if len(raw) == 0 || len(raw) > 8192 {
		return "", "", false
	}
	u, err := crawlURL(nil, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "pbs.twimg.com") ||
		(u.Port() != "" && u.Port() != "443") || u.RawPath != "" {
		return "", "", false
	}
	m := twitterMediaPath.FindStringSubmatch(u.Path)
	if m == nil {
		return "", "", false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", "", false
	}
	for key, values := range q {
		if (key != "format" && key != "name") || len(values) != 1 || values[0] == "" {
			return "", "", false
		}
	}
	format := strings.ToLower(q.Get("format"))
	if format != "" && format != "jpg" && format != "jpeg" && format != "png" && format != "webp" {
		return "", "", false
	}
	if m[2] == "" && format == "" {
		return "", "", false
	}
	return u.String(), m[1], true
}

func twitterVisible(n *html.Node) bool {
	for p := n; p != nil; p = p.Parent {
		for _, a := range p.Attr {
			key, value := strings.ToLower(a.Key), strings.ToLower(strings.TrimSpace(a.Val))
			if key == "hidden" || (key == "aria-hidden" && value == "true") {
				return false
			}
			if p.Data == "img" && (key == "width" || key == "height") && twitterCSSZero(value) {
				return false
			}
			if key == "style" {
				for _, rule := range strings.Split(value, ";") {
					k, v, ok := strings.Cut(rule, ":")
					k = strings.TrimSpace(k)
					v = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "!important"))
					if ok && ((k == "display" && v == "none") || (k == "visibility" && (v == "hidden" || v == "collapse")) ||
						(k == "opacity" && twitterCSSZero(v)) || ((k == "width" || k == "height") && twitterCSSZero(strings.TrimSuffix(v, "px")))) {
						return false
					}
				}
			}
		}
	}
	return true
}

func twitterCSSZero(value string) bool {
	value = strings.TrimSuffix(strings.TrimSpace(value), "%")
	number, err := strconv.ParseFloat(value, 64)
	return err == nil && number == 0
}

func twitterLoginWall(doc *html.Node) bool {
	blocked := false
	walkHTML(doc, func(n *html.Node) {
		if blocked || n.Type != html.ElementNode || !twitterVisible(n) {
			return
		}
		if n.Data == "input" && (strings.EqualFold(attribute(n, "type"), "password") || strings.EqualFold(attribute(n, "autocomplete"), "username")) {
			blocked = true
			return
		}
		if attribute(n, "role") == "dialog" {
			text := strings.ToLower(nodeText(n))
			if strings.Contains(text, "log in to x") || strings.Contains(text, "sign in to x") || strings.Contains(text, "登录 x") || strings.Contains(text, "登录到 x") {
				blocked = true
			}
		}
	})
	return blocked
}

// The observed public server-rendered page exposes photo permalinks. Resolve
// an image only within the closest unambiguous photo container; never scan a
// whole article's avatars, quoted posts, or replies.
func twitterPhotoInContainer(a, article *html.Node, page *url.URL, want twitterStatusRef) (string, string, bool) {
	for container, depth := a, 0; container != nil && container != article && depth < 12; container, depth = container.Parent, depth+1 {
		var src, mediaID string
		images, links := 0, 0
		matched := false
		walkHTML(container, func(n *html.Node) {
			if n.Type != html.ElementNode || !twitterVisible(n) {
				return
			}
			if n.Data == "article" {
				return
			}
			if n.Data == "img" {
				raw, id, ok := twitterAsset(attribute(n, "src"))
				if ok {
					images++
					src, mediaID = raw, id
				}
			}
			if n.Data == "a" {
				u, err := crawlURL(page, attribute(n, "href"))
				if err != nil {
					return
				}
				r := twitterStatus(u)
				if r.photo > 0 {
					links++
					matched = r == want
				}
			}
		})
		if images == 0 {
			continue
		}
		if images != 1 || links != 1 || !matched {
			return "", "", false
		}
		return src, mediaID, true
	}
	return "", "", false
}

func twitterCollect(ctx context.Context, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := twitterStatus(page)
	if ref.id == "" || ref.author == "" || doc == nil {
		return result, errors.New("X 帖子地址或页面无效")
	}
	if page.Scheme == "http" {
		upgraded := *page
		upgraded.Scheme, upgraded.Host = "https", upgraded.Hostname()
		page = &upgraded
	}
	result.Title = "@" + ref.author + "/status/" + ref.id
	if twitterLoginWall(doc) {
		return result, errors.New("X 页面需要登录或授权后才能查看")
	}
	var canonical string
	walkHTML(doc, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:url") && canonical == "" {
			canonical = attribute(n, "content")
		}
	})
	if canonical != "" {
		u, err := crawlURL(page, canonical)
		if err != nil || !twitterSameStatus(page, u) {
			return result, errors.New("X 页面帖子或作者不匹配")
		}
	}
	type candidate struct{ raw, mediaID string }
	found := map[int]candidate{}
	conflict := map[int]bool{}
	invalid := false
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "a" || !twitterVisible(n) {
			return
		}
		u, err := crawlURL(page, attribute(n, "href"))
		if err != nil {
			return
		}
		r := twitterStatus(u)
		if r.id != ref.id || r.author != ref.author || r.photo == 0 || (ref.photo > 0 && r.photo != ref.photo) {
			return
		}
		var article *html.Node
		for p := n.Parent; p != nil; p = p.Parent {
			if p.Data == "article" {
				article = p
				break
			}
		}
		if article == nil || !twitterVisible(article) {
			invalid = true
			return
		}
		raw, id, ok := twitterPhotoInContainer(n, article, page, r)
		if !ok {
			invalid = true
			return
		}
		if prior, exists := found[r.photo]; exists {
			if prior.mediaID != id {
				delete(found, r.photo)
				conflict[r.photo] = true
				invalid = true
			}
			return
		}
		if !conflict[r.photo] {
			found[r.photo] = candidate{raw: raw, mediaID: id}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	indices := make([]int, 0, len(found))
	for index := range found {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		candidate := found[index]
		primary, alternatives := twitterOriginalURLs(page.String(), candidate.raw)
		item := Item{URL: primary, AlternateURLs: alternatives, Filename: fmt.Sprintf("twitter-%s-%02d", ref.id, index), RequireImage: true, PublicOnly: true,
			Metadata: map[string]string{"twitter_status_id": ref.id, "twitter_author": ref.author, "twitter_photo_index": strconv.Itoa(index)}}
		if referer := imageReferer(page.String(), primary); referer != "" {
			item.Headers = map[string]string{"Referer": referer}
		}
		result.Items = append(result.Items, item)
	}
	if len(result.Items) == 0 {
		return result, errors.New("X 帖子未显示可确认的图片，可能需要登录或授权")
	}
	if ref.photo == 0 {
		for i := 1; i <= indices[len(indices)-1]; i++ {
			if _, ok := found[i]; !ok {
				invalid = true
				break
			}
		}
	}
	if invalid {
		return result, errors.New("X 部分图片尚未加载或身份有冲突，已保留可确认的图片")
	}
	return result, nil
}

func discoverTwitterStatus(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isTwitterStatus(page) {
		return result, errors.New("X 帖子地址或下载客户端无效")
	}
	requested := *page
	if requested.Scheme == "http" {
		requested.Scheme, requested.Host = "https", requested.Hostname()
	}
	guarded, err := twitterPageGetter(getter, &requested)
	if err != nil {
		return result, err
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, guarded, crawlPage{url: requested.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, fmt.Errorf("读取 X 帖子失败: %w", err)
	}
	capturePage := twitterCapturePage(&requested, final)
	if doc == nil || capturePage == nil {
		return result, errors.New("X 帖子已跳转，可能需要登录或授权")
	}
	return twitterCollect(ctx, capturePage, doc)
}

// shortcut: only the current author page is examined; add verified pagination
// when the live timeline exposes a supported continuation mechanism.
const maxTwitterProfileArticles = 1000

func twitterProfileProtected(doc *html.Node) bool {
	protected := false
	walkHTML(doc, func(n *html.Node) {
		if protected || n.Type != html.ElementNode || !twitterVisible(n) {
			return
		}
		label := strings.ToLower(attribute(n, "aria-label"))
		if attribute(n, "data-testid") == "icon-lock" || (n.Data == "svg" && strings.Contains(label, "protected")) {
			protected = true
		}
	})
	return protected
}

func twitterContainingArticle(n *html.Node) *html.Node {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && p.Data == "article" {
			return p
		}
	}
	return nil
}

// The public profile response places the author link and the unnumbered post
// link together in the header, before its separate photo container.
func twitterProfileHeaderMatches(link, article *html.Node, page *url.URL, author string) bool {
	for node, depth := link.Parent, 0; node != nil && node != article && depth < 6; node, depth = node.Parent, depth+1 {
		authorLink, photoLink := false, false
		walkHTML(node, func(n *html.Node) {
			if n.Type != html.ElementNode || n.Data != "a" || !twitterVisible(n) || twitterContainingArticle(n) != article {
				return
			}
			u, err := crawlURL(page, attribute(n, "href"))
			if err != nil || u.Scheme != "https" {
				return
			}
			authorLink = authorLink || twitterProfile(u) == author
			photoLink = photoLink || twitterStatus(u).photo != 0
		})
		if authorLink && !photoLink {
			return true
		}
	}
	return false
}

func twitterProfileCollect(ctx context.Context, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	author := twitterProfile(page)
	if author == "" || doc == nil {
		return result, errors.New("X 作者主页地址或页面无效")
	}
	if page.Scheme == "http" {
		upgraded := *page
		upgraded.Scheme, upgraded.Host = "https", upgraded.Hostname()
		page = &upgraded
	}
	result.Title = "@" + author + " - X 图片"
	if twitterLoginWall(doc) {
		return result, errors.New("X 作者主页需要登录或授权后才能查看")
	}
	if twitterProfileProtected(doc) {
		return result, errors.New("X 页面包含受保护账号或帖子，已停止采集")
	}
	canonical, mismatch := false, false
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "meta" || !strings.EqualFold(attribute(n, "property"), "og:url") {
			return
		}
		canonical = true
		u, err := crawlURL(page, attribute(n, "content"))
		if err != nil || !twitterSameProfile(page, u) {
			mismatch = true
		}
	})
	if !canonical || mismatch {
		return result, errors.New("X 页面作者无法确认或与请求不匹配")
	}
	type candidate struct {
		ref          twitterStatusRef
		raw, mediaID string
	}
	found, conflict := map[string]candidate{}, map[string]bool{}
	var order []string
	articles, incomplete := 0, false
	walkHTML(doc, func(article *html.Node) {
		if ctx.Err() != nil || article.Type != html.ElementNode || article.Data != "article" || !twitterVisible(article) {
			return
		}
		if twitterContainingArticle(article) != nil {
			incomplete = true
			return
		}
		articles++
		if articles > maxTwitterProfileArticles {
			incomplete = true
			return
		}
		bases := map[string][]*html.Node{}
		walkHTML(article, func(n *html.Node) {
			if n.Type != html.ElementNode || n.Data != "a" || !twitterVisible(n) || twitterContainingArticle(n) != article {
				return
			}
			u, err := crawlURL(page, attribute(n, "href"))
			if err != nil || u.Scheme != "https" {
				return
			}
			ref := twitterStatus(u)
			if ref.id != "" && ref.author != "" && ref.photo == 0 {
				key := ref.author + ":" + ref.id
				bases[key] = append(bases[key], n)
			}
		})
		if len(bases) != 1 {
			incomplete = true
			return
		}
		var base twitterStatusRef
		var header bool
		for _, links := range bases {
			for _, link := range links {
				u, _ := crawlURL(page, attribute(link, "href"))
				base = twitterStatus(u)
				header = header || twitterProfileHeaderMatches(link, article, page, author)
			}
		}
		if base.author != author || !header {
			incomplete = true
			return
		}
		walkHTML(article, func(n *html.Node) {
			if n.Type != html.ElementNode || n.Data != "a" || !twitterVisible(n) || twitterContainingArticle(n) != article {
				return
			}
			u, err := crawlURL(page, attribute(n, "href"))
			if err != nil || u.Scheme != "https" {
				return
			}
			ref := twitterStatus(u)
			if ref.photo == 0 {
				return
			}
			if ref.author != author || ref.id != base.id {
				incomplete = true
				return
			}
			raw, mediaID, ok := twitterPhotoInContainer(n, article, page, ref)
			if !ok {
				incomplete = true
				return
			}
			key := ref.id + ":" + strconv.Itoa(ref.photo)
			if prior, ok := found[key]; ok {
				if prior.mediaID != mediaID {
					delete(found, key)
					conflict[key] = true
					incomplete = true
				}
				return
			}
			if !conflict[key] {
				found[key] = candidate{ref: ref, raw: raw, mediaID: mediaID}
				order = append(order, key)
			}
		})
	})
	for _, key := range order {
		c, ok := found[key]
		if !ok {
			continue
		}
		primary, alternatives := twitterOriginalURLs(page.String(), c.raw)
		item := Item{URL: primary, AlternateURLs: alternatives, Filename: fmt.Sprintf("twitter-%s-%02d", c.ref.id, c.ref.photo), RequireImage: true, PublicOnly: true,
			Metadata: map[string]string{"twitter_status_id": c.ref.id, "twitter_author": author, "twitter_photo_index": strconv.Itoa(c.ref.photo)}}
		if referer := imageReferer(page.String(), primary); referer != "" {
			item.Headers = map[string]string{"Referer": referer}
		}
		result.Items = append(result.Items, item)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(result.Items) == 0 {
		return result, errors.New("X 作者主页未显示可确认的图片，可能需要登录或授权")
	}
	var warning error = errors.New("X 作者主页仅采集当前已加载的帖子；分页尚未验证，结果可能不完整")
	if incomplete {
		warning = errors.Join(warning, errors.New("部分帖子是引用或身份无法确认，已跳过"))
	}
	return result, warning
}

type twitterProfileSnapshot struct {
	URL       string   `json:"url"`
	Canonical []string `json:"canonical"`
	Articles  []string `json:"articles"`
	Login     bool     `json:"login"`
	Protected bool     `json:"protected"`
	Truncated bool     `json:"truncated"`
}

// Capture only laid-out visible articles and their visible, loaded images. This
// avoids treating CSS-hidden markup or React's streamed placeholders as posts.
const twitterProfileSnapshotScript = `(() => {
  function visible(node) {
    let p = node, depth = 0;
    for (; p && p.nodeType === 1 && depth < 128; p = p.parentElement, depth++) {
      const style = getComputedStyle(p);
      if (p.hidden || (p.getAttribute('aria-hidden') || '').toLowerCase() === 'true' || style.display === 'none' ||
          style.visibility === 'hidden' || style.visibility === 'collapse' || Number(style.opacity) === 0) return false;
    }
    if (p && p.nodeType === 1) return false;
    const rect = node.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0;
  }
  const loginInputs = [...document.querySelectorAll('input[type="password" i], input[autocomplete="username" i]')];
  const dialogs = [...document.querySelectorAll('[role="dialog"]')];
  const login = loginInputs.some(visible) || dialogs.some(node => {
    if (!visible(node)) return false;
    const text = (node.innerText || '').toLowerCase();
    return text.includes('log in to x') || text.includes('sign in to x') || text.includes('登录 x') || text.includes('登录到 x');
  });
  const protectedNodes = [...document.querySelectorAll('[data-testid="icon-lock"],svg[aria-label*="protected" i]')];
  const protectedAccount = protectedNodes.some(visible);
  const canonicalNodes = document.querySelectorAll('meta[property="og:url" i]');
  const canonical = [];
  let truncated = false;
  for (const node of canonicalNodes) {
    if (canonical.length >= 20) { truncated = true; break; }
    const value = node.getAttribute('content') || '';
    canonical.push(value.length <= 8192 ? value : '');
  }
  const articles = [];
  let bytes = 0, examined = 0;
  for (const article of document.querySelectorAll('article')) {
    if (++examined > 10000) { truncated = true; break; }
    if (!visible(article)) continue;
    if (articles.length >= 1000) { truncated = true; break; }
    const source = article.querySelectorAll('a,img,svg,article');
    if (source.length > 10000) { truncated = true; continue; }
    const clone = article.cloneNode(true), copied = clone.querySelectorAll('a,img,svg,article');
    for (let i = 0; i < source.length; i++) {
      const node = source[i];
      if (!visible(node) || (node.tagName === 'IMG' && (!node.complete || node.naturalWidth < 1 || node.naturalHeight < 1))) copied[i].remove();
    }
    const content = clone.outerHTML;
    bytes += JSON.stringify(content).length;
    if (bytes > 1048576) { truncated = true; break; }
    articles.push(content);
  }
  return {url:location.href.length <= 8192 ? location.href : '',canonical,articles,login,protected:protectedAccount,truncated};
})()`

func twitterProfileCapture(ctx context.Context, s *RenderedSession, start, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if !twitterSameProfile(start, page) {
		return result, errors.New("X 作者主页已跳转到其他账号")
	}
	var snapshot twitterProfileSnapshot
	if err := s.run(ctx, chromedp.Evaluate(twitterProfileSnapshotScript, &snapshot)); err != nil {
		return result, err
	}
	current, err := crawlInputURL(snapshot.URL)
	if err != nil || !twitterSameProfile(start, current) || !sameOrigin(page, current) {
		return result, errors.New("X 作者主页已发生变化，请重试")
	}
	if snapshot.Login {
		return result, errors.New("X 作者主页需要登录或授权后才能查看")
	}
	if snapshot.Protected {
		return result, errors.New("X 页面包含受保护账号或帖子，已停止采集")
	}
	var body strings.Builder
	body.WriteString("<html><head>")
	for _, canonical := range snapshot.Canonical {
		body.WriteString(`<meta property="og:url" content="`)
		body.WriteString(html.EscapeString(canonical))
		body.WriteString(`">`)
	}
	body.WriteString("</head><body>")
	for _, article := range snapshot.Articles {
		body.WriteString(article)
	}
	body.WriteString("</body></html>")
	doc, err := html.Parse(strings.NewReader(body.String()))
	if err != nil {
		return result, err
	}
	result, err = twitterProfileCollect(ctx, current, doc)
	if snapshot.Truncated {
		err = errors.Join(err, errors.New("X 作者主页达到可见页面的采集上限"))
	}
	return result, err
}

func discoverTwitterProfile(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if twitterProfile(page) == "" || getter == nil {
		return result, errors.New("X 作者主页地址或下载客户端无效")
	}
	requested := *page
	if requested.Scheme == "http" {
		requested.Scheme, requested.Host = "https", requested.Hostname()
	}
	guarded, err := twitterGuardedGetter(getter, func(req *http.Request, _ []*http.Request) bool {
		return twitterSameProfile(&requested, req.URL)
	})
	if err != nil {
		return result, err
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, guarded, crawlPage{url: requested.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, fmt.Errorf("读取 X 作者主页失败: %w", err)
	}
	if doc == nil || !twitterSameProfile(&requested, final) {
		return result, errors.New("X 作者主页已跳转，可能需要登录或授权")
	}
	return twitterProfileCollect(ctx, &requested, doc)
}

// twitterOriginalURLs follows the original-image rule in the reference script.
// The preview remains a last resort when the CDN does not have an original.
func twitterOriginalURLs(source, raw string) (string, []string) {
	page, err := crawlURL(nil, source)
	if err != nil || (!domainOrSubdomain(page.Hostname(), "twitter.com") && !domainOrSubdomain(page.Hostname(), "x.com")) {
		return raw, nil
	}
	u, err := crawlURL(nil, raw)
	if err != nil || u.User != nil || !strings.EqualFold(u.Hostname(), "pbs.twimg.com") ||
		(u.Port() != "" && !((u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443"))) {
		return raw, nil
	}
	match := twitterMediaPath.FindStringSubmatch(u.Path)
	if len(match) != 4 {
		return raw, nil
	}
	query := u.Query()
	for key, values := range query {
		if (key != "format" && key != "name") || len(values) != 1 {
			return raw, nil
		}
	}
	format := strings.ToLower(query.Get("format"))
	if format != "" && format != "jpg" && format != "jpeg" && format != "png" && format != "webp" {
		return raw, nil
	}
	ext := strings.ToLower(match[2])
	if format == "" && ext == "" {
		return raw, nil
	}
	if ext != "" && format == "" && query.Get("name") == "" {
		u.Path = "/media/" + match[1] + "." + ext + ":orig"
		u.RawPath = ""
	} else {
		u.Path = "/media/" + match[1]
		if ext != "" {
			u.Path += "." + ext
		}
		u.RawPath = ""
		query.Set("name", "orig")
		u.RawQuery = query.Encode()
	}
	primary := u.String()
	var alternatives []string
	if ext == "" {
		for _, candidate := range []string{"jpg", "png"} {
			if candidate == format || (candidate == "jpg" && format == "jpeg") {
				continue
			}
			other := *u
			values := other.Query()
			values.Set("format", candidate)
			other.RawQuery = values.Encode()
			alternatives = append(alternatives, other.String())
		}
	}
	if raw != primary {
		alternatives = append(alternatives, raw)
	}
	return primary, alternatives
}
