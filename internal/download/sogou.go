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

var sogouStateAssignment = regexp.MustCompile(`\bwindow\.__INITIAL_STATE__\s*=\s*`)

func isSogouImageSearch(u *url.URL) bool {
	return u != nil && u.User == nil && (u.Scheme == "https" || u.Scheme == "http") &&
		strings.EqualFold(u.Hostname(), "pic.sogou.com") && u.Path == "/pics" && strings.TrimSpace(u.Query().Get("query")) != "" &&
		(u.Port() == "" || (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80"))
}

type sogouSearchState struct {
	Route struct {
		Query struct {
			Query string `json:"query"`
			Start string `json:"start"`
		} `json:"query"`
	} `json:"route"`
	Search struct {
		Query    string `json:"query"`
		Total    int    `json:"totalNum"`
		PageSize int    `json:"xml_len"`
		List     []struct {
			Original string `json:"oriPicUrl"`
			Source   string `json:"url"`
		} `json:"searchList"`
	} `json:"searchList"`
}

func discoverSogouImages(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isSogouImageSearch(root) {
		return result, errors.New("搜狗图片搜索地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || !sameOrigin(root, final) || !isSogouImageSearch(final) || final.Query().Get("query") != root.Query().Get("query") {
		return result, errors.New("搜狗图片搜索页面不可用或已跳转")
	}
	return sogouCollect(ctx, getter, final, doc)
}

// shortcut: inspect at most 100 result pages until search limits are configurable.
func sogouCollect(ctx context.Context, getter Getter, page *url.URL, first *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if getter == nil || first == nil || !isSogouImageSearch(page) {
		return result, errors.New("搜狗图片搜索页面无效")
	}
	query := page.Query().Get("query")
	start := 0
	if raw := page.Query().Get("start"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 || value > 1000000 {
			return result, errors.New("搜狗图片分页位置无效")
		}
		start = value
	}
	seen := map[string]bool{}
	missing := 0
	finish := func(err error) (Discovery, error) {
		if missing > 0 {
			err = errors.Join(err, fmt.Errorf("搜狗图片有 %d 条结果缺少有效原图地址", missing))
		}
		return result, err
	}
	for count := 0; count < maxCrawlPages; count++ {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		var state sogouSearchState
		found := false
		walkHTML(first, func(n *html.Node) {
			if n.Type != html.ElementNode {
				return
			}
			if n.Data == "title" && result.Title == "" {
				result.Title = nodeText(n)
			}
			if n.Data != "script" || found {
				return
			}
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				if child.Type != html.TextNode {
					continue
				}
				for _, match := range offer1688CodeMatches(child.Data, sogouStateAssignment) {
					if json.NewDecoder(strings.NewReader(child.Data[match[1]:])).Decode(&state) == nil {
						found = true
						return
					}
				}
			}
		})
		if !found || state.Search.List == nil || state.Search.Query != query || state.Route.Query.Query != query {
			return finish(errors.New("搜狗图片未返回匹配的搜索结果，可能需要验证"))
		}
		if state.Route.Query.Start != "" {
			actual, err := strconv.Atoi(state.Route.Query.Start)
			if err != nil || actual != start {
				return finish(errors.New("搜狗图片分页位置不匹配，结果可能不完整"))
			}
		} else if start != 0 {
			return finish(errors.New("搜狗图片分页位置缺失，结果可能不完整"))
		}
		added := 0
		for _, row := range state.Search.List {
			u, err := publicImageURL(row.Original)
			if err != nil || u.User != nil {
				missing++
				continue
			}
			image := u.String()
			if seen[image] {
				continue
			}
			seen[image] = true
			added++
			item := Item{URL: image, RequireImage: true, PublicOnly: true}
			if source, err := crawlURL(nil, row.Source); err == nil {
				if referer := imageReferer(source.String(), image); referer != "" {
					item.Headers = map[string]string{"Referer": referer}
				}
			}
			result.Items = append(result.Items, item)
		}
		if state.Search.Total < 0 || state.Search.PageSize < 1 || state.Search.PageSize > 200 {
			return finish(errors.New("搜狗图片分页信息无效，结果可能不完整"))
		}
		if len(state.Search.List) == 0 || start+state.Search.PageSize >= state.Search.Total {
			return finish(nil)
		}
		if count > 0 && added == 0 {
			return finish(errors.New("搜狗图片分页没有新结果，结果可能不完整"))
		}
		if count == maxCrawlPages-1 {
			return finish(errors.New("搜狗图片达到 100 页上限，结果可能不完整"))
		}
		next := *page
		values := next.Query()
		start += state.Search.PageSize
		values.Set("start", strconv.Itoa(start))
		values.Set("xml_len", strconv.Itoa(state.Search.PageSize))
		next.RawQuery = values.Encode()
		doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: next.String(), referer: pageReferer(page), depth: 1})
		if response != nil {
			response.Body.Close()
		}
		if err != nil {
			return finish(fmt.Errorf("读取搜狗图片下一页失败，结果可能不完整: %w", err))
		}
		if doc == nil || !sameOrigin(page, final) || !isSogouImageSearch(final) || final.Query().Get("query") != query {
			return finish(errors.New("搜狗图片下一页已跳转，结果可能不完整"))
		}
		first = doc
	}
	return finish(errors.New("搜狗图片达到 100 页上限，结果可能不完整"))
}
