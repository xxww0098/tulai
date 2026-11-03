package download

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	baijiahaoID       = regexp.MustCompile(`^[1-9][0-9]{9,23}$`)
	baijiahaoUserID   = regexp.MustCompile(`^[A-Za-z0-9_-]{5,128}$`)
	baijiahaoState    = regexp.MustCompile(`\bwindow\.jsonData\s*=\s*`)
	baijiahaoImageCDN = regexp.MustCompile(`^pics[0-9]\.baidu\.com$`)
)

type baijiahaoArticle struct {
	BSData struct {
		Superlanding []struct {
			ItemType string `json:"itemType"`
			ItemData struct {
				Header  string `json:"header"`
				Readsrc struct {
					Link string `json:"link"`
				} `json:"readsrc"`
				Sections []struct {
					Type string `json:"type"`
					Link string `json:"link"`
				} `json:"sections"`
			} `json:"itemData"`
		} `json:"superlanding"`
	} `json:"bsData"`
}

func baijiahaoArticleID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || !strings.EqualFold(u.Hostname(), "baijiahao.baidu.com") || u.Path != "/s" {
		return ""
	}
	if port := u.Port(); port != "" && !(u.Scheme == "https" && port == "443") && !(u.Scheme == "http" && port == "80") {
		return ""
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q["id"]) != 1 || !baijiahaoID.MatchString(q.Get("id")) {
		return ""
	}
	return q.Get("id")
}

func isBaijiahaoHome(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || !strings.EqualFold(u.Hostname(), "author.baidu.com") || u.Path != "/home" {
		return false
	}
	if port := u.Port(); port != "" && !(u.Scheme == "https" && port == "443") && !(u.Scheme == "http" && port == "80") {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q["app_id"]) > 1 || len(q["uk"]) > 1 {
		return false
	}
	return baijiahaoID.MatchString(q.Get("app_id")) || baijiahaoUserID.MatchString(q.Get("uk"))
}

func isBaijiahaoPage(u *url.URL) bool { return baijiahaoArticleID(u) != "" || isBaijiahaoHome(u) }

func baijiahaoImageURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !baijiahaoImageCDN.MatchString(strings.ToLower(u.Hostname())) ||
		(u.Port() != "" && u.Port() != "443") || !strings.HasPrefix(u.Path, "/feed/") || path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") {
		return "", false
	}
	u.Fragment = ""
	return u.String(), true
}

func discoverBaijiahao(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isBaijiahaoPage(root) {
		return result, errors.New("百家号地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("百家号页面不可用或跳转异常")
	}
	if id := baijiahaoArticleID(root); id != "" {
		if baijiahaoArticleID(final) != id {
			return result, errors.New("百家号文章已跳转到其他内容")
		}
	} else if !isBaijiahaoHome(final) || !sameOrigin(root, final) {
		return result, errors.New("百家号主页已跳转到其他站点")
	}
	return baijiahaoCollect(ctx, getter, final, doc)
}

func baijiahaoArticleAncestor(n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && attribute(p, "data-testid") == "article" {
			return true
		}
	}
	return false
}

// baijiahaoCollect uses the article's readsrc identity and its own image
// sections; unrelated avatars and recommendation images are never collected.
func baijiahaoCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if doc == nil || getter == nil || !isBaijiahaoPage(page) {
		return result, errors.New("百家号页面无效")
	}
	if isBaijiahaoHome(page) {
		return result, errors.New("百家号主页的文章列表当前可能需要验证，暂无法完整采集")
	}
	id := baijiahaoArticleID(page)
	var found bool
	var sections []string
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data != "script" || found {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.TextNode {
				continue
			}
			for _, loc := range offer1688CodeMatches(c.Data, baijiahaoState) {
				var data baijiahaoArticle
				if json.NewDecoder(strings.NewReader(c.Data[loc[1]:])).Decode(&data) != nil {
					continue
				}
				for _, entry := range data.BSData.Superlanding {
					if entry.ItemType != "article" {
						continue
					}
					u, err := url.Parse(entry.ItemData.Readsrc.Link)
					if err != nil || baijiahaoArticleID(u) != id {
						continue
					}
					found = true
					result.Title = firstNonEmpty(entry.ItemData.Header, result.Title)
					for _, section := range entry.ItemData.Sections {
						if section.Type == "img" && section.Link != "" {
							sections = append(sections, section.Link)
						}
					}
					break
				}
				if found {
					break
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !found {
		return result, errors.New("百家号未返回可确认属于该文章的正文数据")
	}
	seen := map[string]bool{}
	incomplete := false
	add := func(raw string) {
		if strings.TrimSpace(raw) == "" {
			return
		}
		image, ok := baijiahaoImageURL(raw)
		if !ok {
			incomplete = true
			return
		}
		if seen[image] {
			return
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, Metadata: map[string]string{"baijiahao_article_id": id}}
		if referer := imageReferer(page.String(), image); referer != "" {
			item.Headers = map[string]string{"Referer": referer}
		}
		result.Items = append(result.Items, item)
	}
	for _, src := range sections {
		add(src)
	}
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() == nil && n.Type == html.ElementNode && n.Data == "img" && baijiahaoArticleAncestor(n) {
			add(firstNonEmpty(attribute(n, "data-src"), attribute(n, "src")))
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(result.Items) == 0 {
		return result, errors.New("百家号文章未返回可信图片")
	}
	if incomplete {
		return result, errors.New("百家号部分图片地址未能验证，结果可能不完整")
	}
	return result, nil
}
