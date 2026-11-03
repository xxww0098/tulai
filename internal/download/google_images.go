package download

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/chromedp/chromedp"
	"golang.org/x/net/html"
)

// Search domains listed by Google at https://www.google.com/supported_domains.
var googleImagesDomains = func() map[string]bool {
	all := strings.Fields(`google.com google.ad google.ae google.com.af google.com.ag google.al google.am google.co.ao google.com.ar google.as google.at google.com.au google.az google.ba google.com.bd google.be google.bf google.bg google.com.bh google.bi google.bj google.com.bn google.com.bo google.com.br google.bs google.bt google.co.bw google.by google.com.bz google.ca google.cd google.cf google.cg google.ch google.ci google.co.ck google.cl google.cm google.cn google.com.co google.co.cr google.com.cu google.cv google.com.cy google.cz google.de google.dj google.dk google.dm google.com.do google.dz google.com.ec google.ee google.com.eg google.es google.com.et google.fi google.com.fj google.fm google.fr google.ga google.ge google.gg google.com.gh google.com.gi google.gl google.gm google.gr google.com.gt google.gy google.com.hk google.hn google.hr google.ht google.hu google.co.id google.ie google.co.il google.im google.co.in google.iq google.is google.it google.je google.com.jm google.jo google.co.jp google.co.ke google.com.kh google.ki google.kg google.co.kr google.com.kw google.kz google.la google.com.lb google.li google.lk google.co.ls google.lt google.lu google.lv google.com.ly google.co.ma google.md google.me google.mg google.mk google.ml google.com.mm google.mn google.com.mt google.mu google.mv google.mw google.com.mx google.com.my google.co.mz google.com.na google.com.ng google.com.ni google.ne google.nl google.no google.com.np google.nr google.nu google.co.nz google.com.om google.com.pa google.com.pe google.com.pg google.com.ph google.com.pk google.pl google.pn google.com.pr google.ps google.pt google.com.py google.com.qa google.ro google.ru google.rw google.com.sa google.com.sb google.sc google.se google.com.sg google.sh google.si google.sk google.com.sl google.sn google.so google.sm google.sr google.st google.com.sv google.td google.tg google.co.th google.com.tj google.tl google.tm google.tn google.to google.com.tr google.tt google.com.tw google.co.tz google.com.ua google.co.ug google.co.uk google.com.uy google.co.uz google.com.vc google.co.ve google.co.vi google.com.vn google.vu google.ws google.rs google.co.za google.co.zm google.co.zw google.cat`)
	domains := make(map[string]bool, len(all))
	for _, name := range all {
		domains[name] = true
	}
	return domains
}()

var googleImagesResultID = regexp.MustCompile(`^[A-Za-z0-9_:-]{1,128}$`)

const maxGoogleImages = 1000

func googleImagesDomain(u *url.URL) string {
	if u == nil || len(u.String()) > 8192 || u.User != nil || u.RawPath != "" || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "http" && u.Port() == "80") && !(u.Scheme == "https" && u.Port() == "443")) {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if strings.HasPrefix(host, "www.") {
		host = strings.TrimPrefix(host, "www.")
	} else if strings.HasPrefix(host, "images.") {
		host = strings.TrimPrefix(host, "images.")
	}
	if !googleImagesDomains[host] {
		return ""
	}
	return host
}

func isGoogleImageSearch(u *url.URL) bool {
	if googleImagesDomain(u) == "" || u.Path != "/search" {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q["tbm"]) > 1 || len(q["udm"]) > 1 {
		return false
	}
	return (q.Get("tbm") == "isch" && (q.Get("udm") == "" || q.Get("udm") == "2")) ||
		(q.Get("udm") == "2" && (q.Get("tbm") == "" || q.Get("tbm") == "isch"))
}

func googleImagesQuery(u *url.URL) string {
	if !isGoogleImageSearch(u) {
		return ""
	}
	q := u.Query()["q"]
	if len(q) != 1 || len(q[0]) > 1024 || strings.TrimSpace(q[0]) == "" || strings.ContainsAny(q[0], "\x00\r\n") {
		return ""
	}
	return q[0]
}

