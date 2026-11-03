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

var (
	pconlinePhotoPath  = regexp.MustCompile(`^/photo/(list_)?([1-9][0-9]{0,19})\.html$`)
	pconlinePhotoCount = regexp.MustCompile(`^[1-9][0-9]*\s*/\s*([1-9][0-9]*)$`)
)

// shortcut: bound anomalously large albums until the limit is configurable.
const maxPconlineImages = 10000

func pconlinePhotoID(u *url.URL) (string, bool) {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || !strings.EqualFold(u.Hostname(), "dp.pconline.com.cn") ||
		(u.Port() != "" && !(u.Scheme == "http" && u.Port() == "80") && !(u.Scheme == "https" && u.Port() == "443")) {
		return "", false
	}
	m := pconlinePhotoPath.FindStringSubmatch(u.Path)
	if len(m) != 3 {
		return "", false
	}
	return m[2], m[1] == "list_"
}

func isPconlinePhoto(u *url.URL) bool { id, _ := pconlinePhotoID(u); return id != "" }

func pconlineGroupURL(id string) *url.URL {
	return &url.URL{Scheme: "https", Host: "dp.pconline.com.cn", Path: "/photo/list_" + id + ".html"}
}

type pconlineRedirectGetter struct {
	base Getter
	id   string
}

func (g *pconlineRedirectGetter) Get(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
	current, err := crawlURL(nil, raw)
	want := pconlineGroupURL(g.id)
	id, group := pconlinePhotoID(current)
	if err != nil || !sameOrigin(current, want) || id != g.id || !group {
		return nil, errors.New("摄影部落作品地址无效")
	}
	for i := 0; i < 6; i++ {
		resp, err := g.base.Get(ctx, current.String(), prepare)
		if err != nil {
			return nil, err
		}
		final, err := crawlURL(nil, firstNonEmpty(resp.FinalURL, current.String()))
		id, group := pconlinePhotoID(final)
		if err != nil || !sameOrigin(want, final) || id != g.id || !group {
			resp.Body.Close()
			return nil, errors.New("摄影部落作品响应地址无效")
		}
		if resp.StatusCode != 301 && resp.StatusCode != 302 && resp.StatusCode != 303 && resp.StatusCode != 307 && resp.StatusCode != 308 {
			return resp, nil
		}
		location := resp.Header.Get("Location")
		resp.Body.Close()
		next, err := crawlURL(current, location)
		id, group = pconlinePhotoID(next)
		if err != nil || !sameOrigin(want, next) || id != g.id || !group {
			return nil, errors.New("摄影部落作品已跳转，可能需要登录或安全验证")
		}
		if next.String() == current.String() {
			return nil, errors.New("摄影部落作品重定向循环")
		}
		current = next
	}
	return nil, errors.New("摄影部落作品重定向次数过多")
}

