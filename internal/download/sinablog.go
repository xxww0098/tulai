package download

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var sinaBlogArticlePath = regexp.MustCompile(`(?i)^/s/blog_([0-9a-z]{16})\.html$`)
var sinaBlogSizeSuffix = regexp.MustCompile(`&[0-9]{1,5}$`)
var sinaBlogHomePath = regexp.MustCompile(`^/u/([0-9]{1,10})/?$`)
var sinaBlogListPath = regexp.MustCompile(`^/s/articlelist_([0-9]{1,10})_([0-9]{1,10})_([0-9]{1,5})\.html$`)

type sinaBlogListRef struct {
	uid, category string
	page          int
}

func sinaBlogListID(u *url.URL) sinaBlogListRef {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		!strings.EqualFold(u.Hostname(), "blog.sina.com.cn") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return sinaBlogListRef{}
	}
	if match := sinaBlogHomePath.FindStringSubmatch(u.Path); len(match) == 2 {
		return sinaBlogListRef{uid: match[1], category: "0", page: 1}
	}
	if match := sinaBlogListPath.FindStringSubmatch(u.Path); len(match) == 4 {
		page, _ := strconv.Atoi(match[3])
		if page > 0 {
			return sinaBlogListRef{uid: match[1], category: match[2], page: page}
		}
	}
	return sinaBlogListRef{}
}

func isSinaBlogList(u *url.URL) bool { return sinaBlogListID(u).uid != "" }

func sinaBlogHasClass(n *html.Node, name string) bool {
	for _, class := range strings.Fields(attribute(n, "class")) {
		if class == name {
			return true
		}
	}
	return false
}

// shortcut: bound directory pages, articles and images until limits are configurable.
func discoverSinaBlogList(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	ref := sinaBlogListID(root)
	uid, err := strconv.ParseUint(ref.uid, 10, 32)
	if getter == nil || err != nil || ref.page == 0 {
		return result, errors.New("新浪博客目录地址或用户编号无效")
	}
	owner := fmt.Sprintf("%08x", uid)
	page := *root
	page.Scheme, page.Host = "https", "blog.sina.com.cn"
	page.Path = fmt.Sprintf("/s/articlelist_%s_%s_%d.html", ref.uid, ref.category, ref.page)
	page.RawPath, page.RawQuery, page.Fragment = "", "", ""
	seen := map[string]bool{}
	var partial error
	articleFailed := false
	for count := 0; count < maxCrawlPages; count++ {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(partial, err)
		}
		doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: 1})
		if response != nil {
			response.Body.Close()
		}
		if err != nil {
			return result, errors.Join(partial, err)
		}
		if doc == nil || sinaBlogListID(final) != ref || !sameOrigin(&page, final) {
			return result, errors.Join(partial, errors.New("新浪博客目录不可用或跳转到其他目录"))
		}
		var articles []*url.URL
		var next string
		foundList, invalid, limited := false, false, false
		walkHTML(doc, func(n *html.Node) {
			if n.Type != html.ElementNode {
				return
			}
			if n.Data == "title" && result.Title == "" {
				result.Title = nodeText(n)
			}
			if sinaBlogHasClass(n, "articleList") {
				foundList = true
			}
			if sinaBlogHasClass(n, "atc_title") {
				walkHTML(n, func(a *html.Node) {
					if a.Type != html.ElementNode || a.Data != "a" {
						return
					}
					u, parseErr := crawlURL(final, attribute(a, "href"))
					if parseErr == nil && strings.EqualFold(u.Hostname(), "blog.sina.com.cn") && u.Scheme == "http" && u.Port() == "" {
						u.Scheme = "https"
					}
					id := sinaBlogArticleID(u)
					if parseErr != nil || id == "" || !strings.HasPrefix(id, owner) || !sameOrigin(final, u) {
						invalid = true
						return
					}
					if !seen[id] {
						if len(seen) >= 1000 {
							limited = true
							return
						}
						seen[id] = true
						articles = append(articles, u)
					}
				})
			}
			if sinaBlogHasClass(n, "SG_pgnext") {
				walkHTML(n, func(a *html.Node) {
					if a.Type == html.ElementNode && a.Data == "a" && next == "" {
						next = attribute(a, "href")
					}
				})
			}
		})
		if !foundList {
			return result, errors.Join(partial, errors.New("新浪博客未返回文章目录，可能需要登录或安全验证"))
		}
		if invalid {
			partial = errors.Join(partial, errors.New("部分新浪博客目录项目不属于当前用户或格式不受支持"))
		}
		for _, article := range articles {
			found, articleErr := discoverSinaBlogArticle(ctx, getter, article)
			if ctx.Err() != nil {
				return result, errors.Join(partial, ctx.Err())
			}
			if articleErr != nil {
				if !articleFailed {
					partial = errors.Join(partial, errors.New("部分新浪博客文章读取失败，结果可能不完整"))
					articleFailed = true
				}
			}
			for _, item := range found.Items {
				if len(result.Items) >= 10000 {
					return result, errors.Join(partial, errors.New("新浪博客目录超过 10000 张图片，结果可能不完整"))
				}
				item.Metadata = map[string]string{"album_id": sinaBlogArticleID(article), "album_title": found.Title}
				result.Items = append(result.Items, item)
			}
		}
		if limited {
			return result, errors.Join(partial, errors.New("新浪博客目录超过 1000 篇文章，结果可能不完整"))
		}
		if next == "" {
			return result, partial
		}
		nextPage, err := crawlURL(final, next)
		if err == nil && strings.EqualFold(nextPage.Hostname(), "blog.sina.com.cn") && nextPage.Scheme == "http" && nextPage.Port() == "" {
			nextPage.Scheme = "https"
		}
		wanted := ref
		wanted.page++
		if err != nil || !sameOrigin(final, nextPage) || sinaBlogListID(nextPage) != wanted {
			return result, errors.Join(partial, errors.New("新浪博客目录下一页地址无效，结果可能不完整"))
		}
		page, ref = *nextPage, wanted
	}
	return result, errors.Join(partial, errors.New("新浪博客目录达到 100 页上限，结果可能不完整"))
}

