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
	rajceOwner       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
	rajceImageHost   = regexp.MustCompile(`^img[0-9]{1,3}\.rajce\.idnes\.cz$`)
	rajceImageLabel  = regexp.MustCompile(`^img[0-9]{1,3}$`)
	rajceMediumID    = regexp.MustCompile(`^[0-9]{1,24}$`)
	rajceSettingsVar = regexp.MustCompile(`\bvar\s+settings\s*=`)
	rajceStorageVar  = regexp.MustCompile(`\bvar\s+storage\s*=`)
	rajcePhotosVar   = regexp.MustCompile(`\bvar\s+photos\s*=`)
)

const maxRajcePhotos = 10000

type rajceAlbumRef struct {
	owner, album, medium string
	legacy               bool
}

func rajceAlbum(page *url.URL) rajceAlbumRef {
	if page == nil || page.User != nil || (page.Scheme != "http" && page.Scheme != "https") ||
		(page.Port() != "" && !(page.Scheme == "https" && page.Port() == "443") && !(page.Scheme == "http" && page.Port() == "80")) ||
		strings.Contains(strings.ToLower(page.EscapedPath()), "%2f") || strings.Contains(strings.ToLower(page.EscapedPath()), "%5c") ||
		strings.Contains(page.Path, "\\") || strings.Contains(page.Path, "..") {
		return rajceAlbumRef{}
	}
	host := strings.ToLower(page.Hostname())
	parts := strings.Split(strings.Trim(page.Path, "/"), "/")
	switch host {
	case "www.rajce.idnes.cz", "rajce.idnes.cz", "rajce.net", "www.rajce.net":
		if (len(parts) != 3 && len(parts) != 4) || !rajceOwner.MatchString(parts[0]) || parts[1] != "album" || parts[2] == "" || len(parts[2]) > 512 {
			return rajceAlbumRef{}
		}
		ref := rajceAlbumRef{owner: strings.ToLower(parts[0]), album: parts[2]}
		if len(parts) == 4 {
			if !rajceMediumID.MatchString(parts[3]) {
				return rajceAlbumRef{}
			}
			ref.medium = parts[3]
		}
		return ref
	}
	for _, suffix := range []string{".rajce.idnes.cz", ".rajce.net"} {
		if !strings.HasSuffix(host, suffix) {
			continue
		}
		owner := strings.TrimSuffix(host, suffix)
		if !rajceOwner.MatchString(owner) || rajceReservedOwner(owner) ||
			len(parts) != 1 || parts[0] == "" || len(parts[0]) > 512 {
			return rajceAlbumRef{}
		}
		return rajceAlbumRef{owner: owner, album: parts[0], legacy: true}
	}
	return rajceAlbumRef{}
}

func isRajceAlbum(page *url.URL) bool { return rajceAlbum(page).owner != "" }

func rajceReservedOwner(owner string) bool {
	switch owner {
	case "www", "alba", "api", "download", "forum", "static":
		return true
	}
	return rajceImageLabel.MatchString(owner)
}

// Profile pages load their album list dynamically. Only recognize the official
// user-subdomain homepage here; do not silently scan a partial generic page.
func isRajceProfile(page *url.URL) bool {
	if page == nil || page.User != nil || (page.Scheme != "http" && page.Scheme != "https") ||
		(page.Port() != "" && !(page.Scheme == "https" && page.Port() == "443") && !(page.Scheme == "http" && page.Port() == "80")) ||
		(page.Path != "" && page.Path != "/") {
		return false
	}
	host := strings.ToLower(page.Hostname())
	for _, suffix := range []string{".rajce.idnes.cz", ".rajce.net"} {
		if strings.HasSuffix(host, suffix) {
			owner := strings.TrimSuffix(host, suffix)
			return rajceOwner.MatchString(owner) && !rajceReservedOwner(owner)
		}
	}
	return false
}

func rajceProfileUnsupported() error {
	return errors.New("Rajče 用户主页的相册列表暂未完整支持，请打开单个相册后采集")
}

// A legacy host may redirect to a renamed canonical slug, but never to a
// different owner's album. Canonical routes must keep both owner and slug.
func rajceSameAlbum(a, b *url.URL) bool {
	x, y := rajceAlbum(a), rajceAlbum(b)
	if x.owner == "" || y.owner == "" || x.owner != y.owner {
		return false
	}
	if x.legacy && !y.legacy {
		return true
	}
	return x.album == y.album
}

