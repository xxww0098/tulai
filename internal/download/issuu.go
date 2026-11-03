package download

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/chromedp/chromedp"
	"golang.org/x/net/html"
)

var (
	issuuName        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`)
	issuuRevision    = regexp.MustCompile(`^[0-9]{12}$`)
	issuuPublication = regexp.MustCompile(`^[a-f0-9]{32}$`)
	issuuSVGPath     = regexp.MustCompile(`^/([0-9]{12})-([a-f0-9]{32})/page_([1-9][0-9]{0,4})\.svg$`)
	issuuFlightPush  = regexp.MustCompile(`\bself\s*\.\s*__next_f\s*\.\s*push\s*\(`)
)

type issuuRef struct{ publisher, document string }

func issuuHost(u *url.URL) bool {
	return u != nil && u.User == nil && (u.Scheme == "http" || u.Scheme == "https") &&
		(u.Port() == "" || (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80")) &&
		(strings.EqualFold(u.Hostname(), "issuu.com") || strings.EqualFold(u.Hostname(), "www.issuu.com"))
}

func issuuDocumentRef(u *url.URL) issuuRef {
	if !issuuHost(u) || path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return issuuRef{}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if (len(parts) != 3 && len(parts) != 4) || !issuuName.MatchString(parts[0]) || parts[1] != "docs" || !issuuName.MatchString(parts[2]) {
		return issuuRef{}
	}
	if len(parts) == 4 {
		page, err := strconv.Atoi(parts[3])
		if err != nil || page < 1 || page > 10000 || strconv.Itoa(page) != parts[3] {
			return issuuRef{}
		}
	}
	return issuuRef{publisher: strings.ToLower(parts[0]), document: strings.ToLower(parts[2])}
}

func isIssuuDocument(u *url.URL) bool { return issuuDocumentRef(u).publisher != "" }

type issuuDocument struct {
	Access string `json:"access"`
	Path   struct {
		Type         string `json:"type"`
		Username     string `json:"username"`
		DocumentName string `json:"documentName"`
	} `json:"path"`
	FullPageImageDimensions []struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"fullPageImageDimensions"`
	PageCount     int    `json:"pageCount"`
	PublicationID string `json:"publicationId"`
	RevisionID    string `json:"revisionId"`
	Title         string `json:"title"`
	Gated         *bool  `json:"isDocumentGated"`
}

// The current page stores initialDocumentData in a JSON-valued Next.js Flight
// chunk. Parse both JSON layers as data; the page's JavaScript is never run.
func issuuInitialDocuments(v any, out *[]issuuDocument, depth int) {
	if depth > 48 || len(*out) >= 8 {
		return
	}
	switch x := v.(type) {
	case []any:
		for _, child := range x {
			issuuInitialDocuments(child, out, depth+1)
		}
	case map[string]any:
		if init, ok := x["initialDocumentData"].(map[string]any); ok {
			if raw, ok := init["document"]; ok {
				if b, err := json.Marshal(raw); err == nil {
					var d issuuDocument
					if json.Unmarshal(b, &d) == nil {
						*out = append(*out, d)
					}
				}
			}
			return
		}
		for _, child := range x {
			issuuInitialDocuments(child, out, depth+1)
		}
	}
}

func issuuState(ctx context.Context, page *url.URL, doc *html.Node) (issuuDocument, error) {
	var state issuuDocument
	ref := issuuDocumentRef(page)
	if err := ctx.Err(); err != nil {
		return state, err
	}
	if ref.publisher == "" || doc == nil {
		return state, errors.New("Issuu 文档页面无效")
	}
	var canonical, mismatch bool
	var scripts []*html.Node
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "script" {
			scripts = append(scripts, n)
		}
		if n.Data == "link" && strings.EqualFold(attribute(n, "rel"), "canonical") {
			u, err := url.Parse(attribute(n, "href"))
			if err != nil || issuuDocumentRef(page.ResolveReference(u)) != ref {
				mismatch = true
			} else {
				canonical = true
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return state, err
	}
	if !canonical || mismatch {
		return state, errors.New("Issuu 文档身份无法确认")
	}
	var documents []issuuDocument
	for _, n := range scripts {
		if err := ctx.Err(); err != nil {
			return state, err
		}
		var raw strings.Builder
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.TextNode {
				raw.WriteString(child.Data)
			}
		}
		text := raw.String()
		for _, m := range offer1688CodeMatches(text, issuuFlightPush) {
			var values []json.RawMessage
			if err := json.NewDecoder(strings.NewReader(text[m[1]:])).Decode(&values); err != nil || len(values) != 2 {
				continue
			}
			var flight string
			if json.Unmarshal(values[1], &flight) != nil {
				continue
			}
			index := strings.IndexByte(flight, ':')
			if index < 1 || index > 20 {
				continue
			}
			var value any
			if json.Unmarshal([]byte(flight[index+1:]), &value) == nil {
				issuuInitialDocuments(value, &documents, 0)
			}
		}
	}
	if len(documents) != 1 {
		return state, errors.New("Issuu 文档数据尚未加载或无法确认")
	}
	state = documents[0]
	if state.Path.Type != "user" || !strings.EqualFold(state.Path.Username, ref.publisher) || !strings.EqualFold(state.Path.DocumentName, ref.document) {
		return issuuDocument{}, errors.New("Issuu 文档数据与页面地址不匹配")
	}
	if state.Access != "PUBLIC" || state.Gated == nil || *state.Gated {
		return issuuDocument{}, errors.New("Issuu 文档需要登录、付费或其他访问授权")
	}
	if !issuuPublication.MatchString(state.PublicationID) || !issuuRevision.MatchString(state.RevisionID) || state.PageCount < 1 || state.PageCount > 10000 || len(state.FullPageImageDimensions) != state.PageCount {
		return issuuDocument{}, errors.New("Issuu 文档页数或公开标识无法确认")
	}
	for _, d := range state.FullPageImageDimensions {
		if d.Width < 1 || d.Height < 1 || d.Width > 100000 || d.Height > 100000 {
			return issuuDocument{}, errors.New("Issuu 页面尺寸数据无效")
		}
	}
	return state, nil
}

func issuuSVG(raw string, state issuuDocument) (string, int, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "svg.issuu.com") ||
		(u.Port() != "" && u.Port() != "443") || u.RawQuery != "" || u.Fragment != "" || path.Clean(u.Path) != u.Path {
		return "", 0, false
	}
	m := issuuSVGPath.FindStringSubmatch(u.Path)
	if len(m) != 4 || m[1] != state.RevisionID || m[2] != state.PublicationID {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[3])
	if err != nil || n < 1 || n > state.PageCount {
		return "", 0, false
	}
	return u.String(), n, true
}

