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
	imdbTitlePath = regexp.MustCompile(`^/title/(tt[0-9]{5,12})(/mediaindex)?/?$`)
	imdbImageID   = regexp.MustCompile(`^rm[0-9]{1,20}$`)
)

type imdbRef struct {
	id    string
	index bool
}

func imdbPageRef(u *url.URL) imdbRef {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return imdbRef{}
	}
	if port := u.Port(); port != "" && !(u.Scheme == "https" && port == "443") && !(u.Scheme == "http" && port == "80") {
		return imdbRef{}
	}
	if host := strings.ToLower(u.Hostname()); host != "www.imdb.com" && host != "imdb.com" {
		return imdbRef{}
	}
	match := imdbTitlePath.FindStringSubmatch(u.Path)
	if len(match) != 3 {
		return imdbRef{}
	}
	return imdbRef{id: match[1], index: match[2] != ""}
}

func isIMDbPage(u *url.URL) bool { return imdbPageRef(u).id != "" }

func imdbIndexURL(page *url.URL, id string) *url.URL {
	target := *page
	target.Path = "/title/" + id + "/mediaindex/"
	target.RawPath, target.RawQuery, target.Fragment = "", "", ""
	return &target
}

func discoverIMDb(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := imdbPageRef(root)
	if getter == nil || ref.id == "" {
		return result, errors.New("IMDb 影片地址或下载客户端无效")
	}
	target := root
	if !ref.index {
		target = imdbIndexURL(root, ref.id)
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: target.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	actual := imdbPageRef(final)
	if doc == nil || actual.id != ref.id || !actual.index || (target.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("IMDb 图片页不可用或跳转到其他影片")
	}
	return imdbPage(ctx, doc, final)
}

// imdbCollect lets a rendered title page load the same media index through its
// current session; an already-rendered media index can be parsed directly.
func imdbCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	ref := imdbPageRef(page)
	if ref.id == "" {
		return Discovery{IsPage: true}, errors.New("IMDb 影片地址无效")
	}
	if ref.index {
		return imdbPage(ctx, doc, page)
	}
	return discoverIMDb(ctx, getter, page)
}

func imdbImageURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "m.media-amazon.com") ||
		(u.Port() != "" && u.Port() != "443") || !strings.HasPrefix(u.Path, "/images/M/") ||
		path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") {
		return "", false
	}
	u.Fragment = ""
	return u.String(), true
}

// imdbPage reads only an image collection directly attached to the requested
// title ID in the reference app's __NEXT_DATA__ response.
func imdbPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := imdbPageRef(page)
	if doc == nil || ref.id == "" || !ref.index {
		return result, errors.New("IMDb 图片页面无效")
	}
	var states []any
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = strings.TrimSuffix(strings.TrimSpace(nodeText(n)), " - IMDb")
		}
		if n.Data != "script" || attribute(n, "id") != "__NEXT_DATA__" {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				var state any
				if json.Unmarshal([]byte(c.Data), &state) == nil {
					states = append(states, state)
				}
			}
		}
	})
	seen := map[string]bool{}
	foundCollection := false
	incomplete := false
	var visit func(any, int)
	visit = func(value any, depth int) {
		if ctx.Err() != nil || depth > 64 {
			return
		}
		switch node := value.(type) {
		case map[string]any:
			if node["id"] == ref.id {
				if images, ok := node["images"].(map[string]any); ok {
					if edges, ok := images["edges"].([]any); ok {
						foundCollection = true
						pageInfo, _ := images["pageInfo"].(map[string]any)
						hasNext, known := pageInfo["hasNextPage"].(bool)
						if !known || hasNext {
							incomplete = true
						}
						for _, edge := range edges {
							edgeObj, _ := edge.(map[string]any)
							image, _ := edgeObj["node"].(map[string]any)
							id, _ := image["id"].(string)
							raw, _ := image["url"].(string)
							imageURL, valid := imdbImageURL(raw)
							if !valid || !imdbImageID.MatchString(id) {
								incomplete = true
								continue
							}
							if seen[id] {
								continue
							}
							seen[id] = true
							metadata := map[string]string{"imdb_title_id": ref.id, "imdb_image_id": id}
							if caption, ok := image["caption"].(map[string]any); ok {
								if text, ok := caption["plainText"].(string); ok && text != "" {
									metadata["caption"] = text
								}
							}
							item := Item{URL: imageURL, RequireImage: true, Metadata: metadata}
							if referer := imageReferer(page.String(), imageURL); referer != "" {
								item.Headers = map[string]string{"Referer": referer}
							}
							result.Items = append(result.Items, item)
						}
					}
				}
			}
			for _, child := range node {
				visit(child, depth+1)
			}
		case []any:
			for _, child := range node {
				visit(child, depth+1)
			}
		}
	}
	for _, state := range states {
		visit(state, 0)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !foundCollection || len(result.Items) == 0 {
		return result, errors.New("IMDb 未返回可确认属于该影片的图片数据，可能需要登录或安全验证")
	}
	if incomplete {
		return result, errors.New("IMDb 图片仍有分页或部分图片不可用，结果可能不完整")
	}
	return result, nil
}
