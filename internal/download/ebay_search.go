package download

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var ebaySearchPath = regexp.MustCompile(`^/sch/(?:[0-9]+/)?i\.html$`)

func ebaySearchMarket(host string) string {
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	switch host {
	case "cafr.ebay.ca":
		host = "ebay.ca"
	case "benl.ebay.be", "befr.ebay.be":
		host = "ebay.be"
	}
	if ebayMarkets[host] {
		return host
	}
	return ""
}

func isEbaySearch(u *url.URL) bool {
	return u != nil && u.User == nil && (u.Scheme == "http" || u.Scheme == "https") &&
		(u.Port() == "" || (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80")) &&
		ebaySearchMarket(u.Hostname()) != "" && ebaySearchPath.MatchString(u.Path)
}

func ebaySearchQuery(u *url.URL) (url.Values, int, error) {
	if !isEbaySearch(u) || len(u.RawQuery) > 8192 {
		return nil, 0, errors.New("eBay 搜索地址无效")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query["_pgn"]) > 1 || len(query["_nkw"]) > 1 {
		return nil, 0, errors.New("eBay 搜索条件或页码无效")
	}
	page := 1
	if query.Has("_pgn") {
		page, err = strconv.Atoi(query.Get("_pgn"))
		if err != nil || page < 1 || page > 1000000 {
			return nil, 0, errors.New("eBay 搜索页码无效")
		}
	}
	delete(query, "_pgn")
	delete(query, "_trksid")
	delete(query, "_trkparms")
	return query, page, nil
}

// ebaySearchSameRequest permits only site tracking changes and an HTTPS upgrade.
func ebaySearchSameRequest(expected, actual *url.URL) bool {
	first, firstPage, err := ebaySearchQuery(expected)
	if err != nil {
		return false
	}
	second, secondPage, err := ebaySearchQuery(actual)
	return err == nil && expected.Path == actual.Path && ebaySearchMarket(expected.Hostname()) == ebaySearchMarket(actual.Hostname()) &&
		(expected.Scheme != "https" || actual.Scheme == "https") && firstPage == secondPage && first.Encode() == second.Encode()
}

func ebaySearchNext(page *url.URL, raw string) (*url.URL, error) {
	next, err := crawlURL(page, raw)
	if err != nil || next.User != nil {
		return nil, errors.New("eBay 下一页地址无效")
	}
	_, pageNumber, err := ebaySearchQuery(page)
	if err != nil {
		return nil, err
	}
	query, nextNumber, err := ebaySearchQuery(next)
	if err != nil || nextNumber != pageNumber+1 {
		return nil, errors.New("eBay 下一页页码无效")
	}
	query.Set("_pgn", strconv.Itoa(pageNumber))
	comparison := *next
	comparison.RawQuery = query.Encode()
	if !ebaySearchSameRequest(page, &comparison) {
		return nil, errors.New("eBay 下一页改变了站点或搜索条件")
	}
	return next, nil
}

func ebaySearchFetch(ctx context.Context, getter Getter, expected *url.URL) (*html.Node, *url.URL, error) {
	switch g := getter.(type) {
	case *HTTPGetter:
		if g == nil || g.client == nil {
			return nil, nil, errors.New("eBay 搜索下载客户端无效")
		}
		clone, client := *g, *g.client
		previous := client.CheckRedirect
		client.Jar = nil
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= 6 || !ebaySearchSameRequest(expected, req.URL) {
				return errors.New("eBay 搜索跳转改变了站点或搜索条件")
			}
			if previous != nil {
				if err := previous(req, via); err != nil {
					return err
				}
			}
			if !ebaySearchSameRequest(expected, req.URL) {
				return errors.New("eBay 搜索跳转改变了站点或搜索条件")
			}
			return nil
		}
		clone.client = &client
		getter = &clone
	case *RenderedSession:
		if g == nil {
			return nil, nil, errors.New("eBay 浏览器会话无效")
		}
		ctx = context.WithValue(ctx, renderedRedirectGuardKey{}, func(next *url.URL) bool {
			return ebaySearchSameRequest(expected, next)
		})
	default:
		return nil, nil, errors.New("eBay 搜索需要支持安全重定向的下载客户端")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: expected.String(), depth: 1})
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		return nil, nil, err
	}
	if doc == nil || final == nil || !ebaySearchSameRequest(expected, final) {
		return nil, nil, errors.New("eBay 搜索页已跳转或查询条件发生变化")
	}
	return doc, final, nil
}

func discoverEbaySearch(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if _, _, err := ebaySearchQuery(root); err != nil {
		return result, err
	}
	page := *root
	if page.Scheme == "http" {
		page.Scheme, page.Host = "https", page.Hostname()
	}
	doc, final, err := ebaySearchFetch(ctx, getter, &page)
	if err != nil {
		return result, fmt.Errorf("读取 eBay 搜索页失败: %w", err)
	}
	return ebaySearchCollect(ctx, getter, final, doc)
}

type ebaySearchPage struct {
	title, next string
	items       []Item
	missing     int
	blocked     bool
	noResults   bool
	grid        bool
	ambiguous   bool
	limited     bool
}

