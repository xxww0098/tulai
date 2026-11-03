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
	zolAlbumPath  = regexp.MustCompile(`^/[0-9]{1,8}/([1-9][0-9]{4,11})/pic\.shtml$`)
	zolViewerPath = regexp.MustCompile(`^/picture_index_[0-9]{1,10}/index[0-9]{1,15}_[0-9]{1,12}_p([1-9][0-9]{4,11})\.shtml$`)
	zolPicList    = regexp.MustCompile(`\b(?:var|let|const)\s+picList\s*=\s*`)
	zolPicture    = regexp.MustCompile(`^/product/([0-9]{1,10})_(?:100x75|280x210)/([0-9]{1,10}/[A-Za-z0-9_-]+\.(?:jpg|jpeg|png|webp|gif))$`)
	zolID         = regexp.MustCompile(`^[1-9][0-9]{0,14}$`)
)

type zolRef struct {
	id     string
	viewer bool
}

type zolPictureEntry struct {
	ProID       json.RawMessage `json:"proId"`
	PicID       json.RawMessage `json:"picId"`
	PicSrc      string          `json:"picSrc"`
	Name        string          `json:"name"`
	ClassName   string          `json:"className"`
	ClassPicPos json.RawMessage `json:"classPicPos"`
}

func zolPageRef(u *url.URL) zolRef {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || !strings.EqualFold(u.Hostname(), "detail.zol.com.cn") {
		return zolRef{}
	}
	if port := u.Port(); port != "" && !(u.Scheme == "https" && port == "443") && !(u.Scheme == "http" && port == "80") {
		return zolRef{}
	}
	if match := zolAlbumPath.FindStringSubmatch(u.Path); len(match) == 2 {
		return zolRef{id: match[1]}
	}
	if match := zolViewerPath.FindStringSubmatch(u.Path); len(match) == 2 {
		return zolRef{id: match[1], viewer: true}
	}
	return zolRef{}
}

func isZOLPage(u *url.URL) bool { return zolPageRef(u).id != "" }

func zolRawID(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		value = string(raw)
	}
	if zolID.MatchString(value) {
		return value
	}
	return ""
}

func zolOriginalImage(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") || path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") {
		return "", false
	}
	switch strings.ToLower(u.Hostname()) {
	case "2a.zol-img.com.cn", "2b.zol-img.com.cn", "2c.zol-img.com.cn", "2d.zol-img.com.cn", "2e.zol-img.com.cn", "2f.zol-img.com.cn":
	default:
		return "", false
	}
	match := zolPicture.FindStringSubmatch(u.Path)
	if len(match) != 3 {
		return "", false
	}
	u.Path = "/product/" + match[1] + "/" + match[2]
	u.RawPath = ""
	u.Fragment = ""
	return u.String(), true
}

func discoverZOL(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := zolPageRef(root).id
	if getter == nil || id == "" {
		return result, errors.New("中关村在线图片页地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || zolPageRef(final).id != id || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("中关村在线图片页不可用或跳转到其他产品")
	}
	return zolCollect(ctx, getter, final, doc)
}

func zolClass(n *html.Node, want string) bool {
	for _, class := range strings.Fields(attribute(n, "class")) {
		if class == want {
			return true
		}
	}
	return false
}

func zolItem(page *url.URL, image, productID, pictureID, category string) Item {
	metadata := map[string]string{"zol_product_id": productID}
	if pictureID != "" {
		metadata["zol_picture_id"] = pictureID
	}
	if category != "" {
		metadata["category"] = category
	}
	item := Item{URL: image, RequireImage: true, Metadata: metadata}
	if referer := imageReferer(page.String(), image); referer != "" {
		item.Headers = map[string]string{"Referer": referer}
	}
	return item
}

// zolCollect follows a product image link from pic.shtml to the same product's
// viewer; the viewer's JSON picList describes the complete picture collection.
func zolCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := zolPageRef(page)
	if doc == nil || ref.id == "" || getter == nil {
		return result, errors.New("中关村在线图片页面无效")
	}
	if ref.viewer {
		return zolViewer(ctx, doc, page)
	}
	var viewer *url.URL
	seen := map[string]bool{}
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data != "a" || !zolClass(n, "imgwrap") {
			return
		}
		u, err := crawlURL(page, attribute(n, "href"))
		target := zolPageRef(u)
		if err != nil || target.id != ref.id || !target.viewer || (page.Scheme == "https" && u.Scheme != "https") {
			return
		}
		if viewer == nil {
			copyURL := *u
			copyURL.RawQuery, copyURL.Fragment = "", ""
			viewer = &copyURL
		}
		walkHTML(n, func(child *html.Node) {
			if child.Type != html.ElementNode || child.Data != "img" {
				return
			}
			image, ok := zolOriginalImage(firstNonEmpty(attribute(child, ".src"), attribute(child, "src")))
			if ok && !seen[image] {
				seen[image] = true
				result.Items = append(result.Items, zolItem(page, image, ref.id, "", attribute(child, "alt")))
			}
		})
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if viewer == nil {
		return result, errors.New("中关村在线未返回该产品的图片集链接")
	}
	viewerDoc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: viewer.String(), depth: 1, referer: page.Scheme + "://" + page.Host + "/"})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, errors.Join(errors.New("中关村在线图片集加载失败，已保留部分图片"), err)
	}
	actual := zolPageRef(final)
	if viewerDoc == nil || actual.id != ref.id || !actual.viewer || (viewer.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("中关村在线图片集已跳转到其他产品，已保留部分图片")
	}
	collected, err := zolViewer(ctx, viewerDoc, final)
	if len(collected.Items) == 0 {
		return result, errors.Join(errors.New("中关村在线图片集未加载，已保留部分图片"), err)
	}
	return collected, err
}

func zolViewer(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := zolPageRef(page)
	if doc == nil || !ref.viewer {
		return result, errors.New("中关村在线图片集页面无效")
	}
	var entries []zolPictureEntry
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data != "script" || entries != nil {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.TextNode {
				continue
			}
			for _, loc := range offer1688CodeMatches(c.Data, zolPicList) {
				var list []zolPictureEntry
				if json.NewDecoder(strings.NewReader(c.Data[loc[1]:])).Decode(&list) == nil {
					entries = list
					return
				}
			}
		}
	})
	seen := map[string]bool{}
	incomplete := false
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		id := zolRawID(entry.PicID)
		image, ok := zolOriginalImage(entry.PicSrc)
		if zolRawID(entry.ProID) != ref.id || id == "" || !ok {
			incomplete = true
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		result.Items = append(result.Items, zolItem(page, image, ref.id, id, firstNonEmpty(entry.ClassName, entry.Name)))
	}
	if len(result.Items) == 0 {
		return result, errors.New("中关村在线未返回可确认属于该产品的原图")
	}
	if incomplete {
		return result, errors.New("中关村在线部分图片地址无效，结果可能不完整")
	}
	return result, nil
}