// Google may rename the image-search mode or normalize its own host, but the
// search term and user-selected filters must remain bound to the original page.
func googleImagesSameSearch(start, final *url.URL) bool {
	term := googleImagesQuery(start)
	if term == "" || term != googleImagesQuery(final) || googleImagesDomain(start) != googleImagesDomain(final) || final.Scheme != "https" {
		return false
	}
	a, b := start.Query(), final.Query()
	for _, key := range []string{"tbs", "safe", "as_q", "as_epq", "as_oq", "as_eq", "as_sitesearch", "as_filetype", "as_rights", "as_nlo", "as_nhi", "as_dt", "as_occt", "as_st", "as_ylo", "as_yhi", "cr", "gl", "lr", "hl", "nfpr", "filter", "imgtype", "imgsz", "imgc", "imgcolor", "imgar", "authuser"} {
		if !slices.Equal(a[key], b[key]) {
			return false
		}
	}
	return true
}

// Request the supplied search URL once; check redirects before each request
// and never manufacture an endpoint for verification or pagination.
func discoverGoogleImages(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if googleImagesQuery(root) == "" {
		return result, errors.New("Google 图片搜索地址或查询条件无效")
	}
	client, ok := getter.(*HTTPGetter)
	if !ok || client == nil || client.client == nil {
		return result, errors.New("Google 图片搜索需要支持安全重定向的下载客户端")
	}
	requested := *root
	if requested.Scheme == "http" {
		requested.Scheme, requested.Host = "https", requested.Hostname()
	}
	clone, guarded := *client, *client.client
	previous := guarded.CheckRedirect
	guarded.Jar = nil
	guarded.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 6 || !googleImagesSameSearch(&requested, next.URL) {
			return errors.New("Google 图片搜索已跳转到其他站点或查询，可能需要验证")
		}
		if previous != nil {
			if err := previous(next, via); err != nil {
				return err
			}
		}
		if !googleImagesSameSearch(&requested, next.URL) {
			return errors.New("Google 图片搜索已跳转到其他站点或查询，可能需要验证")
		}
		if len(via) > 0 && !sameOrigin(via[0].URL, next.URL) {
			next.Header.Del("Cookie")
			next.Header.Del("Authorization")
			if ref, err := url.Parse(next.Header.Get("Referer")); err == nil && ref.Host != "" {
				next.Header.Set("Referer", ref.Scheme+"://"+ref.Host+"/")
			}
		}
		return nil
	}
	clone.client = &guarded
	doc, final, _, response, err := fetchCrawlPage(ctx, &clone, crawlPage{url: requested.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		var status *HTTPStatusError
		if errors.As(err, &status) {
			switch status.StatusCode {
			case http.StatusForbidden:
				return result, fmt.Errorf("Google 图片搜索拒绝访问，可能需要在浏览器完成验证: %w", err)
			case http.StatusTooManyRequests:
				return result, fmt.Errorf("Google 图片搜索已限流，请稍后重试: %w", err)
			}
		}
		return result, fmt.Errorf("读取 Google 图片搜索失败: %w", err)
	}
	if doc == nil || !googleImagesSameSearch(&requested, final) {
		return result, errors.New("Google 图片搜索已跳转到其他站点或查询，可能需要验证")
	}
	snapshot, err := googleImagesStaticSnapshot(ctx, final, doc)
	if err != nil {
		return result, err
	}
	return googleImagesCollect(ctx, final, snapshot)
}

type googleImagesRow struct {
	Image  string `json:"image"`
	Source string `json:"source"`
	ID     string `json:"id"`
	DocID  string `json:"docid"`
	Title  string `json:"title"`
	Width  string `json:"width"`
	Height string `json:"height"`
}

type googleImagesSnapshot struct {
	URL       string            `json:"url"`
	Rows      []googleImagesRow `json:"rows"`
	Blocked   bool              `json:"blocked"`
	Truncated bool              `json:"truncated"`
	Missing   int               `json:"missing"`
}