func ebaySearchParse(ctx context.Context, page *url.URL, doc *html.Node) ebaySearchPage {
	var found ebaySearchPage
	var containers []*html.Node
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || !googleImagesStaticVisible(n) {
			return
		}
		if n.Type == html.TextNode && n.Parent != nil && n.Parent.Data != "script" && n.Parent.Data != "style" {
			value := strings.ToLower(n.Data)
			found.blocked = found.blocked || strings.Contains(value, "verify yourself") || strings.Contains(value, "verify you are human") ||
				strings.Contains(value, "pardon our interruption") || strings.Contains(value, "access to this page has been denied") ||
				strings.Contains(value, "security measure") || strings.Contains(value, "unusual traffic")
			found.noResults = found.noResults || strings.Contains(value, "no exact matches found") || strings.Contains(value, "no results found")
		}
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && found.title == "" {
			found.title = nodeText(n)
		}
		if n.Data == "div" && ebayHasClass(n, "srp-river-results") {
			found.grid = true
			containers = append(containers, n)
		}
		if n.Data == "a" && ebayHasClass(n, "pagination__next") && !strings.EqualFold(attribute(n, "aria-disabled"), "true") {
			raw := attribute(n, "href")
			if raw != "" && found.next != "" && raw != found.next {
				found.ambiguous = true
			} else if raw != "" {
				found.next = raw
			}
		}
	})
	if found.blocked || found.noResults {
		return found
	}
	for _, container := range containers {
		walkHTML(container, func(card *html.Node) {
			if ctx.Err() != nil || card.Type != html.ElementNode || card.Data != "li" || !ebayHasClass(card, "s-item") || !googleImagesStaticVisible(card) {
				return
			}
			if len(found.items) == maxEbayImages {
				found.limited = true
				return
			}
			var id, title, rawImage, alt string
			walkHTML(card, func(n *html.Node) {
				if n.Type != html.ElementNode || !googleImagesStaticVisible(n) {
					return
				}
				if n.Data == "a" && ebayHasClass(n, "s-item__link") && id == "" {
					if product, err := crawlURL(page, attribute(n, "href")); err == nil && ebaySearchMarket(product.Hostname()) == ebaySearchMarket(page.Hostname()) {
						id = ebayItemID(product)
					}
				}
				if n.Data == "div" && ebayHasClass(n, "s-item__title") && title == "" {
					title = nodeText(n)
				}
				if n.Data != "img" || rawImage != "" {
					return
				}
				withinImage := false
				for parent, depth := n.Parent, 0; parent != nil && parent != card && depth < 128; parent, depth = parent.Parent, depth+1 {
					if parent.Data == "div" && ebayHasClass(parent, "image-treatment") {
						withinImage = true
						break
					}
				}
				if !withinImage {
					return
				}
				for _, name := range []string{"src", "data-src"} {
					if raw := attribute(n, name); raw != "" {
						if _, ok := ebayImageURL(page, raw); ok {
							rawImage, alt = raw, attribute(n, "alt")
							break
						}
					}
				}
			})
			if id == "" {
				return
			}
			if rawImage == "" {
				found.missing++
				return
			}
			original, _ := ebayImageURL(page, rawImage)
			u, _ := url.Parse(original)
			u.Path = ebayThumbSize.ReplaceAllString(u.Path, `-l1600.$1`)
			u.RawPath = ""
			image := u.String()
			title = firstNonEmpty(title, alt)
			item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"ebay_item_id": id, "image_kind": "main", "album_id": id, "album_title": title}}
			if image != original {
				item.AlternateURLs = []string{original}
			}
			if ref := imageReferer(page.String(), image); ref != "" {
				item.Headers = map[string]string{"Referer": ref}
			}
			found.items = append(found.items, item)
		})
	}
	return found
}

// shortcut: follow at most 100 advertised pages and 10,000 products per search.
func ebaySearchCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if _, _, err := ebaySearchQuery(page); err != nil || doc == nil || getter == nil {
		return result, errors.New("eBay 搜索页面或下载客户端无效")
	}
	seen := make(map[string]bool)
	missing := 0
	finish := func(err error) (Discovery, error) {
		if missing != 0 {
			err = errors.Join(err, fmt.Errorf("eBay 有 %d 个商品缺少可确认的图片", missing))
		}
		return result, err
	}
	for count := 0; count < maxCrawlPages; count++ {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		parsed := ebaySearchParse(ctx, page, doc)
		if result.Title == "" {
			result.Title = parsed.title
		}
		if parsed.blocked {
			return finish(errors.New("eBay 搜索需要验证或拒绝访问，结果可能不完整"))
		}
		if parsed.noResults {
			return finish(nil)
		}
		if !parsed.grid || (len(parsed.items) == 0 && parsed.missing == 0) {
			return finish(errors.New("eBay 搜索页面未返回可识别的商品列表"))
		}
		missing += parsed.missing
		newCount := 0
		for _, item := range parsed.items {
			id := item.Metadata["ebay_item_id"]
			if seen[id] {
				continue
			}
			if len(result.Items) == maxEbayImages {
				return finish(fmt.Errorf("eBay 搜索已达到 %d 个商品的扫描上限", maxEbayImages))
			}
			seen[id] = true
			result.Items = append(result.Items, item)
			newCount++
		}
		if parsed.limited {
			return finish(fmt.Errorf("eBay 搜索已达到 %d 个商品的扫描上限", maxEbayImages))
		}
		if parsed.ambiguous {
			return finish(errors.New("eBay 下一页链接不明确，结果可能不完整"))
		}
		if count > 0 && newCount == 0 {
			return finish(errors.New("eBay 下一页没有新商品，结果可能不完整"))
		}
		if parsed.next == "" {
			return finish(nil)
		}
		if count+1 == maxCrawlPages {
			return finish(fmt.Errorf("eBay 搜索已达到 %d 页的扫描上限", maxCrawlPages))
		}
		next, err := ebaySearchNext(page, parsed.next)
		if err != nil {
			return finish(fmt.Errorf("eBay 搜索结果可能不完整: %w", err))
		}
		doc, page, err = ebaySearchFetch(ctx, getter, next)
		if err != nil {
			if ctx.Err() != nil {
				return finish(ctx.Err())
			}
			return finish(fmt.Errorf("读取 eBay 下一页失败，已保留当前结果: %w", err))
		}
	}
	return finish(nil)
}
