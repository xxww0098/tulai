package download

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var zcoolDigits = regexp.MustCompile(`^[0-9]{1,20}$`)

// shortcut: bound unusually large works and report an incomplete result.
const maxZcoolImages = 10000

func zcoolWorkToken(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		(!strings.EqualFold(u.Hostname(), "www.zcool.com.cn") && !strings.EqualFold(u.Hostname(), "zcool.com.cn")) ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "work" || !strings.HasSuffix(parts[1], ".html") {
		return ""
	}
	token := strings.TrimSuffix(parts[1], ".html")
	if len(token) < 3 || len(token) > 32 || token[0] != 'Z' {
		return ""
	}
	encoded := token[1:]
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil || !zcoolDigits.Match(decoded) || strings.Trim(string(decoded), "0") == "" {
		return ""
	}
	return token
}

func isZcoolWork(u *url.URL) bool { return zcoolWorkToken(u) != "" }

func discoverZcoolWork(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	token := zcoolWorkToken(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || token == "" {
		return result, errors.New("站酷作品地址或下载客户端无效")
	}
	page := *root
	if _, ok := getter.(*HTTPGetter); ok {
		page.Scheme, page.Host = "https", "www.zcool.com.cn"
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		return result, fmt.Errorf("读取站酷作品失败: %w", err)
	}
	if doc == nil || final == nil || !sameOrigin(&page, final) || zcoolWorkToken(final) != token {
		return result, errors.New("站酷作品已跳转，可能需要登录或安全验证")
	}
	return zcoolWorkPage(ctx, doc, final)
}

func zcoolImageURL(page *url.URL, raw string) (string, bool) {
	initial, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.Contains(initial.Path, "..") {
		return "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil ||
		(!strings.EqualFold(u.Hostname(), "img.zcool.cn") && !strings.EqualFold(u.Hostname(), "img.zcool.com.cn")) ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		!strings.HasPrefix(u.Path, "/community/") {
		return "", false
	}
	u.Scheme, u.Host = "https", strings.ToLower(u.Hostname())
	return u.String(), true
}

// zcoolWorkPage only reads this work's productImages or its workShowBox DOM.
// Signed CDN query parameters are retained, including image processing rules.
func zcoolWorkPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if doc == nil || !isZcoolWork(page) {
		return result, errors.New("站酷作品页面无效")
	}
	var nextData json.RawMessage
	var fallbackTitle string
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && fallbackTitle == "" {
			fallbackTitle = nodeText(n)
		}
		if n.Data != "script" || attribute(n, "id") != "__NEXT_DATA__" || len(nextData) != 0 {
			return
		}
		var text strings.Builder
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.TextNode {
				text.WriteString(child.Data)
			}
		}
		if text.Len() <= maxPageBytes {
			nextData = json.RawMessage(text.String())
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	type productImage struct {
		URLBig string `json:"urlBig"`
	}
	var state struct {
		Props struct {
			PageProps struct {
				Data struct {
					Title         string          `json:"title"`
					ProductImages json.RawMessage `json:"productImages"`
				} `json:"data"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	stateValid := len(nextData) > 0 && json.Unmarshal(nextData, &state) == nil
	result.Title = strings.TrimSpace(state.Props.PageProps.Data.Title)
	if result.Title == "" {
		result.Title = fallbackTitle
	}
	seen := make(map[string]bool)
	invalid, limited := 0, false
	add := func(raw string) {
		if ctx.Err() != nil || limited {
			return
		}
		image, ok := zcoolImageURL(page, raw)
		if !ok {
			invalid++
			return
		}
		if seen[image] {
			return
		}
		if len(result.Items) >= maxZcoolImages {
			limited = true
			return
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, Metadata: map[string]string{"zcool_work": zcoolWorkToken(page)}}
		if ref := imageReferer(page.Scheme+"://"+page.Host+"/", image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	imagesRaw := state.Props.PageProps.Data.ProductImages
	var images []productImage
	imagesPresent := stateValid && len(imagesRaw) > 0 && !bytes.Equal(bytes.TrimSpace(imagesRaw), []byte("null"))
	badList := false
	if imagesPresent {
		if json.Unmarshal(imagesRaw, &images) != nil {
			badList = true
		} else {
			for _, image := range images {
				if ctx.Err() != nil || limited {
					break
				}
				if image.URLBig == "" {
					invalid++
					continue
				}
				add(image.URLBig)
			}
		}
	}
	if len(result.Items) == 0 {
		walkHTML(doc, func(n *html.Node) {
			if ctx.Err() != nil || limited || n.Type != html.ElementNode || n.Data != "img" {
				return
			}
			inside := false
			for parent := n; parent != nil; parent = parent.Parent {
				for _, class := range strings.Fields(attribute(parent, "class")) {
					if class == "workShowBox" {
						inside = true
					}
				}
			}
			if !inside {
				return
			}
			raw := firstNonEmpty(attribute(n, "data-original"), attribute(n, "data-src"), attribute(n, "src"))
			if raw != "" {
				add(raw)
			}
		})
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var problems []error
	if len(nextData) > 0 && !stateValid {
		problems = append(problems, errors.New("站酷作品数据格式无效，结果可能不完整"))
	}
	if badList {
		problems = append(problems, errors.New("站酷作品图片列表格式无效"))
	}
	if invalid > 0 {
		problems = append(problems, fmt.Errorf("%d 张站酷图片地址无效或不可识别", invalid))
	}
	if limited {
		problems = append(problems, fmt.Errorf("站酷作品超过 %d 张图片，结果不完整", maxZcoolImages))
	}
	if len(result.Items) == 0 {
		problems = append(problems, errors.New("站酷作品未返回图片，可能需要登录或安全验证"))
	}
	return result, errors.Join(problems...)
}
