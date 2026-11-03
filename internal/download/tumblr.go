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
	tumblrIDPattern   = regexp.MustCompile(`^[0-9]{5,24}$`)
	tumblrBlogPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	tumblrMediaHost   = regexp.MustCompile(`^[0-9]{1,3}\.media\.tumblr\.com$`)
)

type tumblrRef struct{ blog, id string }
type tumblrMedia struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}
type tumblrBlock struct {
	Type  string          `json:"type"`
	Media json.RawMessage `json:"media"`
}
type tumblrPost struct {
	ID             json.RawMessage `json:"id"`
	IDString       string          `json:"idString"`
	LegacyIDString string          `json:"id_string"`
	BlogName       string          `json:"blogName"`
	LegacyBlogName string          `json:"blog_name"`
	Blog           struct {
		Name string `json:"name"`
	} `json:"blog"`
	PostURL       string        `json:"postUrl"`
	LegacyPostURL string        `json:"post_url"`
	Summary       string        `json:"summary"`
	Title         string        `json:"title"`
	Content       []tumblrBlock `json:"content"`
	Trail         []struct {
		Content []tumblrBlock `json:"content"`
	} `json:"trail"`
	Photos []struct {
		Original tumblrMedia   `json:"original_size"`
		Alt      []tumblrMedia `json:"alt_sizes"`
	} `json:"photos"`
}

func tumblrPageRef(u *url.URL) tumblrRef {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return tumblrRef{}
	}
	port := u.Port()
	if port != "" && !(u.Scheme == "https" && port == "443") && !(u.Scheme == "http" && port == "80") {
		return tumblrRef{}
	}
	host := strings.ToLower(u.Hostname())
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	var blog, id string
	if host == "www.tumblr.com" || host == "tumblr.com" {
		if len(parts) < 2 || len(parts) > 3 {
			return tumblrRef{}
		}
		blog, id = parts[0], parts[1]
	} else if strings.HasSuffix(host, ".tumblr.com") {
		blog = strings.TrimSuffix(host, ".tumblr.com")
		if strings.Contains(blog, ".") || len(parts) < 2 || len(parts) > 3 || parts[0] != "post" {
			return tumblrRef{}
		}
		id = parts[1]
	} else {
		return tumblrRef{}
	}
	if !tumblrBlogPattern.MatchString(blog) || !tumblrIDPattern.MatchString(id) {
		return tumblrRef{}
	}
	return tumblrRef{blog: strings.ToLower(blog), id: id}
}

func isTumblrPost(u *url.URL) bool { return tumblrPageRef(u).id != "" }

func discoverTumblrPost(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	want := tumblrPageRef(root)
	if getter == nil || want.id == "" {
		return result, errors.New("Tumblr 帖子地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || tumblrPageRef(final) != want || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("Tumblr 帖子不可用或跳转到其他内容")
	}
	return tumblrPostPage(ctx, doc, final)
}

func tumblrMediaURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Scheme != "https" || !tumblrMediaHost.MatchString(strings.ToLower(u.Hostname())) || (u.Port() != "" && u.Port() != "443") || u.Path == "" || path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") {
		return "", false
	}
	u.Fragment = ""
	return u.String(), true
}

func tumblrRawID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	value = string(raw)
	if tumblrIDPattern.MatchString(value) {
		return value
	}
	return ""
}

func tumblrMatches(p tumblrPost, ref tumblrRef) bool {
	idFound := false
	for _, id := range []string{p.IDString, p.LegacyIDString, tumblrRawID(p.ID)} {
		if id == "" {
			continue
		}
		if id != ref.id {
			return false
		}
		idFound = true
	}
	if !idFound {
		return false
	}
	blogFound := false
	for _, blog := range []string{p.BlogName, p.LegacyBlogName, p.Blog.Name} {
		if blog == "" {
			continue
		}
		if !strings.EqualFold(blog, ref.blog) {
			return false
		}
		blogFound = true
	}
	for _, raw := range []string{p.PostURL, p.LegacyPostURL} {
		if u, err := url.Parse(raw); err == nil {
			actual := tumblrPageRef(u)
			if actual.id != "" && actual != ref {
				return false
			}
			if actual == ref {
				blogFound = true
			}
		}
	}
	return blogFound
}

