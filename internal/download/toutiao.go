package download

import (
	"context"
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	toutiaoArticlePath = regexp.MustCompile(`^/article/([1-9][0-9]{0,23})/?$`)
	toutiaoImageHost   = regexp.MustCompile(`^p[0-9]{1,2}(?:-sign)?\.toutiaoimg\.com$`)
)

func toutiaoArticleID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	if p := u.Port(); p != "" && !(u.Scheme == "https" && p == "443") && !(u.Scheme == "http" && p == "80") {
		return ""
	}
	switch strings.ToLower(u.Hostname()) {
	case "toutiao.com", "www.toutiao.com", "m.toutiao.com":
	default:
		return ""
	}
	parts := toutiaoArticlePath.FindStringSubmatch(u.Path)
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}

func isToutiaoArticle(u *url.URL) bool { return toutiaoArticleID(u) != "" }

func discoverToutiaoArticle(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := toutiaoArticleID(root)
	if getter == nil || id == "" {
		return result, errors.New("今日头条文章地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || toutiaoArticleID(final) != id || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("今日头条文章不可用或跳转到其他内容")
	}
	return toutiaoArticlePage(ctx, doc, final)
}

func toutiaoImageURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") ||
		!toutiaoImageHost.MatchString(strings.ToLower(u.Hostname())) || u.Path == "" ||
		path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") {
		return "", false
	}
	u.Fragment = ""
	return u.String(), true
}

func toutiaoArticleAncestor(n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type != html.ElementNode {
			continue
		}
		for _, class := range strings.Fields(attribute(p, "class")) {
			if class == "show-monitor" {
				return true
			}
		}
	}
	return false
}

// toutiaoArticlePage follows the reference article selector: image src values
// inside show-monitor. Page scripts are not executed and unrelated images are
// not used as fallback when the site returns a verification page.
func toutiaoArticlePage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := toutiaoArticleID(page)
	if doc == nil || id == "" {
		return result, errors.New("今日头条文章页面无效")
	}
	var identityMismatch bool
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(nodeText(n)), "-今日头条"))
		}
		var canonical string
		if n.Data == "link" && strings.EqualFold(attribute(n, "rel"), "canonical") {
			canonical = attribute(n, "href")
		} else if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:url") {
			canonical = attribute(n, "content")
		}
		if canonical != "" {
			ref, err := url.Parse(canonical)
			if err != nil || toutiaoArticleID(page.ResolveReference(ref)) != id {
				identityMismatch = true
			}
		}
	})
	if identityMismatch {
		return result, errors.New("今日头条页面声明的文章与请求不一致")
	}
	seen := map[string]bool{}
	incomplete := false
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode || n.Data != "img" || !toutiaoArticleAncestor(n) {
			return
		}
		image, ok := toutiaoImageURL(attribute(n, "src"))
		if !ok {
			incomplete = true
			return
		}
		if seen[image] {
			return
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"toutiao_article_id": id}}
		if referer := imageReferer(page.String(), image); referer != "" {
			item.Headers = map[string]string{"Referer": referer}
		}
		result.Items = append(result.Items, item)
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(result.Items) == 0 {
		return result, errors.New("今日头条未返回可确认属于该文章的正文图片，页面可能需要在浏览器中验证")
	}
	if incomplete {
		return result, errors.New("今日头条部分正文图片地址未能验证，结果可能不完整")
	}
	return result, nil
}
