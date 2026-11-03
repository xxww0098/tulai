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
	houzzProjectPath = regexp.MustCompile(`^/hznb/projects/[^/]{1,400}-pj-vj~([0-9]{1,20})/?$`)
	houzzDigits      = regexp.MustCompile(`^[0-9]{1,20}$`)
	houzzImagePath   = regexp.MustCompile(`^/(?:fimgs|simgs)/([0-9a-fA-F]{16})_`)
)

// shortcut: use only image URLs actually published in the document; rendered
// or paginated pictures not present in the captured document remain partial.
const maxHouzzPhotos = 1000

func houzzProjectID(page *url.URL) string {
	if page == nil || page.User != nil || (page.Scheme != "https" && page.Scheme != "http") ||
		(page.Port() != "" && !(page.Scheme == "https" && page.Port() == "443") && !(page.Scheme == "http" && page.Port() == "80")) ||
		strings.Contains(strings.ToLower(page.EscapedPath()), "%2f") || strings.Contains(page.Path, "\\") || strings.Contains(page.Path, "..") {
		return ""
	}
	switch strings.ToLower(page.Hostname()) {
	case "houzz.com", "www.houzz.com":
	default:
		return ""
	}
	match := houzzProjectPath.FindStringSubmatch(page.Path)
	if match == nil {
		return ""
	}
	return match[1]
}
func isHouzzProject(page *url.URL) bool { return houzzProjectID(page) != "" }

// Only the exact project endpoint is accepted after a redirect.
func discoverHouzzProject(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := houzzProjectID(page)
	if getter == nil || id == "" {
		return result, errors.New("Houzz 项目地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, fmt.Errorf("读取 Houzz 项目失败，可能需要登录或稍后重试：%w", err)
	}
	if doc == nil || !sameOrigin(page, final) || houzzProjectID(final) != id {
		return result, errors.New("Houzz 项目已跳转，可能需要登录或验证")
	}
	return houzzProjectPage(ctx, doc, final)
}

type houzzProjectState struct {
	Data struct {
		PageName string `json:"pageName"`
		Stores   struct {
			Data struct {
				View struct {
					Data struct {
						Project json.Number `json:"current_project_id"`
						Author  json.Number `json:"profile_user_id"`
					} `json:"data"`
				} `json:"ViewProjectPageStore"`
				Projects struct {
					Data map[string]struct {
						ID         json.Number   `json:"id"`
						UserID     json.Number   `json:"userId"`
						Name       string        `json:"name"`
						PhotoIDs   []json.Number `json:"photoIds"`
						PhotoCount *int          `json:"photoCount"`
						CoverID    json.Number   `json:"coverSpaceId"`
					} `json:"data"`
				} `json:"ProjectStore"`
				Spaces struct {
					Data map[string]struct {
						ID        json.Number `json:"id"`
						ProjectID json.Number `json:"projectId"`
						Title     string      `json:"title"`
						ImageIDs  []string    `json:"imageIds"`
					} `json:"data"`
				} `json:"SpaceStore"`
				Images struct {
					Data map[string]struct {
						ExternalID string `json:"externalId"`
					} `json:"data"`
				} `json:"ImageStore"`
			} `json:"data"`
		} `json:"stores"`
	} `json:"data"`
}

type houzzPhoto struct {
	id, title string
	images    map[string]bool
}

// Accept only an HTTPS URL already present in the HTML or DOM and bound to a
// project image. Never derive a higher-resolution path or purchase URL.
func houzzDisplayedImage(raw string) (string, string, bool) {
	if len(raw) == 0 || len(raw) > 8192 {
		return "", "", false
	}
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "st.hzcdn.com") ||
		(u.Port() != "" && u.Port() != "443") || u.Fragment != "" || strings.Contains(u.Path, "..") || strings.Contains(u.Path, "\\") ||
		strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return "", "", false
	}
	match := houzzImagePath.FindStringSubmatch(u.Path)
	if match == nil {
		return "", "", false
	}
	return u.String(), strings.ToLower(match[1]), true
}

func houzzProjectPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := houzzProjectID(page)
	if id == "" || doc == nil {
		return result, errors.New("Houzz 项目页面无效")
	}
	var states []string
	var ogImage string
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "script" && attribute(n, "id") == "hz-ctx" && strings.EqualFold(attribute(n, "type"), "application/json") {
			if n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
				states = append(states, n.FirstChild.Data)
			} else {
				states = append(states, "")
			}
		}
		if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:image") && ogImage == "" {
			ogImage = attribute(n, "content")
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(states) != 1 || len(states[0]) == 0 || len(states[0]) > maxPageBytes {
		return result, errors.New("Houzz 项目数据缺失或超过大小限制")
	}
	var state houzzProjectState
	if json.Unmarshal([]byte(states[0]), &state) != nil || state.Data.PageName != "viewProject" {
		return result, errors.New("Houzz 项目数据格式无法识别")
	}
	stores := state.Data.Stores.Data
	project, ok := stores.Projects.Data[id]
	if !ok || stores.View.Data.Project.String() != id || project.ID.String() != id || !houzzDigits.MatchString(project.UserID.String()) ||
		project.UserID.String() != stores.View.Data.Author.String() || project.PhotoCount == nil || *project.PhotoCount < 0 || project.PhotoIDs == nil {
		return result, errors.New("Houzz 项目或作者归属无法确认")
	}
	result.Title = project.Name
	photos := make(map[string]houzzPhoto)
	order := make([]string, 0, len(project.PhotoIDs))
	bad := 0
	limited := false
	for _, photoID := range project.PhotoIDs {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		key := photoID.String()
		if !houzzDigits.MatchString(key) {
			bad++
			continue
		}
		if _, exists := photos[key]; exists {
			bad++
			continue
		}
		if len(order) >= maxHouzzPhotos {
			limited = true
			break
		}
		space, ok := stores.Spaces.Data[key]
		if !ok || space.ID.String() != key || space.ProjectID.String() != id || len(space.ImageIDs) == 0 {
			bad++
			continue
		}
		photo := houzzPhoto{id: key, title: space.Title, images: make(map[string]bool)}
		for _, ext := range space.ImageIDs {
			image, ok := stores.Images.Data[ext]
			if !ok || image.ExternalID != ext {
				bad++
				continue
			}
			// The URL parser further restricts this to a 16-digit hex image ID.
			photo.images[strings.ToLower(ext)] = true
		}
		if len(photo.images) == 0 {
			bad++
			continue
		}
		photos[key] = photo
		order = append(order, key)
	}
	selected := make(map[string]string)
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode || n.Data != "img" {
			return
		}
		key := attribute(n, "id")
		photo, ok := photos[key]
		if !ok {
			return
		}
		for _, attr := range []string{"src", "data-src", "data-lazy-src"} {
			image, ext, ok := houzzDisplayedImage(attribute(n, attr))
			if ok && photo.images[ext] {
				selected[key] = image
				break
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	cover := project.CoverID.String()
	if photo, ok := photos[cover]; ok && selected[cover] == "" {
		image, ext, ok := houzzDisplayedImage(ogImage)
		if ok && photo.images[ext] {
			selected[cover] = image
		}
	}
	seen := make(map[string]bool)
	for _, key := range order {
		image := selected[key]
		if image == "" || seen[image] {
			continue
		}
		seen[image] = true
		photo := photos[key]
		result.Items = append(result.Items, Item{URL: image, RequireImage: true, PublicOnly: true, Headers: map[string]string{"Referer": page.Scheme + "://" + page.Host + "/"},
			Metadata: map[string]string{"title": firstNonEmpty(photo.title, project.Name), "houzz_project_id": id, "houzz_photo_id": key, "houzz_author_id": project.UserID.String()}})
	}
	var issues []error
	if bad > 0 || limited {
		issues = append(issues, errors.New("部分 Houzz 图片的项目或来源无法确认"))
	}
	if len(result.Items) < *project.PhotoCount || len(project.PhotoIDs) < *project.PhotoCount {
		issues = append(issues, fmt.Errorf("Houzz 项目仅找到 %d/%d 张已公开展示的图片，结果可能不完整", len(result.Items), *project.PhotoCount))
	}
	if len(result.Items) == 0 {
		issues = append(issues, errors.New("未找到可确认属于该项目的公开展示图"))
	}
	issues = append(issues, errors.New("Houzz 仅采集页面提供的展示图，未获取可能受限的原图"))
	return result, errors.Join(issues...)
}
