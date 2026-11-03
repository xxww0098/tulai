package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var (
	yandePostPath  = regexp.MustCompile(`^/post/show/([1-9][0-9]{0,18})/?$`)
	yandeRegister  = regexp.MustCompile(`\bPost\.register(_resp)?\s*\(`)
	yandeImagePath = regexp.MustCompile(`^/image/([0-9a-f]{32})/[^/]+\.(?:jpe?g|png|gif|webp|bmp)$`)
)

func isYandePage(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		!strings.EqualFold(u.Hostname(), "yande.re") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return false
	}
	if u.Path == "/post" || u.Path == "/post/" {
		return true
	}
	match := yandePostPath.FindStringSubmatch(u.Path)
	if len(match) != 2 {
		return false
	}
	id, err := strconv.ParseInt(match[1], 10, 64)
	return err == nil && id > 0
}

type yandePost struct {
	ID      int64  `json:"id"`
	MD5     string `json:"md5"`
	FileURL string `json:"file_url"`
	Status  string `json:"status"`
}

func yandeImage(post yandePost) string {
	u, err := url.Parse(post.FileURL)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "files.yande.re") ||
		(u.Port() != "" && u.Port() != "443") || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	match := yandeImagePath.FindStringSubmatch(u.Path)
	if len(match) != 2 || match[1] != post.MD5 || post.ID <= 0 || post.Status != "active" {
		return ""
	}
	return u.String()
}

func discoverYande(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if getter == nil || !isYandePage(root) {
		return result, errors.New("yande.re 页面或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || !isYandePage(final) || (root.Scheme == "https" && final.Scheme != "https") ||
		root.Path != final.Path || root.Query().Encode() != final.Query().Encode() {
		return result, errors.New("yande.re 页面已跳转，可能需要登录或验证")
	}
	return yandeCollect(ctx, getter, final, doc)
}

// shortcut: inspect at most 100 result pages until search limits are configurable.
func yandeCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if getter == nil || doc == nil || !isYandePage(page) {
		return result, errors.New("yande.re 页面无效")
	}
	postID := int64(0)
	if match := yandePostPath.FindStringSubmatch(page.Path); len(match) == 2 {
		postID, _ = strconv.ParseInt(match[1], 10, 64)
	}
	pageNo := 1
	if raw := page.Query().Get("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 1000000 {
			return result, errors.New("yande.re 页码无效")
		}
		pageNo = n
	}
	seen := map[int64]bool{}
	missing := 0
	finish := func(err error) (Discovery, error) {
		if missing > 0 {
			err = errors.Join(err, fmt.Errorf("yande.re 有 %d 张图片未返回可验证的原图地址", missing))
		}
		return result, err
	}
	for count := 0; count < maxCrawlPages; count++ {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		listed := map[int64]bool{}
		var records []yandePost
		var nextRaw string
		walkHTML(doc, func(n *html.Node) {
			if n.Type != html.ElementNode {
				return
			}
			if n.Data == "title" && result.Title == "" {
				result.Title = nodeText(n)
			}
			if n.Data == "a" {
				for _, class := range strings.Fields(attribute(n, "class")) {
					if class == "thumb" {
						u, err := crawlURL(page, attribute(n, "href"))
						if err == nil && sameOrigin(page, u) {
							if m := yandePostPath.FindStringSubmatch(u.Path); len(m) == 2 {
								if id, err := strconv.ParseInt(m[1], 10, 64); err == nil {
									listed[id] = true
								}
							}
						}
					}
				}
				for _, rel := range strings.Fields(attribute(n, "rel")) {
					if rel == "next" && nextRaw == "" {
						nextRaw = attribute(n, "href")
					}
				}
			}
			if n.Data != "script" {
				return
			}
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				if child.Type != html.TextNode {
					continue
				}
				for _, m := range offer1688CodeMatches(child.Data, yandeRegister) {
					if m[2] >= 0 {
						var response struct {
							Posts []yandePost `json:"posts"`
						}
						if json.NewDecoder(strings.NewReader(child.Data[m[1]:])).Decode(&response) == nil {
							records = append(records, response.Posts...)
						}
					} else {
						var post yandePost
						if json.NewDecoder(strings.NewReader(child.Data[m[1]:])).Decode(&post) == nil {
							records = append(records, post)
						}
					}
				}
			}
		})
		if postID != 0 {
			listed = map[int64]bool{postID: true}
		}
		if len(listed) == 0 {
			return finish(errors.New("yande.re 页面未返回可确认的图片，可能需要登录或验证"))
		}
		added := 0
		for _, post := range records {
			if !listed[post.ID] || seen[post.ID] {
				continue
			}
			image := yandeImage(post)
			if image == "" {
				continue
			}
			seen[post.ID], added = true, added+1
			item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"yande_post_id": strconv.FormatInt(post.ID, 10)}}
			if ref := imageReferer(page.String(), image); ref != "" {
				item.Headers = map[string]string{"Referer": ref}
			}
			result.Items = append(result.Items, item)
		}
		for id := range listed {
			if !seen[id] {
				missing++
			}
		}
		if postID != 0 || nextRaw == "" {
			return finish(nil)
		}
		if added == 0 {
			return finish(errors.New("yande.re 分页没有新图片，结果可能不完整"))
		}
		if count == maxCrawlPages-1 {
			return finish(errors.New("yande.re 达到 100 页上限，结果可能不完整"))
		}
		next, err := crawlURL(page, nextRaw)
		if err != nil || !sameOrigin(page, next) || next.Path != page.Path {
			return finish(errors.New("yande.re 下一页地址无效，结果可能不完整"))
		}
		currentQuery, nextQuery := page.Query(), next.Query()
		currentQuery.Del("page")
		actual, err := strconv.Atoi(nextQuery.Get("page"))
		nextQuery.Del("page")
		if err != nil || actual != pageNo+1 || currentQuery.Encode() != nextQuery.Encode() {
			return finish(errors.New("yande.re 下一页筛选条件或页码不匹配，结果可能不完整"))
		}
		newDoc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: next.String(), referer: pageReferer(page), depth: 1})
		if response != nil {
			response.Body.Close()
		}
		if err != nil {
			return finish(fmt.Errorf("读取 yande.re 下一页失败: %w", err))
		}
		if newDoc == nil || !sameOrigin(next, final) || next.Path != final.Path || next.Query().Encode() != final.Query().Encode() {
			return finish(errors.New("yande.re 下一页发生跳转，结果可能不完整"))
		}
		page, pageNo, doc = final, actual, newDoc
	}
	return finish(nil)
}
