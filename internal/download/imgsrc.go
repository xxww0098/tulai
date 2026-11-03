package download

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

var (
	imgsrcOwner     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	imgsrcPhotoPath = regexp.MustCompile(`^([1-9][0-9]{0,19})\.html$`)
	imgsrcAlbumPath = regexp.MustCompile(`^a([1-9][0-9]{0,19})\.html$`)
	imgsrcTapePath  = regexp.MustCompile(`^tape-([1-9][0-9]{0,19})-([1-9][0-9]{0,19})-(0|[1-9][0-9]{0,6})\.html$`)
	imgsrcCDN       = regexp.MustCompile(`^b[0-9]{1,2}\.dd\.icdn\.ru$`)
	imgsrcImageFile = regexp.MustCompile(`^imgsrc\.ru_([1-9][0-9]{0,19})[A-Za-z]{1,12}\.jpg$`)
	imgsrcAnchor    = regexp.MustCompile(`^#p([1-9][0-9]{0,19})$`)
	imgsrcCount     = regexp.MustCompile(`(?i)([1-9][0-9]{0,5})\s+photo\b`)
)

const imgsrcImageLimit = 10000

type imgsrcRef struct {
	owner, kind, photo, album, first string
	offset                           int
}

func imgsrcHost(u *url.URL) bool {
	return u != nil && u.User == nil && (u.Scheme == "http" || u.Scheme == "https") &&
		(u.Port() == "" || (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80")) &&
		(strings.EqualFold(u.Hostname(), "imgsrc.ru") || strings.EqualFold(u.Hostname(), "www.imgsrc.ru"))
}
func imgsrcPageRef(u *url.URL) imgsrcRef {
	if !imgsrcHost(u) || path.Clean(u.Path) != u.Path || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return imgsrcRef{}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || !imgsrcOwner.MatchString(parts[0]) {
		return imgsrcRef{}
	}
	ref := imgsrcRef{owner: strings.ToLower(parts[0])}
	if m := imgsrcPhotoPath.FindStringSubmatch(parts[1]); len(m) == 2 {
		ref.kind = "photo"
		ref.photo = m[1]
		return ref
	}
	if m := imgsrcAlbumPath.FindStringSubmatch(parts[1]); len(m) == 2 {
		ref.kind = "album"
		ref.album = m[1]
		return ref
	}
	if m := imgsrcTapePath.FindStringSubmatch(parts[1]); len(m) == 4 {
		ref.kind = "tape"
		ref.album = m[1]
		ref.first = m[2]
		ref.offset, _ = strconv.Atoi(m[3])
		return ref
	}
	return imgsrcRef{}
}
func isImgsrcPage(u *url.URL) bool { return imgsrcPageRef(u).kind != "" }

// Recognized credentials supplied by the user or by a same-origin album link
// are carried only to the same site, never to the image host or in a Referer.
func imgsrcPublicQuery(u *url.URL) bool {
	if u == nil {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	for key, v := range q {
		if len(v) != 1 {
			return false
		}
		switch key {
		case "pwd", "token":
			if len(v[0]) > 512 || (v[0] != "" && u.Scheme != "https") {
				return false
			}
			for _, ch := range v[0] {
				if unicode.IsControl(ch) {
					return false
				}
			}
		case "lang":
			if v[0] != "en" && v[0] != "ru" {
				return false
			}
		case "per_page":
			if v[0] != "12" && v[0] != "24" && v[0] != "36" && v[0] != "48" {
				return false
			}
		case "fts":
			if v[0] != "0" && v[0] != "1" {
				return false
			}
		default:
			return false
		}
	}
	return true
}
func imgsrcCleanURL(u *url.URL) *url.URL {
	v := *u
	v.Fragment = ""
	return &v
}

func imgsrcHasCredentials(u *url.URL) bool {
	if u == nil {
		return false
	}
	q := u.Query()
	return q.Get("pwd") != "" || q.Get("token") != ""
}

func imgsrcFollowURL(page, target *url.URL) (*url.URL, bool) {
	if !sameOrigin(page, target) || !imgsrcPublicQuery(target) {
		return nil, false
	}
	u := imgsrcCleanURL(target)
	q, source := u.Query(), page.Query()
	for _, key := range []string{"pwd", "token"} {
		if q.Get(key) == "" && source.Get(key) != "" {
			q.Set(key, source.Get(key))
		} else if q.Get(key) == "" {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()
	u.ForceQuery = false
	return u, true
}

// The normal HTTP getter can be copied with a stricter per-request redirect
// policy. An arbitrary Getter cannot guarantee that query credentials stay on
// the original origin, so credentialed pagination requires this wrapper.
func imgsrcGuardGetter(getter Getter, origin *url.URL) (Getter, bool) {
	g, ok := getter.(*HTTPGetter)
	if !ok || g == nil || g.client == nil {
		return getter, false
	}
	copyGetter, copyClient := *g, *g.client
	check := copyClient.CheckRedirect
	copyClient.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if next.URL.User != nil || !sameOrigin(origin, next.URL) {
			return errors.New("ImgSrc 已阻止携带凭据的跨站跳转")
		}
		if check != nil {
			return check(next, via)
		}
		if len(via) >= 10 {
			return errors.New("ImgSrc 重定向次数过多")
		}
		return nil
	}
	copyGetter.client = &copyClient
	return &copyGetter, true
}

func imgsrcImage(raw, owner, id string) (string, bool) {
	if owner == "" || id == "" {
		return "", false
	}
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	if len(strings.Fields(raw)) != 1 {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !imgsrcCDN.MatchString(strings.ToLower(u.Hostname())) ||
		(u.Port() != "" && u.Port() != "443") || u.RawQuery != "" || u.Fragment != "" || path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 4 || !strings.EqualFold(parts[1], owner) || !strings.EqualFold(parts[0], owner[:1]) || parts[2] != id[len(id)-1:] {
		return "", false
	}
	m := imgsrcImageFile.FindStringSubmatch(parts[3])
	if len(m) != 2 || m[1] != id {
		return "", false
	}
	return u.String(), true
}

func imgsrcSourceID(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	m := imgsrcImageFile.FindStringSubmatch(path.Base(u.Path))
	if len(m) != 2 {
		return ""
	}
	return m[1]
}

func imgsrcBigPicture(n *html.Node) bool {
	if n.Parent == nil || n.Parent.Type != html.ElementNode || n.Parent.Data != "picture" {
		return false
	}
	for child := n.Parent.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == "img" {
			for _, class := range strings.Fields(attribute(child, "class")) {
				if class == "big" {
					return true
				}
			}
		}
	}
	return false
}

func imgsrcRestricted(ctx context.Context, doc *html.Node) bool {
	var restricted bool
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data != "input" || !strings.EqualFold(attribute(n, "type"), "password") {
			return
		}
		for p := n; p != nil; p = p.Parent {
			if p.Type != html.ElementNode {
				continue
			}
			for _, a := range p.Attr {
				if strings.EqualFold(a.Key, "hidden") {
					return
				}
			}
			style := strings.ToLower(strings.Join(strings.Fields(attribute(p, "style")), ""))
			if strings.EqualFold(attribute(p, "aria-hidden"), "true") || strings.Contains(style, "display:none") || strings.Contains(style, "visibility:hidden") {
				return
			}
		}
		restricted = true
	})
	return restricted
}
func imgsrcCanonical(ctx context.Context, page *url.URL, doc *html.Node, want imgsrcRef) bool {
	var found, mismatch bool
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode || n.Data != "link" || !strings.EqualFold(attribute(n, "rel"), "canonical") {
			return
		}
		raw := attribute(n, "href")
		u, err := url.Parse(raw)
		if raw == "" || err != nil || imgsrcPageRef(page.ResolveReference(u)) != want {
			mismatch = true
		} else {
			found = true
		}
	})
	return found && !mismatch
}