func googleImagesResultNode(n *html.Node) bool {
	if n == nil || n.Type != html.ElementNode || n.Data != "div" {
		return false
	}
	var isGrid, isResult bool
	for _, class := range strings.Fields(attribute(n, "class")) {
		isGrid = isGrid || class == "islrtb"
		isResult = isResult || class == "isv-r"
	}
	return isGrid && isResult
}

func googleImagesStaticActive(n *html.Node) bool {
	for depth, p := 0, n; p != nil; depth, p = depth+1, p.Parent {
		if depth >= 128 {
			return false
		}
		if p.Type == html.ElementNode && (p.Data == "script" || p.Data == "style" || p.Data == "template" || p.Data == "noscript") {
			return false
		}
	}
	return true
}

// Do not treat hidden markup, scripts or templates as search results.
func googleImagesStaticVisible(n *html.Node) bool {
	if !googleImagesStaticActive(n) {
		return false
	}
	for depth, p := 0, n; p != nil; depth, p = depth+1, p.Parent {
		if depth >= 128 {
			return false
		}
		if p.Type != html.ElementNode {
			continue
		}
		for _, attr := range p.Attr {
			key := strings.ToLower(attr.Key)
			value := strings.ToLower(strings.TrimSpace(attr.Val))
			if key == "hidden" || (key == "aria-hidden" && value == "true") {
				return false
			}
			if p.Data == "img" && (key == "width" || key == "height") && googleImagesCSSZero(value) {
				return false
			}
			if key == "style" {
				for _, rule := range strings.Split(value, ";") {
					k, v, ok := strings.Cut(rule, ":")
					k = strings.TrimSpace(k)
					v = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "!important"))
					if ok && ((k == "display" && v == "none") || (k == "visibility" && (v == "hidden" || v == "collapse")) ||
						(k == "opacity" && googleImagesCSSZero(v)) || ((k == "width" || k == "height") && googleImagesCSSZero(strings.TrimSuffix(v, "px")))) {
						return false
					}
				}
			}
		}
	}
	return true
}

func googleImagesCSSZero(value string) bool {
	value = strings.TrimSuffix(strings.TrimSpace(value), "%")
	number, err := strconv.ParseFloat(value, 64)
	return err == nil && number == 0
}

func googleImagesStaticThumbnail(n *html.Node) bool {
	if n.Data != "img" || !googleImagesStaticVisible(n) {
		return false
	}
	raw := attribute(n, "src")
	if len(raw) > 8192 {
		return false
	}
	_, err := publicImageURL(raw)
	return err == nil
}