func discoverPconlinePhoto(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id, _ := pconlinePhotoID(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if id == "" || getter == nil {
		return result, errors.New("摄影部落作品地址或下载客户端无效")
	}
	return pconlineFetchGroup(ctx, getter, id)
}

func pconlineFetchGroup(ctx context.Context, getter Getter, id string) (Discovery, error) {
	result := Discovery{IsPage: true}
	page := pconlineGroupURL(id)
	pageGetter := getter
	if source, ok := getter.(*HTTPGetter); ok {
		if source == nil || source.client == nil {
			return result, errors.New("摄影部落下载客户端无效")
		}
		clone, client := *source, *source.client
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client.Jar = nil
		clone.client = &client
		pageGetter = &pconlineRedirectGetter{base: &clone, id: id}
	}
	doc, final, _, resp, err := fetchCrawlPage(ctx, pageGetter, crawlPage{url: page.String(), depth: 1})
	if resp != nil {
		resp.Body.Close()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err != nil {
		var status *HTTPStatusError
		if errors.As(err, &status) {
			switch status.StatusCode {
			case http.StatusUnauthorized, http.StatusForbidden:
				return result, fmt.Errorf("摄影部落作品可能需要登录或安全验证: %w", err)
			case http.StatusTooManyRequests:
				return result, fmt.Errorf("摄影部落请求受到限流，请稍后重试: %w", err)
			}
		}
		return result, fmt.Errorf("读取摄影部落作品失败: %w", err)
	}
	finalID, group := pconlinePhotoID(final)
	if doc == nil || final == nil || !sameOrigin(page, final) || finalID != id || !group {
		return result, errors.New("摄影部落作品页面未加载或已跳转，可能需要登录或安全验证")
	}
	return pconlinePhotoPage(ctx, doc, final)
}

func pconlineCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	id, group := pconlinePhotoID(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if id == "" || doc == nil || getter == nil {
		return result, errors.New("摄影部落作品页面无效")
	}
	current, currentErr := pconlinePhotoPage(ctx, doc, page)
	if group {
		return current, currentErr
	}
	// Require a valid current-page canonical identity before making another request.
	if len(current.Items) == 0 {
		return current, currentErr
	}
	full, fullErr := pconlineFetchGroup(ctx, getter, id)
	if len(full.Items) == 0 {
		return current, errors.Join(currentErr, fullErr, errors.New("整组作品读取失败，仅保留当前展示图"))
	}
	return full, fullErr
}

func pconlineHasClass(n *html.Node, class string) bool {
	for _, candidate := range strings.Fields(attribute(n, "class")) {
		if candidate == class {
			return true
		}
	}
	return false
}

func pconlineImageURL(raw string) (string, bool) {
	if len(raw) > 16384 {
		return "", false
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "img.pconline.com.cn") ||
		(u.Port() != "" && u.Port() != "443") || !strings.HasPrefix(u.Path, "/images/upload/upc/tx/photoblog/") ||
		!strings.HasSuffix(u.Path, "_mthumb.jpg") || strings.Contains(u.Path, "..") {
		return "", false
	}
	u.Host, u.Fragment = "img.pconline.com.cn", ""
	return u.String(), true
}

func pconlinePhotoPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id, group := pconlinePhotoID(page)
	if doc == nil || id == "" {
		return result, errors.New("摄影部落作品页面无效")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var canonical []string
	var title string
	var count int
	var raw []string
	var badIndex, challenge bool
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" {
			text := nodeText(n)
			if strings.Contains(text, "安全验证") || strings.Contains(strings.ToLower(text), "security check") {
				challenge = true
			}
		}
		if n.Data == "h1" && title == "" {
			title = nodeText(n)
		}
		if n.Data == "link" {
			for _, rel := range strings.Fields(strings.ToLower(attribute(n, "rel"))) {
				if rel == "canonical" {
					canonical = append(canonical, attribute(n, "href"))
				}
			}
		}
		if pconlineHasClass(n, "photo-gallery-count") {
			if m := pconlinePhotoCount.FindStringSubmatch(nodeText(n)); len(m) == 2 {
				count, _ = strconv.Atoi(m[1])
			}
		}
		if n.Data != "img" {
			return
		}
		if group && pconlineHasClass(n, "photo-group-modal-image") {
			var slide, stage, modal bool
			for parent := n.Parent; parent != nil; parent = parent.Parent {
				if pconlineHasClass(parent, "photo-group-modal-slide") {
					index, err := strconv.Atoi(attribute(parent, "data-index"))
					if err != nil || index != len(raw) {
						badIndex = true
					}
					slide = true
				}
				stage = stage || pconlineHasClass(parent, "photo-group-modal-stage")
				modal = modal || pconlineHasClass(parent, "photo-group-modal")
			}
			if slide && stage && modal {
				raw = append(raw, attribute(n, "src"))
			}
		} else if !group && pconlineHasClass(n, "photo-gallery-image") {
			var stage, gallery bool
			for parent := n.Parent; parent != nil; parent = parent.Parent {
				stage = stage || pconlineHasClass(parent, "photo-gallery-stage")
				gallery = gallery || pconlineHasClass(parent, "photo-gallery")
			}
			if stage && gallery {
				raw = append(raw, attribute(n, "src"))
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if challenge {
		return result, errors.New("摄影部落需要完成安全验证")
	}
	if len(canonical) != 1 {
		return result, errors.New("摄影部落页面缺少唯一的作品地址，无法确认图片归属")
	}
	canonicalURL, err := crawlURL(page, canonical[0])
	canonicalID, canonicalGroup := pconlinePhotoID(canonicalURL)
	if err != nil || !sameOrigin(page, canonicalURL) || canonicalID != id || canonicalGroup != group {
		return result, errors.New("摄影部落页面与请求的作品不匹配")
	}
	result.Title = title
	seen := make(map[string]bool)
	var invalid, limited bool
	for _, value := range raw {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		image, ok := pconlineImageURL(value)
		if !ok {
			invalid = true
			continue
		}
		if seen[image] {
			continue
		}
		if len(result.Items) >= maxPconlineImages {
			limited = true
			break
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, Metadata: map[string]string{"pconline_photo_id": id, "image_kind": "display"}}
		if ref := imageReferer("https://dp.pconline.com.cn/", image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	var problems []error
	if len(result.Items) == 0 {
		problems = append(problems, errors.New("摄影部落没有已加载的展示图片"))
	} else {
		problems = append(problems, errors.New("仅获取摄影部落页面展示图，原图可能受权限限制"))
	}
	if badIndex || count == 0 || (group && count != len(result.Items)) || (!group && count > len(result.Items)) || invalid {
		problems = append(problems, errors.New("摄影部落作品图片可能不完整或来源未验证"))
	}
	if limited {
		problems = append(problems, fmt.Errorf("摄影部落作品超过 %d 张图片，结果不完整", maxPconlineImages))
	}
	return result, errors.Join(problems...)
}
