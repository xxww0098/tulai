package download

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

func isTiebaThread(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host != "tieba.baidu.com" && host != "www.tieba.baidu.com" && host != "c.tieba.baidu.com" {
		return false
	}
	if port := u.Port(); port != "" && !((u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80")) {
		return false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(u.Path, "/p/"), "/")
	if !strings.HasPrefix(u.Path, "/p/") || id == "" {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// shortcut: 帖子最多扫描 100 页；需要更大帖子时改为可配置。
func discoverTiebaThread(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	initial, err := tiebaPageNumber(root)
	if err != nil {
		return result, err
	}
	queue := []*url.URL{root}
	seenPages := map[int]bool{initial: true}
	seenImages := map[string]bool{}
	var origin *url.URL
	limited := false
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		page := queue[0]
		queue = queue[1:]
		referer := ""
		if origin != nil {
			referer = pageReferer(origin)
		}
		doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), referer: referer, depth: 1})
		if response != nil {
			response.Body.Close()
		}
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			var status *HTTPStatusError
			if errors.As(err, &status) && status.StatusCode == 403 {
				return result, fmt.Errorf("读取百度贴吧帖子被拒绝，可能需要安全验证: %w", err)
			}
			return result, fmt.Errorf("读取百度贴吧帖子失败，结果可能不完整: %w", err)
		}
		if doc == nil || !tiebaThreadRedirect(root, page, final) {
			return result, errors.New("百度贴吧帖子不可用或跳转到了其他页面，结果可能不完整")
		}
		wantPage, _ := tiebaPageNumber(page)
		gotPage, pageErr := tiebaPageNumber(final)
		if pageErr != nil || gotPage != wantPage {
			return result, errors.New("百度贴吧帖子跳转到了其他页码，结果可能不完整")
		}
		for _, key := range []string{"see_lz", "only_thread"} {
			if root.Query().Has(key) && final.Query().Get(key) != root.Query().Get(key) {
				return result, errors.New("百度贴吧帖子跳转后筛选条件发生变化，结果可能不完整")
			}
		}
		if origin == nil {
			origin = final
		}
		content := false
		walkHTML(doc, func(n *html.Node) {
			if n.Type != html.ElementNode {
				return
			}
			if result.Title == "" && n.Data == "title" {
				result.Title = nodeText(n)
			}
			if attribute(n, "id") == "j_p_postlist" {
				content = true
			}
			classes := strings.Fields(attribute(n, "class"))
			for _, name := range classes {
				if name == "l_post" || name == "j_p_postlist" || name == "core_title_txt" || name == "BDE_Image" || name == "d_content_img" {
					content = true
				}
			}
			if n.Data == "img" && (tiebaHasClass(classes, "BDE_Image") || tiebaHasClass(classes, "d_content_img")) {
				for _, candidate := range imageCandidates(n) {
					u, err := crawlURL(final, candidate)
					if err != nil || u.User != nil {
						continue
					}
					image := u.String()
					if tiebaHasClass(classes, "BDE_Image") {
						image = tiebaOriginalURL(final.String(), image)
					}
					if seenImages[image] {
						continue
					}
					seenImages[image] = true
					item := Item{URL: image}
					if ref := imageReferer(pageReferer(final), image); ref != "" {
						item.Headers = map[string]string{"Referer": ref}
					}
					result.Items = append(result.Items, item)
				}
			}
			if n.Data != "a" {
				return
			}
			next, err := crawlURL(final, attribute(n, "href"))
			if err != nil || !tiebaThreadRedirect(root, page, next) {
				return
			}
			q := next.Query()
			if !q.Has("pn") {
				return
			}
			for _, key := range []string{"see_lz", "only_thread"} {
				if q.Has(key) && q.Get(key) != root.Query().Get(key) {
					return
				}
			}
			number, err := tiebaPageNumber(next)
			if err != nil || seenPages[number] {
				return
			}
			if len(seenPages) >= maxCrawlPages {
				limited = true
				return
			}
			seenPages[number] = true
			copy := *origin
			query := root.Query()
			query.Set("pn", strconv.Itoa(number))
			copy.RawQuery = query.Encode()
			queue = append(queue, &copy)
		})
		if !content {
			return result, errors.New("百度贴吧未返回可识别的帖子内容，可能需要安全验证")
		}
	}
	if limited {
		return result, fmt.Errorf("百度贴吧帖子超过 %d 页的扫描上限，结果可能不完整", maxCrawlPages)
	}
	return result, nil
}

func tiebaPageNumber(u *url.URL) (int, error) {
	value := u.Query().Get("pn")
	if value == "" {
		return 1, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		return 0, errors.New("百度贴吧帖子页码无效")
	}
	return n, nil
}

func tiebaThreadRedirect(root, from, to *url.URL) bool {
	if !isTiebaThread(to) || strings.TrimSuffix(root.Path, "/") != strings.TrimSuffix(to.Path, "/") {
		return false
	}
	if sameOrigin(from, to) {
		return true
	}
	return from.Scheme == "http" && to.Scheme == "https" && strings.EqualFold(from.Hostname(), to.Hostname())
}

func tiebaHasClass(classes []string, want string) bool {
	for _, class := range classes {
		if class == want {
			return true
		}
	}
	return false
}

// tiebaOriginalURL maps Baidu forum image transformations back to the item path.
func tiebaOriginalURL(source, raw string) string {
	page, err := crawlURL(nil, source)
	if err != nil || !isTiebaThread(page) {
		return raw
	}
	u, err := crawlURL(nil, raw)
	if err != nil || u.User != nil || (u.Port() != "" && u.Port() != "80" && u.Port() != "443") {
		return raw
	}
	host := strings.ToLower(u.Hostname())
	parts := strings.Split(u.Path, "/")
	if !strings.HasSuffix(host, ".baidu.com") || len(parts) < 5 || parts[1] != "forum" || parts[2] == "" || parts[3] == "" || parts[4] == "" {
		return raw
	}
	parts[2], parts[3] = "pic", "item"
	u.Path, u.RawPath = strings.Join(parts, "/"), ""
	return u.String()
}