func sinaBlogArticleID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		!strings.EqualFold(u.Hostname(), "blog.sina.com.cn") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return ""
	}
	match := sinaBlogArticlePath.FindStringSubmatch(u.Path)
	if len(match) != 2 {
		return ""
	}
	return strings.ToLower(match[1])
}

func isSinaBlogArticle(u *url.URL) bool { return sinaBlogArticleID(u) != "" }

func discoverSinaBlogArticle(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	want := sinaBlogArticleID(root)
	if getter == nil || want == "" {
		return result, errors.New("新浪博客文章地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || sinaBlogArticleID(final) != want || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("新浪博客文章不可用或跳转到其他文章")
	}
	return sinaBlogArticlePage(ctx, doc, final)
}

func sinaBlogImage(page *url.URL, raw string) (string, string) {
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil {
		return "", ""
	}
	original := u.String()
	if domainOrSubdomain(u.Hostname(), "sinaimg.cn") && u.RawQuery == "" && u.RawPath == "" &&
		(u.Port() == "" || (u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443")) {
		parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 2)
		if len(parts) == 2 && parts[1] != "" && !strings.Contains(parts[1], "/") {
			switch parts[0] {
			case "small", "thumbnail", "bmiddle", "middle", "large", "orignal", "maxwidth.2048":
				u.Scheme, u.Host = "https", strings.ToLower(u.Hostname())
				original = u.String()
				u.Path = "/orignal/" + sinaBlogSizeSuffix.ReplaceAllString(parts[1], "")
			}
		}
	}
	if u.String() == original {
		return original, ""
	}
	return u.String(), original
}

func sinaBlogArticlePage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := sinaBlogArticleID(page)
	if doc == nil || id == "" {
		return result, errors.New("新浪博客文章页面无效")
	}
	var content *html.Node
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "h2" && strings.EqualFold(attribute(n, "id"), "t_"+id) {
			result.Title = nodeText(n)
		}
		if attribute(n, "id") == "sina_keyword_ad_area2" && content == nil {
			content = n
		}
	})
	if content == nil {
		return result, errors.New("新浪博客未返回文章正文，可能需要登录或安全验证")
	}
	seen := map[string]bool{}
	limited := false
	walkHTML(content, func(n *html.Node) {
		if ctx.Err() != nil || limited || n.Type != html.ElementNode || n.Data != "img" {
			return
		}
		raw := attribute(n, "real_src")
		if raw == "" {
			return
		}
		image, alternate := sinaBlogImage(page, raw)
		if image == "" || seen[image] {
			return
		}
		if len(result.Items) >= 10000 {
			limited = true
			return
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, PublicOnly: true}
		if alternate != "" {
			item.AlternateURLs = []string{alternate}
		}
		if ref := imageReferer(page.String(), image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	// shortcut: limit unusually large posts until page limits are configurable.
	if limited {
		return result, errors.New("新浪博客文章图片超过 10000 张，结果可能不完整")
	}
	return result, nil
}
