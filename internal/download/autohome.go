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

var autohomePhotoPath = regexp.MustCompile(`^/(photo|clubphoto)/series/([0-9]{1,12})/([0-9]{1,8})/([0-9]{1,12})\.html$`)
var autohomePhotoHost = regexp.MustCompile(`^car[0-9]{1,2}\.autoimg\.cn$`)
var autohomeNeighbor = regexp.MustCompile(`\bvar\s+(?:prevurl|nexturl)\s*=\s*['"]([^'"\r\n]+)['"]`)
var autohomeModel = regexp.MustCompile(`\bvar\s+__CarModel\s*=\s*`)

type autohomePhotoRef struct{ kind, model, category, photo string }

func autohomePhotoID(u *url.URL) autohomePhotoRef {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		!strings.EqualFold(u.Hostname(), "car.autohome.com.cn") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return autohomePhotoRef{}
	}
	m := autohomePhotoPath.FindStringSubmatch(u.Path)
	if len(m) != 5 {
		return autohomePhotoRef{}
	}
	return autohomePhotoRef{kind: m[1], model: m[2], category: m[3], photo: m[4]}
}

func isAutohomePhoto(u *url.URL) bool { return autohomePhotoID(u).photo != "" }

func discoverAutohomePhoto(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isAutohomePhoto(root) {
		return result, errors.New("汽车之家图片地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || autohomePhotoID(final) != autohomePhotoID(root) || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("汽车之家图片已跳转或不可用")
	}
	return autohomeCollect(ctx, getter, final, doc)
}

func autohomeImage(page *url.URL, raw string) string {
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || !autohomePhotoHost.MatchString(strings.ToLower(u.Hostname())) ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		u.Path == "" || path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") {
		return ""
	}
	u.Scheme, u.Host = "https", strings.ToLower(u.Hostname())
	return u.String()
}

// shortcut: stop after 100 image pages until large galleries have configurable limits.
func autohomeCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	root := autohomePhotoID(page)
	if getter == nil || doc == nil || root.photo == "" {
		return result, errors.New("汽车之家图片页面无效")
	}
	type entry struct {
		page *url.URL
		doc  *html.Node
	}
	queue := []entry{{page: page, doc: doc}}
	seen := map[string]bool{root.photo: true}
	seenImages := map[string]bool{}
	rootSeries := ""
	var partial error
	warned := map[string]bool{}
	warn := func(message string) {
		if !warned[message] {
			warned[message] = true
			partial = errors.Join(partial, errors.New(message))
		}
	}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(partial, err)
		}
		current := queue[0]
		queue = queue[1:]
		if current.doc == nil {
			fetched, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: current.page.String(), depth: 1})
			if response != nil {
				response.Body.Close()
			}
			if ctx.Err() != nil {
				return result, errors.Join(partial, ctx.Err())
			}
			if err != nil || fetched == nil || autohomePhotoID(final) != autohomePhotoID(current.page) || !sameOrigin(current.page, final) {
				warn("部分汽车之家图片页读取失败，结果可能不完整")
				continue
			}
			current.doc, current.page = fetched, final
		}
		var main *html.Node
		var neighbors []string
		var model struct {
			Series   json.Number `json:"CarSeriesId"`
			Spec     json.Number `json:"CarSpec"`
			Category json.Number `json:"CarPicTypeId"`
			Photo    json.Number `json:"CarImgId"`
		}
		walkHTML(current.doc, func(n *html.Node) {
			if n.Type != html.ElementNode {
				return
			}
			if n.Data == "title" && result.Title == "" {
				result.Title = nodeText(n)
			}
			if attribute(n, "id") == "main" && main == nil {
				main = n
			}
			if n.Data == "script" {
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.TextNode {
						for _, match := range offer1688CodeMatches(c.Data, autohomeModel) {
							_ = json.NewDecoder(strings.NewReader(c.Data[match[1]:])).Decode(&model)
						}
						for _, m := range autohomeNeighbor.FindAllStringSubmatch(c.Data, -1) {
							neighbors = append(neighbors, m[1])
						}
					}
				}
			}
		})
		ref := autohomePhotoID(current.page)
		validModel := model.Series.String() != "" && model.Spec.String() == ref.model && model.Category.String() == ref.category && model.Photo.String() == ref.photo
		if model.Series.String() != "" && !validModel {
			warn("汽车之家图片与页面中的车型信息不匹配")
			continue
		}
		if current.page == page {
			if validModel {
				rootSeries = model.Series.String()
			} else {
				warn("汽车之家车型信息不完整，只采集当前车型的图片")
			}
		} else if (rootSeries != "" && (!validModel || model.Series.String() != rootSeries)) || (rootSeries == "" && ref.model != root.model) {
			warn("部分汽车之家图片与当前车系不匹配，结果可能不完整")
			continue
		}
		image := ""
		if main != nil {
			walkHTML(main, func(n *html.Node) {
				if n.Type == html.ElementNode && n.Data == "img" && attribute(n, "id") == "img" && image == "" {
					image = autohomeImage(current.page, attribute(n, "src"))
				}
			})
		}
		if image == "" {
			warn("部分汽车之家图片未加载，结果可能不完整")
		} else if !seenImages[image] {
			seenImages[image] = true
			item := Item{URL: image, RequireImage: true}
			if referer := imageReferer(current.page.String(), image); referer != "" {
				item.Headers = map[string]string{"Referer": referer}
			}
			result.Items = append(result.Items, item)
		}
		for _, raw := range neighbors {
			u, err := crawlURL(current.page, raw)
			ref := autohomePhotoID(u)
			if err != nil || !sameOrigin(current.page, u) || ref.kind != root.kind || ref.category != root.category || ref.photo == "" || seen[ref.photo] || (rootSeries == "" && ref.model != root.model) {
				continue
			}
			if len(seen) >= maxCrawlPages {
				warn("汽车之家图片达到 100 页上限，结果可能不完整")
				continue
			}
			seen[ref.photo] = true
			queue = append(queue, entry{page: u})
		}
	}
	return result, partial
}