func tumblrBestMedia(media []tumblrMedia) (string, bool) {
	best := ""
	var maxArea int64 = -1
	for _, m := range media {
		image, ok := tumblrMediaURL(m.URL)
		if !ok {
			continue
		}
		area := int64(0)
		if m.Width > 0 && m.Width <= 100000 && m.Height > 0 && m.Height <= 100000 {
			area = int64(m.Width) * int64(m.Height)
		}
		if best == "" || area > maxArea {
			best = image
			maxArea = area
		}
	}
	return best, best != ""
}

func tumblrBlockMedia(block tumblrBlock) (string, bool) {
	var list []tumblrMedia
	if json.Unmarshal(block.Media, &list) == nil {
		return tumblrBestMedia(list)
	}
	var single tumblrMedia
	if json.Unmarshal(block.Media, &single) == nil {
		return tumblrBestMedia([]tumblrMedia{single})
	}
	return "", false
}

// tumblrPostPage consumes only the requested post's image blocks and reblog
// trail, rather than images from other posts, ads or blog avatars on the page.
func tumblrPostPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := tumblrPageRef(page)
	if doc == nil || ref.id == "" {
		return result, errors.New("Tumblr 帖子页面无效")
	}
	var canonical, cover string
	var target *tumblrPost
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "meta" {
			switch strings.ToLower(firstNonEmpty(attribute(n, "property"), attribute(n, "name"))) {
			case "og:url":
				canonical = firstNonEmpty(canonical, attribute(n, "content"))
			case "og:image":
				cover = firstNonEmpty(cover, attribute(n, "content"))
			case "og:title":
				result.Title = firstNonEmpty(attribute(n, "content"), result.Title)
			}
		}
		if n.Data != "script" || attribute(n, "id") != "___INITIAL_STATE___" || target != nil {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.TextNode {
				continue
			}
			var roots map[string]json.RawMessage
			if json.Unmarshal([]byte(c.Data), &roots) != nil {
				return
			}
			// Tumblr's public post page places initialTimeline under PeeprRoute.
			// Iterate top-level route objects to handle the same schema on blog view.
			for _, raw := range roots {
				var route struct {
					InitialTimeline struct {
						Objects []tumblrPost `json:"objects"`
					} `json:"initialTimeline"`
					Response struct {
						Posts []tumblrPost `json:"posts"`
					} `json:"response"`
				}
				if json.Unmarshal(raw, &route) != nil {
					continue
				}
				for _, posts := range [][]tumblrPost{route.InitialTimeline.Objects, route.Response.Posts} {
					for i := range posts {
						candidate := posts[i]
						if tumblrMatches(candidate, ref) {
							target = &candidate
							return
						}
					}
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	seen := map[string]bool{}
	add := func(image string) {
		if seen[image] {
			return
		}
		seen[image] = true
		item := Item{URL: image}
		if referer := imageReferer(page.String(), image); referer != "" {
			item.Headers = map[string]string{"Referer": referer}
		}
		result.Items = append(result.Items, item)
	}
	if target != nil {
		result.Title = firstNonEmpty(target.Title, target.Summary, result.Title)
		partial := false
		blocks := func(items []tumblrBlock) {
			for _, block := range items {
				if block.Type != "image" {
					continue
				}
				if image, ok := tumblrBlockMedia(block); ok {
					add(image)
				} else {
					partial = true
				}
			}
		}
		blocks(target.Content)
		for _, trail := range target.Trail {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			blocks(trail.Content)
		}
		for _, photo := range target.Photos {
			media := append([]tumblrMedia{photo.Original}, photo.Alt...)
			if image, ok := tumblrBestMedia(media); ok {
				add(image)
			} else {
				partial = true
			}
		}
		if len(result.Items) == 0 {
			return result, errors.New("Tumblr 帖子未返回可信图片")
		}
		if partial {
			return result, errors.New("Tumblr 帖子部分图片未能取得")
		}
		return result, nil
	}
	u, err := crawlURL(page, canonical)
	if err != nil || tumblrPageRef(u) != ref {
		return result, errors.New("Tumblr 帖子数据尚未加载，可能需要登录或验证")
	}
	if image, ok := tumblrMediaURL(cover); ok {
		add(image)
		return result, errors.New("Tumblr 仅取得帖子封面，图片集可能不完整")
	}
	return result, errors.New("Tumblr 帖子未返回可信图片")
}
