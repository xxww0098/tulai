package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	deviantArtPath  = regexp.MustCompile(`^/[A-Za-z0-9_-]{1,64}/art/[^/]+-([0-9]{6,15})/?$`)
	deviantArtState = regexp.MustCompile(`\bwindow\s*\.\s*__INITIAL_STATE__\s*=\s*JSON\s*\.\s*parse\s*\(\s*`)
	deviantArtCDN   = regexp.MustCompile(`^images-wixmp-[a-f0-9]{24}\.wixmp\.com$`)
	deviantArtName  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,200}$`)
)

func deviantArtID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		(!strings.EqualFold(u.Hostname(), "deviantart.com") && !strings.EqualFold(u.Hostname(), "www.deviantart.com")) ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return ""
	}
	match := deviantArtPath.FindStringSubmatch(u.Path)
	if len(match) != 2 || strings.Trim(match[1], "0") == "" {
		return ""
	}
	return match[1]
}

func isDeviantArtWork(u *url.URL) bool { return deviantArtID(u) != "" }

func discoverDeviantArtWork(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := deviantArtID(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || id == "" {
		return result, errors.New("DeviantArt 作品地址或下载客户端无效")
	}
	page := *root
	if _, ok := getter.(*HTTPGetter); ok {
		page.Scheme, page.Host = "https", "www.deviantart.com"
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		return result, fmt.Errorf("读取 DeviantArt 作品失败: %w", err)
	}
	if doc == nil || final == nil || !sameOrigin(&page, final) || deviantArtID(final) != id {
		return result, errors.New("DeviantArt 作品已跳转，可能需要登录或验证")
	}
	return deviantArtWorkPage(ctx, doc, final)
}

type deviantArtMedia struct {
	BaseURI    string   `json:"baseUri"`
	PrettyName string   `json:"prettyName"`
	Tokens     []string `json:"token"`
	Types      []struct {
		Type      string `json:"t"`
		Component string `json:"c"`
	} `json:"types"`
}

type deviantArtStateData struct {
	Entities struct {
		Deviations map[string]struct {
			ID             json.RawMessage `json:"deviationId"`
			URL            string          `json:"url"`
			Title          string          `json:"title"`
			Type           string          `json:"type"`
			IsBlocked      bool            `json:"isBlocked"`
			IsDeleted      bool            `json:"isDeleted"`
			IsDefaultImage bool            `json:"isDefaultImage"`
			IsDownloadable bool            `json:"isDownloadable"`
			IsVideo        bool            `json:"isVideo"`
			IsMultiMedia   bool            `json:"isMultiMedia"`
			Media          deviantArtMedia `json:"media"`
		} `json:"deviation"`
		Extended map[string]struct {
			Download *struct {
				URL string `json:"url"`
			} `json:"download"`
			Restrictions *struct {
				Remaining *int `json:"remaining"`
			} `json:"downloadRestrictions"`
		} `json:"deviationExtended"`
	} `json:"@@entities"`
}

func deviantArtCDNURL(raw string) (string, bool) {
	u, err := crawlURL(nil, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") ||
		!deviantArtCDN.MatchString(strings.ToLower(u.Hostname())) || !strings.HasPrefix(u.Path, "/f/") || strings.Contains(u.Path, "..") {
		return "", false
	}
	u.Host = strings.ToLower(u.Hostname())
	return u.String(), true
}

func deviantArtFullview(media deviantArtMedia) (string, bool) {
	base, ok := deviantArtCDNURL(media.BaseURI)
	if !ok || len(media.Tokens) > 1 || !deviantArtName.MatchString(media.PrettyName) || strings.Contains(media.PrettyName, "..") {
		return "", false
	}
	u, _ := url.Parse(base)
	if u.RawQuery != "" {
		return "", false
	}
	var component string
	for _, entry := range media.Types {
		if entry.Type == "fullview" {
			component = entry.Component
			break
		}
	}
	if component == "" || len(component) > 4096 || !strings.HasPrefix(component, "/v1/") || strings.ContainsAny(component, "?#") || strings.Contains(component, "..") {
		return "", false
	}
	component = strings.ReplaceAll(component, "<prettyName>", media.PrettyName)
	if strings.ContainsAny(component, "<>") {
		return "", false
	}
	u.Path += component
	u.RawPath = ""
	if len(media.Tokens) == 1 {
		if len(media.Tokens[0]) == 0 || len(media.Tokens[0]) > 8192 {
			return "", false
		}
		u.RawQuery = url.Values{"token": {media.Tokens[0]}}.Encode()
	}
	return deviantArtCDNURL(u.String())
}

func deviantArtOriginalURL(page *url.URL, raw, id string) (string, bool) {
	if value, ok := deviantArtCDNURL(raw); ok {
		return value, true
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !sameOrigin(page, u) ||
		!(u.Path == "/download/"+id || strings.HasPrefix(u.Path, "/download/"+id+"/")) {
		return "", false
	}
	return u.String(), true
}

func deviantArtWorkPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := deviantArtID(page)
	if doc == nil || id == "" {
		return result, errors.New("DeviantArt 作品页面无效")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var ogURL, ogImage, ogTitle string
	var state deviantArtStateData
	stateSeen, stateValid := false, false
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "meta" {
			switch strings.ToLower(firstNonEmpty(attribute(n, "property"), attribute(n, "name"))) {
			case "og:url":
				ogURL = attribute(n, "content")
			case "og:image":
				ogImage = attribute(n, "content")
			case "og:title":
				ogTitle = attribute(n, "content")
			}
		}
		if n.Data != "script" || stateValid {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode {
				continue
			}
			for _, match := range offer1688CodeMatches(child.Data, deviantArtState) {
				stateSeen = true
				tail := child.Data[match[1]:]
				decoded, consumed, ok := offer1688String(tail)
				if !ok || len(decoded) > maxPageBytes || !strings.HasPrefix(strings.TrimSpace(tail[consumed:]), ")") {
					continue
				}
				var candidate deviantArtStateData
				if json.Unmarshal([]byte(decoded), &candidate) == nil {
					state, stateValid = candidate, true
					break
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if ogURL != "" {
		canonical, err := crawlURL(page, ogURL)
		if err != nil || deviantArtID(canonical) != id {
			return result, errors.New("DeviantArt 页面作品 ID 不匹配")
		}
	}
	if ogTitle != "" {
		result.Title = ogTitle
	}
	var original, fullview string
	var problems []error
	if stateValid {
		work, found := state.Entities.Deviations[id]
		if !found {
			return result, errors.New("DeviantArt 页面未包含当前作品，可能需要登录或验证")
		}
		var targetID string
		if json.Unmarshal(work.ID, &targetID) != nil {
			targetID = strings.TrimSpace(string(work.ID))
		}
		if targetID != "" && targetID != "null" && targetID != id {
			return result, errors.New("DeviantArt 作品数据 ID 不匹配")
		}
		if work.URL != "" {
			current, err := crawlURL(page, work.URL)
			if err != nil || deviantArtID(current) != id {
				return result, errors.New("DeviantArt 作品数据地址不匹配")
			}
		}
		if work.IsBlocked || work.IsDeleted || work.IsDefaultImage {
			return result, errors.New("DeviantArt 作品不可访问，可能需要登录或授权")
		}
		if work.Title != "" {
			result.Title = work.Title
		}
		if work.IsVideo || (work.Type != "" && work.Type != "image") {
			return result, errors.New("DeviantArt 作品不是静态图片")
		}
		if work.IsMultiMedia {
			problems = append(problems, errors.New("DeviantArt 多媒体作品仅采集当前图片，结果可能不完整"))
		}
		fullview, _ = deviantArtFullview(work.Media)
		if ext, ok := state.Entities.Extended[id]; ok && work.IsDownloadable && ext.Download != nil &&
			(ext.Restrictions == nil || ext.Restrictions.Remaining == nil || *ext.Restrictions.Remaining > 0) {
			if value, ok := deviantArtOriginalURL(page, ext.Download.URL, id); ok {
				original = value
			} else {
				problems = append(problems, errors.New("DeviantArt 原图地址不可识别，已尝试展示图"))
			}
		}
	} else if stateSeen {
		problems = append(problems, errors.New("DeviantArt 作品数据格式无效，可能只能取得预览"))
	}
	kind := "original"
	image := original
	if image == "" {
		image, kind = fullview, "fullview"
	}
	if image == "" && ogURL != "" {
		if preview, ok := deviantArtCDNURL(ogImage); ok {
			image, kind = preview, "preview"
			problems = append(problems, errors.New("DeviantArt 仅返回预览图，可能不是完整尺寸"))
		}
	}
	if image == "" {
		return result, errors.Join(append(problems, errors.New("DeviantArt 作品未返回可用图片，可能需要登录或授权"))...)
	}
	item := Item{URL: image, RequireImage: true, Metadata: map[string]string{"deviantart_id": id, "image_kind": kind}}
	if original != "" && fullview != "" && original != fullview {
		a, _ := url.Parse(original)
		b, _ := url.Parse(fullview)
		if sameOrigin(a, b) {
			item.AlternateURLs = []string{fullview}
		}
	}
	if ref := imageReferer(page.Scheme+"://"+page.Host+"/", image); ref != "" {
		item.Headers = map[string]string{"Referer": ref}
	}
	result.Items = append(result.Items, item)
	return result, errors.Join(problems...)
}