func imgsrcItem(page *url.URL, image, id string) Item {
	return Item{URL: image, RequireImage: true, PublicOnly: true, Headers: map[string]string{"Referer": page.Scheme + "://" + page.Host + "/"}, Metadata: map[string]string{"imgsrc_photo_id": id}}
}

type imgsrcParsed struct {
	result        Discovery
	tape          *url.URL
	next          []*url.URL
	invalid       bool
	expected      int
	expectedAlbum string
}

func imgsrcPhoto(ctx context.Context, doc *html.Node, page *url.URL) (imgsrcParsed, error) {
	x := imgsrcParsed{result: Discovery{IsPage: true}}
	ref := imgsrcPageRef(page)
	if doc == nil || ref.kind != "photo" {
		return x, errors.New("ImgSrc 照片页面无效")
	}
	if imgsrcRestricted(ctx, doc) {
		return x, errors.New("ImgSrc 相册需要密码或登录授权")
	}
	if !imgsrcCanonical(ctx, page, doc, ref) {
		return x, errors.New("ImgSrc 照片身份无法确认")
	}
	var image string
	var tapeRef imgsrcRef
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && x.result.Title == "" {
			x.result.Title = nodeText(n)
		}
		if n.Data == "source" && attribute(n, "id") == "big-pic-jpeg" && strings.EqualFold(attribute(n, "type"), "image/jpeg") {
			candidate, ok := imgsrcImage(attribute(n, "srcset"), ref.owner, ref.photo)
			if !ok || (image != "" && image != candidate) {
				x.invalid = true
			} else {
				image = candidate
			}
		}
		if n.Data != "a" {
			return
		}
		raw := attribute(n, "href")
		u, err := url.Parse(raw)
		if err != nil {
			return
		}
		u = page.ResolveReference(u)
		if sameOrigin(page, u) && u.Path == "/members/zip_manager.php" {
			q, parseErr := url.ParseQuery(u.RawQuery)
			if parseErr == nil && len(q["zip"]) == 1 {
				parts := strings.Split(q.Get("zip"), "/")
				count := imgsrcCount.FindStringSubmatch(nodeText(n))
				if len(parts) == 2 && strings.EqualFold(parts[0], ref.owner) && len(count) == 2 {
					album := strings.TrimSuffix(parts[1], ".zip")
					if imgsrcAlbumPath.MatchString("a"+album+".html") && parts[1] == album+".zip" {
						x.expectedAlbum = album
						x.expected, _ = strconv.Atoi(count[1])
					}
				}
			}
			return
		}
		r := imgsrcPageRef(u)
		if r.kind != "tape" || r.owner != ref.owner {
			return
		}
		u, ok := imgsrcFollowURL(page, u)
		if !ok {
			x.invalid = true
			return
		}
		r.offset = 0
		if tapeRef.kind != "" && tapeRef != r {
			x.invalid = true
			return
		}
		tapeRef = r
		x.tape = u
	})
	if err := ctx.Err(); err != nil {
		return x, err
	}
	if image == "" {
		return x, errors.New("ImgSrc 未提供公开的当前照片")
	}
	x.result.Items = append(x.result.Items, imgsrcItem(page, image, ref.photo))
	if x.invalid {
		return x, errors.New("ImgSrc 部分照片或相册链接无法验证")
	}
	return x, nil
}