func issuuItems(ctx context.Context, page *url.URL, state issuuDocument, urls []string) (Discovery, error) {
	result := Discovery{IsPage: true, Title: state.Title}
	pages := map[int]string{}
	invalid := false
	for _, raw := range urls {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		image, n, ok := issuuSVG(raw, state)
		if !ok {
			invalid = true
			continue
		}
		pages[n] = image
	}
	ordered := make([]int, 0, len(pages))
	for n := range pages {
		ordered = append(ordered, n)
	}
	sort.Ints(ordered)
	for _, n := range ordered {
		item := Item{URL: pages[n], RequireImage: true, PublicOnly: true, Metadata: map[string]string{"issuu_page": strconv.Itoa(n), "issuu_publication_id": state.PublicationID}}
		if referer := imageReferer(page.String(), item.URL); referer != "" {
			item.Headers = map[string]string{"Referer": referer}
		}
		result.Items = append(result.Items, item)
	}
	if len(result.Items) == 0 {
		return result, errors.New("Issuu 尚未提供可确认的阅读器页图，请在下载浏览器中打开文档")
	}
	if invalid || len(result.Items) < state.PageCount {
		return result, errors.New("Issuu 仅读取了阅读器当前公开的页图，其他页面尚未确认")
	}
	return result, nil
}

func discoverIssuu(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := issuuDocumentRef(root)
	if getter == nil || ref.publisher == "" {
		return result, errors.New("Issuu 文档地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || issuuDocumentRef(final) != ref || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("Issuu 文档页面不可用，可能需要登录或访问授权")
	}
	return issuuCollect(ctx, final, doc)
}

// Static HTML does not expose the reader's page SVGs in the currently observed
// frontend; do not derive image URLs from a page count.
func issuuCollect(ctx context.Context, page *url.URL, doc *html.Node) (Discovery, error) {
	state, err := issuuState(ctx, page, doc)
	if err != nil {
		return Discovery{IsPage: true}, err
	}
	return issuuItems(ctx, page, state, nil)
}

type issuuFrame struct {
	Source string   `json:"source"`
	Images []string `json:"images"`
}

// Read only the current same-origin reader DOM. This does not navigate, click,
// request hidden pages, or inspect browser credentials.
// shortcut: inspect at most 2000 exposed pages until larger readers are verified.
func issuuRenderedCollect(ctx context.Context, session *RenderedSession, page *url.URL, doc *html.Node) (Discovery, error) {
	state, err := issuuState(ctx, page, doc)
	if err != nil {
		return Discovery{IsPage: true}, err
	}
	if session == nil {
		return Discovery{IsPage: true, Title: state.Title}, errors.New("Issuu 阅读器不可用")
	}
	var frames []issuuFrame
	const script = `(() => {
		const out=[];
		for (const f of document.querySelectorAll('iframe[src]')) {
		  try {
		    const u=new URL(f.getAttribute('src'),location.href);
		    if (u.origin!==location.origin || u.pathname!='/rd4') continue;
		    const d=f.contentDocument; if(!d) continue;
		    const nodes=d.querySelectorAll('link[rel="preload"][as="image"][href], svg[data-src]');
		    const images=Array.from(nodes).slice(0,2000).map(n=>n.getAttribute(n.tagName.toLowerCase()==='link'?'href':'data-src')).filter(Boolean);
		    for (const section of d.querySelectorAll('section.page[style]')) {
		      for (const match of section.style.backgroundImage.matchAll(/url\(["']?([^"')]+)["']?\)/g)) images.push(match[1]);
		    }
		    out.push({source:u.href,images:images.slice(0,2000)});
		  } catch (_) {}
		}
		return out;
	})()`
	if err := session.run(ctx, chromedp.Evaluate(script, &frames)); err != nil {
		return Discovery{IsPage: true, Title: state.Title}, err
	}
	ref := issuuDocumentRef(page)
	var images []string
	for _, frame := range frames {
		u, err := url.Parse(frame.Source)
		if err != nil || !issuuHost(u) || !sameOrigin(page, u) || u.Path != "/rd4" {
			continue
		}
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(q["u"]) != 1 || len(q["d"]) != 1 || !strings.EqualFold(q.Get("u"), ref.publisher) || !strings.EqualFold(q.Get("d"), ref.document) {
			continue
		}
		images = append(images, frame.Images...)
	}
	return issuuItems(ctx, page, state, images)
}
