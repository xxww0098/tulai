package download

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var zhihuArticlePath = regexp.MustCompile(`^/p/([0-9]{1,20})/?$`)
var zhihuAnswerPath = regexp.MustCompile(`^/question/([0-9]{1,20})/answer/([0-9]{1,20})/?$`)

type zhihuRef struct{ kind, question, id string }

func zhihuPageRef(u *url.URL) zhihuRef {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return zhihuRef{}
	}
	switch strings.ToLower(u.Hostname()) {
	case "zhuanlan.zhihu.com":
		if m := zhihuArticlePath.FindStringSubmatch(u.Path); len(m) == 2 {
			return zhihuRef{kind: "article", id: m[1]}
		}
	case "www.zhihu.com", "zhihu.com":
		if m := zhihuAnswerPath.FindStringSubmatch(u.Path); len(m) == 3 {
			return zhihuRef{kind: "answer", question: m[1], id: m[2]}
		}
	}
	return zhihuRef{}
}

func isZhihuPage(u *url.URL) bool { return zhihuPageRef(u).id != "" }

func discoverZhihu(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := zhihuPageRef(root)
	if getter == nil || ref.id == "" {
		return result, errors.New("知乎文章或回答地址无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || zhihuPageRef(final) != ref || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("知乎页面已跳转，可能需要登录或安全验证")
	}
	return zhihuPage(ctx, doc, final)
}

func zhihuClass(n *html.Node, class string) bool {
	for _, value := range strings.Fields(attribute(n, "class")) {
		if value == class {
			return true
		}
	}
	return false
}

func zhihuAnswerOwner(n *html.Node, id string) bool {
	for p := n; p != nil; p = p.Parent {
		if p.Type != html.ElementNode || !zhihuClass(p, "AnswerItem") {
			continue
		}
		if attribute(p, "data-answer-id") == id {
			return true
		}
		var info struct {
			ItemID json.RawMessage `json:"itemId"`
			Type   string          `json:"type"`
		}
		if json.Unmarshal([]byte(attribute(p, "data-zop")), &info) == nil && (info.Type == "" || info.Type == "answer") {
			var value string
			if json.Unmarshal(info.ItemID, &value) != nil {
				value = string(info.ItemID)
			}
			return value == id
		}
		return false
	}
	return false
}

// shortcut: bound unusually large posts until the image limit is configurable.
func zhihuPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := zhihuPageRef(page)
	if doc == nil || ref.id == "" {
		return result, errors.New("知乎文章或回答页面无效")
	}
	var bodies []*html.Node
	var canonical string
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:url") {
			canonical = attribute(n, "content")
		}
		if ref.kind == "article" && (zhihuClass(n, "Post-RichText") || attribute(n, "itemprop") == "articleBody") {
			bodies = append(bodies, n)
		}
		if ref.kind == "answer" && zhihuClass(n, "RichContent-inner") && zhihuAnswerOwner(n, ref.id) {
			bodies = append(bodies, n)
		}
	})
	if canonical != "" {
		u, err := crawlURL(page, canonical)
		if err != nil || zhihuPageRef(u) != ref {
			return result, errors.New("知乎页面与请求的文章或回答不匹配")
		}
	}
	if len(bodies) == 0 {
		return result, errors.New("知乎未返回可确认的正文，可能需要登录或安全验证")
	}
	seen := map[string]bool{}
	limited := false
	for _, body := range bodies {
		walkHTML(body, func(n *html.Node) {
			if ctx.Err() != nil || limited || n.Type != html.ElementNode || n.Data != "img" {
				return
			}
			raw := firstNonEmpty(attribute(n, "data-original"), attribute(n, "data-actualsrc"), attribute(n, "src"))
			u, err := crawlURL(page, raw)
			if err != nil || u.User != nil || seen[u.String()] {
				return
			}
			if len(result.Items) >= 10000 {
				limited = true
				return
			}
			seen[u.String()] = true
			item := Item{URL: u.String(), RequireImage: true, PublicOnly: true}
			if referer := imageReferer(page.String(), item.URL); referer != "" {
				item.Headers = map[string]string{"Referer": referer}
			}
			result.Items = append(result.Items, item)
		})
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if limited {
		return result, errors.New("知乎正文图片超过 10000 张，结果可能不完整")
	}
	return result, nil
}