func imgsrcTape(ctx context.Context, doc *html.Node, page *url.URL) (imgsrcParsed, error) {
	x := imgsrcParsed{result: Discovery{IsPage: true}}
	ref := imgsrcPageRef(page)
	if doc == nil || ref.kind != "tape" {
		return x, errors.New("ImgSrc 相册页面无效")
	}
	if imgsrcRestricted(ctx, doc) {
		return x, errors.New("ImgSrc 相册需要密码或登录授权")
	}
	if !imgsrcCanonical(ctx, page, doc, ref) {
		return x, errors.New("ImgSrc 相册分页身份无法确认")
	}
	seen := map[string]bool{}
	next := map[string]*url.URL{}
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && x.result.Title == "" {
			x.result.Title = nodeText(n)
		}
		if n.Data == "source" && strings.EqualFold(attribute(n, "type"), "image/jpeg") && imgsrcBigPicture(n) {
			var anchor string
			for p := n.Parent; p != nil; p = p.Parent {
				if p.Type == html.ElementNode && p.Data == "a" {
					anchor = attribute(p, "href")
					break
				}
			}
			id := ""
			if m := imgsrcAnchor.FindStringSubmatch(anchor); len(m) == 2 {
				id = m[1]
			} else if u, err := url.Parse(anchor); err == nil && anchor != "" {
				u = page.ResolveReference(u)
				r := imgsrcPageRef(u)
				nextPage := r.kind == "tape" && r.owner == ref.owner && r.album == ref.album && r.first == ref.first && r.offset > ref.offset
				ownerPage := strings.EqualFold(u.Path, "/"+ref.owner) && u.RawQuery == "" && u.Fragment == ""
				if sameOrigin(page, u) && imgsrcPublicQuery(u) && (nextPage || ownerPage) {
					id = imgsrcSourceID(attribute(n, "srcset"))
				}
			}
			if id == "" {
				return
			}
			image, ok := imgsrcImage(attribute(n, "srcset"), ref.owner, id)
			if !ok {
				x.invalid = true
				return
			}
			if seen[id] {
				return
			}
			seen[id] = true
			x.result.Items = append(x.result.Items, imgsrcItem(page, image, id))
			return
		}
		if n.Data != "a" {
			return
		}
		u, err := url.Parse(attribute(n, "href"))
		if err != nil {
			return
		}
		u = page.ResolveReference(u)
		r := imgsrcPageRef(u)
		if r.kind != "tape" || r.owner != ref.owner || r.album != ref.album || r.first != ref.first {
			return
		}
		u, ok := imgsrcFollowURL(page, u)
		if !ok {
			x.invalid = true
			return
		}
		if r.offset != ref.offset {
			next[u.String()] = u
		}
	})
	if err := ctx.Err(); err != nil {
		return x, err
	}
	for _, u := range next {
		x.next = append(x.next, u)
	}
	sort.Slice(x.next, func(i, j int) bool { return imgsrcPageRef(x.next[i]).offset < imgsrcPageRef(x.next[j]).offset })
	if len(x.result.Items) == 0 {
		return x, errors.New("ImgSrc 相册未返回可验证的公开图片")
	}
	if x.invalid {
		return x, errors.New("ImgSrc 相册中有无法验证的图片或分页")
	}
	return x, nil
}