func googleImagesStaticSnapshot(ctx context.Context, page *url.URL, doc *html.Node) (googleImagesSnapshot, error) {
	snapshot := googleImagesSnapshot{URL: page.String()}
	var cards []*html.Node
	sawQuery, queryMatches := false, false
	selected, thumbnails := map[*html.Node]bool{}, map[*html.Node]bool{}
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil {
			return
		}
		if n.Type == html.TextNode && n.Parent != nil && n.Parent.Data == "noscript" && strings.Contains(n.Data, "/httpservice/retry/enablejs") {
			snapshot.Blocked = true
		}
		if n.Type != html.ElementNode {
			return
		}
		if (n.Data == "input" || n.Data == "textarea") && attribute(n, "name") == "q" && googleImagesStaticActive(n) {
			for depth, p := 0, n.Parent; p != nil && depth < 128; depth, p = depth+1, p.Parent {
				if p.Type != html.ElementNode || p.Data != "form" {
					continue
				}
				action := attribute(p, "action")
				if action == "" {
					action = "/search"
				}
				u, err := crawlURL(page, action)
				if err == nil && googleImagesDomain(u) == googleImagesDomain(page) && u.Path == "/search" {
					value := attribute(n, "value")
					if n.Data == "textarea" {
						value = nodeText(n)
					}
					if value != "" {
						sawQuery = true
						queryMatches = queryMatches || value == googleImagesQuery(page)
					}
				}
				break
			}
		}
		// Google uses this endpoint for the JavaScript-required response, even
		// when it also contains a logo or stale markup.
		if (n.Data == "a" && strings.Contains(attribute(n, "href"), "/httpservice/retry/enablejs")) ||
			(n.Data == "meta" && strings.Contains(attribute(n, "content"), "/httpservice/retry/enablejs")) {
			snapshot.Blocked = true
		}
		switch n.Data {
		case "div":
			for _, class := range strings.Fields(attribute(n, "class")) {
				if class == "g-recaptcha" && googleImagesStaticActive(n) {
					snapshot.Blocked = true
				}
			}
		case "form":
			if googleImagesStaticActive(n) {
				snapshot.Blocked = snapshot.Blocked || strings.Contains(strings.ToLower(attribute(n, "action")), "/sorry")
			}
		case "input", "textarea":
			if googleImagesStaticActive(n) {
				snapshot.Blocked = snapshot.Blocked || strings.Contains(strings.ToLower(attribute(n, "name")), "captcha")
			}
		case "iframe":
			if googleImagesStaticActive(n) {
				snapshot.Blocked = snapshot.Blocked || strings.Contains(strings.ToLower(attribute(n, "src")), "/recaptcha/")
			}
		}
		if googleImagesResultNode(n) && googleImagesStaticVisible(n) {
			if len(cards) >= maxGoogleImages {
				snapshot.Truncated = true
			} else {
				cards = append(cards, n)
				selected[n] = true
			}
		}
		if n.Data == "img" && googleImagesStaticThumbnail(n) {
			for depth, p := 0, n.Parent; p != nil && depth < 128; depth, p = depth+1, p.Parent {
				if p.Data == "script" || p.Data == "style" || p.Data == "template" || p.Data == "noscript" {
					break
				}
				if googleImagesResultNode(p) {
					if selected[p] {
						thumbnails[p] = true
					}
					break
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return snapshot, err
	}
	if snapshot.Blocked {
		return snapshot, errors.New("Google 图片搜索要求启用 JavaScript 或完成验证，请在下载浏览器中重试")
	}
	if sawQuery && !queryMatches {
		return snapshot, errors.New("Google 图片搜索页面内容与请求的查询不一致")
	}
	for _, n := range cards {
		if !thumbnails[n] {
			snapshot.Missing++
			continue
		}
		snapshot.Rows = append(snapshot.Rows, googleImagesRow{Image: attribute(n, "data-ou"), Source: attribute(n, "data-ru"), ID: attribute(n, "data-tbnid"),
			DocID: attribute(n, "data-docid"), Title: attribute(n, "data-pt"), Width: attribute(n, "data-ow"), Height: attribute(n, "data-oh")})
	}
	return snapshot, nil
}

// shortcut: recognize the evidenced historic DOM and current visible results;
// add a verified current result format and pagination when available.
const googleImagesSnapshotScript = `(() => {
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
  const blocked = [...document.querySelectorAll('form[action*="/sorry"],input[name*="captcha" i],iframe[src*="/recaptcha/" i]')].some(visible);
  const nodes = document.querySelectorAll('div.islrtb.isv-r');
  const rows = [];
  let truncated = false, bytes = 0, examined = 0;
  for (const node of nodes) {
    if (++examined > 10000) { truncated = true; break; }
    if (!visible(node)) continue;
    const thumbnail = node.querySelector('img');
    if (!thumbnail || !visible(thumbnail) || !thumbnail.complete || thumbnail.naturalWidth < 1 || thumbnail.naturalHeight < 1) continue;
    if (rows.length >= 1000) { truncated = true; break; }
	    const attr = (name, limit) => { const value = node.getAttribute(name) || ''; return value.length <= limit ? value : ''; };
	    const row = {image:attr('data-ou',8192),source:attr('data-ru',8192),id:attr('data-tbnid',128),docid:attr('data-docid',128),
	      title:attr('data-pt',512),width:attr('data-ow',12),height:attr('data-oh',12)};
	    bytes += JSON.stringify(row).length;
	    if (bytes > 524288) { truncated = true; break; }
	    rows.push(row);
  }
	  return {url:location.href.length <= 8192 ? location.href : '',rows,blocked,truncated};
})()`

func googleImagesCollect(ctx context.Context, page *url.URL, snapshot googleImagesSnapshot) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	query := googleImagesQuery(page)
	if query == "" {
		return result, errors.New("Google 图片搜索地址或查询条件无效")
	}
	result.Title = query + " - Google 图片"
	if snapshot.Blocked {
		return result, errors.New("Google 图片搜索需要完成页面验证")
	}
	type candidate struct {
		row           googleImagesRow
		image, source string
	}
	rows := make(map[string]candidate)
	order := make([]string, 0, len(snapshot.Rows))
	conflict := map[string]bool{}
	missing := snapshot.Missing
	var interrupted error
	for i, row := range snapshot.Rows {
		if err := ctx.Err(); err != nil {
			interrupted = err
			break
		}
		if i >= maxGoogleImages {
			snapshot.Truncated = true
			break
		}
		if len(row.Image) > 8192 || len(row.Source) > 8192 || !googleImagesResultID.MatchString(row.ID) || !googleImagesResultID.MatchString(row.DocID) {
			missing++
			continue
		}
		image, imageErr := publicImageURL(row.Image)
		source, sourceErr := publicImageURL(row.Source)
		if imageErr != nil || sourceErr != nil {
			missing++
			continue
		}
		if strings.HasPrefix(strings.ToLower(image.Hostname()), "encrypted-tbn") && strings.HasSuffix(strings.ToLower(image.Hostname()), ".gstatic.com") {
			missing++
			continue
		}
		if prior, ok := rows[row.ID]; ok {
			if prior.image != image.String() || prior.row.DocID != row.DocID {
				delete(rows, row.ID)
				conflict[row.ID] = true
				missing++
			}
			continue
		}
		if !conflict[row.ID] {
			rows[row.ID] = candidate{row: row, image: image.String(), source: source.String()}
			order = append(order, row.ID)
		}
	}
	seen := map[string]bool{}
	for _, id := range order {
		c, ok := rows[id]
		if !ok || seen[c.image] {
			continue
		}
		seen[c.image] = true
		item := Item{URL: c.image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{
			"google_query": query, "google_result_id": id, "google_document_id": c.row.DocID, "google_source": c.source,
		}}
		if len(c.row.Title) <= 512 && c.row.Title != "" {
			item.Metadata["google_title"] = c.row.Title
		}
		for key, value := range map[string]string{"google_width": c.row.Width, "google_height": c.row.Height} {
			if n, err := strconv.Atoi(value); err == nil && n > 0 && n <= 1000000 {
				item.Metadata[key] = strconv.Itoa(n)
			}
		}
		origin := &url.URL{Scheme: page.Scheme, Host: page.Host, Path: "/"}
		if ref := imageReferer(origin.String(), item.URL); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	if len(result.Items) == 0 {
		if interrupted != nil {
			return result, interrupted
		}
		return result, errors.New("Google 图片搜索尚未显示可识别的结果，可能需要完成验证或使用了尚不支持的页面结构")
	}
	var warning error = errors.New("Google 图片仅采集当前页可确认的结果；分页尚未验证，结果可能不完整")
	if missing > 0 {
		warning = errors.Join(warning, fmt.Errorf("%d 条结果缺少可确认的图片、缩略图、来源或身份信息", missing))
	}
	if snapshot.Truncated {
		warning = errors.Join(warning, fmt.Errorf("Google 图片当前页面达到采集数量或数据量上限（最多 %d 条）", maxGoogleImages))
	}
	return result, errors.Join(warning, interrupted)
}

func googleImagesCapture(ctx context.Context, s *RenderedSession, start, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if !googleImagesSameSearch(start, page) {
		return result, errors.New("Google 图片搜索已跳转或查询条件发生变化")
	}
	var snapshot googleImagesSnapshot
	if err := s.run(ctx, chromedp.Evaluate(googleImagesSnapshotScript, &snapshot)); err != nil {
		return result, err
	}
	current, err := crawlInputURL(snapshot.URL)
	if err != nil || !googleImagesSameSearch(start, current) || !sameOrigin(page, current) {
		return result, errors.New("Google 图片搜索页面已发生变化，请重试")
	}
	return googleImagesCollect(ctx, current, snapshot)
}