func rajceStorage(raw string) (*url.URL, bool) {
	if len(raw) == 0 || len(raw) > 8192 {
		return nil, false
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "http" && u.Port() == "80") && !(u.Scheme == "https" && u.Port() == "443")) ||
		!rajceImageHost.MatchString(strings.ToLower(u.Hostname())) || u.RawQuery != "" || u.Fragment != "" ||
		!strings.HasPrefix(u.Path, "/") || !strings.HasSuffix(u.Path, "/") || strings.Contains(u.Path, "..") ||
		strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") || strings.Contains(u.Path, "\\") {
		return nil, false
	}
	u.Scheme, u.Host = "https", strings.ToLower(u.Hostname())
	return u, true
}

func rajceImageURL(storage *url.URL, raw string) (string, bool) {
	if storage == nil || len(raw) == 0 || len(raw) > 8192 {
		return "", false
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || strings.Contains(parsed.Path, "..") || strings.Contains(parsed.Path, "\\") ||
		strings.Contains(strings.ToLower(parsed.EscapedPath()), "%2f") {
		return "", false
	}
	u, err := crawlURL(storage, raw)
	if err != nil || u.User != nil ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		!strings.EqualFold(u.Hostname(), storage.Hostname()) || !strings.HasPrefix(u.Path, storage.Path+"images/") || u.Path == storage.Path+"images/" {
		return "", false
	}
	u.Scheme, u.Host = "https", storage.Hostname()
	return u.String(), true
}

type rajceMedium struct {
	FileName string `json:"fileName"`
	Name     string `json:"name"`
	Desc     string `json:"desc"`
	ImageURL string `json:"image_url"`
	Type     string `json:"type"`
	IsVideo  bool   `json:"is_video"`
	OldVideo bool   `json:"isVideo"`
}

type rajceSettings struct {
	AlbumName    string      `json:"album_name"`
	AlbumID      json.Number `json:"album_id"`
	AlbumStorage string      `json:"album_storage"`
	AlbumURL     struct {
		Path string `json:"path"`
	} `json:"album_url"`
	MediaCount  *int          `json:"media_count"`
	LegacyMedia []rajceMedium `json:"legacy_media"`
}

type rajceLD struct {
	Type        string `json:"@type"`
	URL         string `json:"url"`
	ContentURL  string `json:"contentUrl"`
	Description string `json:"description"`
	Name        string `json:"name"`
}

func discoverRajceAlbum(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if getter == nil || !isRajceAlbum(root) {
		return result, errors.New("Rajče 相册地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		return result, fmt.Errorf("读取 Rajče 相册失败: %w", err)
	}
	if doc == nil || final == nil || !rajceSameAlbum(root, final) || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("Rajče 相册已跳转，可能需要登录、分享权限或安全验证")
	}
	return rajceCollect(ctx, getter, final, doc)
}

// rajceCollect handles both the captured DOM and fetched HTML without running JS.
func rajceCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isRajceAlbum(page) || doc == nil {
		return result, errors.New("Rajče 相册页面或下载客户端无效")
	}
	var settings rajceSettings
	var haveSettings, malformed bool
	var storageRaw string
	var legacy []rajceMedium
	var ld []rajceLD
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data != "script" {
			return
		}
		for text := n.FirstChild; text != nil; text = text.NextSibling {
			if text.Type != html.TextNode {
				continue
			}
			if strings.EqualFold(attribute(n, "type"), "application/ld+json") {
				var data []rajceLD
				if json.Unmarshal([]byte(text.Data), &data) == nil {
					ld = append(ld, data...)
				}
				continue
			}
			for _, match := range offer1688CodeMatches(text.Data, rajceSettingsVar) {
				tail := strings.TrimSpace(text.Data[match[1]:])
				decoder := json.NewDecoder(strings.NewReader(tail))
				var value rajceSettings
				if decoder.Decode(&value) != nil || !offer1688LiteralEnd(tail[int(decoder.InputOffset()):]) {
					malformed = true
					continue
				}
				if haveSettings {
					malformed = true
					continue
				}
				settings, haveSettings = value, true
			}
			for _, match := range offer1688CodeMatches(text.Data, rajceStorageVar) {
				tail := strings.TrimSpace(text.Data[match[1]:])
				value, used, ok := offer1688String(tail)
				if ok && offer1688LiteralEnd(tail[used:]) && storageRaw == "" {
					storageRaw = value
				}
			}
			for _, match := range offer1688CodeMatches(text.Data, rajcePhotosVar) {
				tail := strings.TrimSpace(text.Data[match[1]:])
				decoder := json.NewDecoder(strings.NewReader(tail))
				var value []rajceMedium
				if decoder.Decode(&value) == nil && offer1688LiteralEnd(tail[int(decoder.InputOffset()):]) {
					legacy = append(legacy, value...)
				} else {
					malformed = true
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if haveSettings && settings.AlbumURL.Path != "" {
		canonical := *page
		canonical.Path, canonical.RawPath = settings.AlbumURL.Path, ""
		if !rajceSameAlbum(page, &canonical) {
			return result, errors.New("Rajče 相册数据与当前页面不匹配")
		}
	}
	if settings.AlbumName != "" {
		result.Title = settings.AlbumName
	}
	if settings.AlbumStorage != "" {
		if storageRaw != "" {
			first, firstOK := rajceStorage(storageRaw)
			second, secondOK := rajceStorage(settings.AlbumStorage)
			if !firstOK || !secondOK || first.String() != second.String() {
				return result, errors.New("Rajče 相册图片来源不一致")
			}
		}
		storageRaw = settings.AlbumStorage
	}
	storage, ok := rajceStorage(storageRaw)
	if !ok {
		return result, errors.New("未找到 Rajče 相册图片来源，可能需要登录、分享权限或安全验证")
	}
	for _, entry := range ld {
		if entry.Type == "MediaGallery" && entry.URL != "" {
			gallery, err := crawlURL(page, entry.URL)
			if err != nil || !rajceSameAlbum(page, gallery) {
				return result, errors.New("Rajče 相册元数据与当前页面不匹配")
			}
		}
	}
	seen := map[string]bool{}
	invalid, limited := false, false
	add := func(raw, name string) {
		if ctx.Err() != nil {
			return
		}
		image, valid := rajceImageURL(storage, raw)
		if !valid {
			invalid = true
			return
		}
		if seen[image] {
			return
		}
		if len(result.Items) >= maxRajcePhotos {
			limited = true
			return
		}
		seen[image] = true
		metadata := map[string]string{"album_title": result.Title}
		if settings.AlbumID != "" {
			metadata["album_id"] = settings.AlbumID.String()
		}
		if name != "" {
			metadata["title"] = name
		}
		item := Item{URL: image, RequireImage: true, Metadata: metadata}
		if ref := imageReferer(page.String(), image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	images, videos := 0, 0
	for _, entry := range ld {
		switch entry.Type {
		case "ImageObject":
			images++
			add(entry.ContentURL, firstNonEmpty(entry.Description, entry.Name))
		case "VideoObject":
			videos++
		}
	}
	if images == 0 && len(settings.LegacyMedia) > 0 {
		videos = 0
		for _, entry := range settings.LegacyMedia {
			if entry.IsVideo || entry.OldVideo || strings.EqualFold(entry.Type, "video") {
				videos++
				continue
			}
			images++
			add(entry.ImageURL, firstNonEmpty(entry.Desc, entry.Name))
		}
	} else if images == 0 && len(legacy) > 0 {
		videos = 0
		for _, entry := range legacy {
			if entry.IsVideo || entry.OldVideo || strings.EqualFold(entry.Type, "video") {
				videos++
				continue
			}
			images++
			if entry.FileName == "" || strings.ContainsAny(entry.FileName, `/\\?#`) || strings.Contains(entry.FileName, "..") {
				invalid = true
				continue
			}
			u := *storage
			u.Path += "images/" + entry.FileName
			u.RawPath = ""
			add(u.String(), firstNonEmpty(entry.Desc, entry.Name))
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var issues []error
	if settings.MediaCount == nil || *settings.MediaCount < 0 || images+videos != *settings.MediaCount {
		issues = append(issues, errors.New("无法确认 Rajče 相册内容完整，结果可能不完整"))
	}
	if images == 0 && videos == 0 && (settings.MediaCount == nil || *settings.MediaCount != 0) {
		issues = append(issues, errors.New("未找到 Rajče 相册图片，可能需要登录、分享权限或等待加载"))
	}
	if videos > 0 {
		issues = append(issues, fmt.Errorf("Rajče 相册包含 %d 个视频，已跳过", videos))
	}
	if invalid || malformed {
		issues = append(issues, errors.New("部分 Rajče 相册数据或图片地址无效，结果可能不完整"))
	}
	if limited {
		issues = append(issues, fmt.Errorf("Rajče 相册超过 %d 张图片上限", maxRajcePhotos))
	}
	return result, errors.Join(issues...)
}