func imgsrcFetchError(ctx context.Context, err error) error {
	if canceled := ctx.Err(); canceled != nil {
		return canceled
	}
	var status *HTTPStatusError
	if errors.As(err, &status) {
		return errors.New("ImgSrc 页面读取失败，HTTP " + strconv.Itoa(status.StatusCode))
	}
	return errors.New("ImgSrc 页面读取失败，请检查网络或授权状态")
}

func discoverImgsrc(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	want := imgsrcPageRef(root)
	if getter == nil || want.kind == "" {
		return result, errors.New("ImgSrc 地址或下载客户端无效")
	}
	if !imgsrcPublicQuery(root) {
		return result, errors.New("ImgSrc 无法安全使用此网址中的访问参数")
	}
	request := imgsrcCleanURL(root)
	guarded := false
	if imgsrcHasCredentials(request) {
		getter, guarded = imgsrcGuardGetter(getter, request)
		if !guarded {
			return result, errors.New("当前下载客户端无法安全隔离 ImgSrc 访问凭据")
		}
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: request.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, imgsrcFetchError(ctx, err)
	}
	if doc == nil || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("ImgSrc 页面不可用")
	}
	actual := imgsrcPageRef(final)
	if actual.owner != want.owner || !imgsrcPublicQuery(final) {
		return result, errors.New("ImgSrc 页面发生跳转或需要访问授权")
	}
	if imgsrcHasCredentials(request) {
		var ok bool
		final, ok = imgsrcFollowURL(request, final)
		if !ok {
			return result, errors.New("ImgSrc 已阻止跨站传递访问凭据")
		}
	}
	if want.kind == "photo" && actual != want {
		return result, errors.New("ImgSrc 照片已跳转")
	}
	if want.kind == "tape" && actual != want {
		return result, errors.New("ImgSrc 相册分页已跳转")
	}
	if want.kind == "album" && actual.kind != "photo" {
		return result, errors.New("ImgSrc 相册尚未提供公开照片")
	}
	var parsed imgsrcParsed
	if actual.kind == "photo" {
		parsed, err = imgsrcPhoto(ctx, doc, final)
	} else {
		parsed, err = imgsrcTape(ctx, doc, final)
	}
	if want.kind == "album" && (parsed.tape == nil || imgsrcPageRef(parsed.tape).album != want.album) {
		return result, errors.New("ImgSrc 相册编号与照片不匹配")
	}
	if err != nil {
		return parsed.result, err
	}
	result = parsed.result
	expected := 0
	if parsed.tape != nil && parsed.expectedAlbum == imgsrcPageRef(parsed.tape).album {
		expected = parsed.expected
	}
	queue := parsed.next
	if parsed.tape != nil {
		queue = append([]*url.URL{parsed.tape}, queue...)
	}
	if len(queue) == 0 {
		if actual.kind == "photo" {
			return result, errors.New("ImgSrc 仅采集当前照片，相册分页尚未确认")
		}
		return result, nil
	}
	seenPages := map[string]bool{imgsrcCleanURL(final).String(): true}
	seenPhotos := map[string]bool{}
	for _, it := range result.Items {
		seenPhotos[it.Metadata["imgsrc_photo_id"]] = true
	}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		u := queue[0]
		queue = queue[1:]
		key := u.String()
		if seenPages[key] {
			continue
		}
		if len(seenPages) >= maxCrawlPages {
			return result, errors.New("ImgSrc 相册分页达到采集上限")
		}
		seenPages[key] = true
		if imgsrcHasCredentials(u) && !guarded {
			getter, guarded = imgsrcGuardGetter(getter, u)
			if !guarded {
				return result, errors.New("当前下载客户端无法安全隔离 ImgSrc 相册访问凭据")
			}
		}
		doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: key, depth: 2})
		if response != nil {
			response.Body.Close()
		}
		if err != nil {
			return result, imgsrcFetchError(ctx, err)
		}
		if doc == nil || imgsrcPageRef(final) != imgsrcPageRef(u) || (u.Scheme == "https" && final.Scheme != "https") || !imgsrcPublicQuery(final) {
			return result, errors.New("ImgSrc 相册分页发生跳转或需要访问授权")
		}
		parsed, err := imgsrcTape(ctx, doc, final)
		for _, item := range parsed.result.Items {
			id := item.Metadata["imgsrc_photo_id"]
			if seenPhotos[id] {
				continue
			}
			if len(result.Items) >= imgsrcImageLimit {
				return result, errors.New("ImgSrc 相册图片达到采集上限")
			}
			seenPhotos[id] = true
			result.Items = append(result.Items, item)
		}
		if err != nil {
			return result, err
		}
		queue = append(queue, parsed.next...)
	}
	if expected > 0 && len(result.Items) != expected {
		return result, errors.New("ImgSrc 返回的图片数量与相册标注不一致，结果可能不完整")
	}
	return result, nil
}

// Capture reads only the page currently displayed, so it does not send browser
// credentials or query inaccessible album pages behind the user's back.
func imgsrcRenderedPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !imgsrcPublicQuery(page) {
		return result, errors.New("ImgSrc 无法安全使用此网址中的访问参数")
	}
	ref := imgsrcPageRef(page)
	var parsed imgsrcParsed
	var err error
	switch ref.kind {
	case "photo":
		parsed, err = imgsrcPhoto(ctx, doc, page)
	case "tape":
		parsed, err = imgsrcTape(ctx, doc, page)
	default:
		return result, errors.New("ImgSrc 相册页面尚未加载")
	}
	if err != nil {
		return parsed.result, err
	}
	return parsed.result, errors.New("ImgSrc 仅采集当前页面图片，其他相册页面尚未读取")
}
