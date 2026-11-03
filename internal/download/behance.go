package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var (
	behanceIDPattern      = regexp.MustCompile(`^[0-9]{1,24}$`)
	behanceProfilePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{2,100}$`)
	behanceProjectPattern = regexp.MustCompile(`(?:["']project["']|\bproject)\s*:\s*`)
	behanceActivePattern  = regexp.MustCompile(`(?:["']activeSection["']|\bactiveSection)\s*:\s*`)
)

type behanceImageSize struct {
	URL   string `json:"url"`
	Width int    `json:"width"`
	Type  string `json:"type"`
}

type behanceModule struct {
	Type       string          `json:"type"`
	Sizes      json.RawMessage `json:"sizes"`
	ImageSizes []struct {
		URL   string `json:"url"`
		Width int    `json:"width"`
	} `json:"imageSizes"`
}

type behanceProject struct {
	ID        json.RawMessage `json:"id"`
	Name      string          `json:"name"`
	URL       string          `json:"url"`
	IsPrivate bool            `json:"isPrivate"`
	Modules   []behanceModule `json:"modules"`
	Covers    struct {
		Original     string             `json:"original"`
		SizeOriginal string             `json:"size_original"`
		AllAvailable []behanceImageSize `json:"allAvailable"`
	} `json:"covers"`
}

type behanceWork struct {
	User struct {
		Username string `json:"username"`
	} `json:"user"`
	Projects        []behanceProject `json:"projects"`
	ProfileProjects []behanceProject `json:"profileProjects"`
	HasMore         *bool            `json:"hasMore"`
}

func behanceHost(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	if !strings.EqualFold(u.Hostname(), "behance.net") && !strings.EqualFold(u.Hostname(), "www.behance.net") {
		return false
	}
	p := u.Port()
	return p == "" || (u.Scheme == "https" && p == "443") || (u.Scheme == "http" && p == "80")
}

func behanceProjectID(u *url.URL) string {
	if !behanceHost(u) {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || len(parts) > 3 || parts[0] != "gallery" || !behanceIDPattern.MatchString(parts[1]) {
		return ""
	}
	return parts[1]
}

func isBehanceProject(u *url.URL) bool { return behanceProjectID(u) != "" }

func behanceProfileID(u *url.URL) string {
	if !behanceHost(u) {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 1 || !behanceProfilePattern.MatchString(parts[0]) {
		return ""
	}
	switch strings.ToLower(parts[0]) {
	case "about", "adobe", "blog", "careers", "collections", "community", "contact", "discover", "feed", "gallery", "galleries", "help", "hire", "jobs", "live", "login", "logout", "messages", "notifications", "portfolio", "privacy", "prosite", "search", "settings", "signup", "team", "terms", "user":
		return ""
	}
	return parts[0]
}

func isBehanceProfile(u *url.URL) bool { return behanceProfileID(u) != "" }

func discoverBehance(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	projectID, profile := behanceProjectID(root), behanceProfileID(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || (projectID == "" && profile == "") {
		return result, errors.New("Behance 页面或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || (projectID != "" && behanceProjectID(final) != projectID) || (profile != "" && !strings.EqualFold(behanceProfileID(final), profile)) || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("Behance 页面不可用或跳转到了其他作品或主页")
	}
	return behanceCollect(ctx, getter, final, doc)
}

// behanceCollect shares project extraction with the browser capture flow.
func behanceCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || doc == nil {
		return result, errors.New("Behance 页面或下载客户端无效")
	}
	if id := behanceProjectID(page); id != "" {
		return behanceProjectPage(ctx, doc, page, id)
	}
	if owner := behanceProfileID(page); owner != "" {
		return behanceProfilePage(ctx, getter, doc, page, owner)
	}
	return result, errors.New("Behance 页面无效")
}

func behanceProjectPage(ctx context.Context, doc *html.Node, page *url.URL, id string) (Discovery, error) {
	result := Discovery{IsPage: true}
	var project *behanceProject
	var canonical, cover string
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "meta" {
			switch strings.ToLower(attribute(n, "property")) {
			case "og:url":
				canonical = firstNonEmpty(canonical, attribute(n, "content"))
			case "og:image":
				cover = firstNonEmpty(cover, attribute(n, "content"))
			case "og:title":
				result.Title = firstNonEmpty(attribute(n, "content"), result.Title)
			}
		}
		if n.Data != "script" {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode {
				continue
			}
			text := child.Data
			for _, loc := range offer1688CodeMatches(text, behanceProjectPattern) {
				var candidate behanceProject
				if json.NewDecoder(strings.NewReader(text[loc[1]:])).Decode(&candidate) == nil && behanceRawID(candidate.ID) == id && (project == nil || len(candidate.Modules) > len(project.Modules)) {
					project = &candidate
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	seen := make(map[string]bool)
	var problems []error
	if project != nil && !project.IsPrivate {
		result.Title = firstNonEmpty(project.Name, result.Title)
		for _, module := range project.Modules {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			if module.Type != "" && !strings.EqualFold(module.Type, "image") {
				continue
			}
			image := behanceModuleImage(page, module)
			if image == nil {
				problems = append(problems, errors.New("Behance 作品图片地址不可用"))
				continue
			}
			behanceAddImage(&result, seen, page, image)
		}
		if len(result.Items) > 0 {
			return result, errors.Join(problems...)
		}
		if image := behanceCover(page, *project); image != nil {
			behanceAddImage(&result, seen, page, image)
		}
	}
	if len(result.Items) == 0 {
		if u, err := crawlURL(page, canonical); err == nil && behanceProjectID(u) == id {
			if image, ok := behanceImageURL(page, cover); ok {
				behanceAddImage(&result, seen, page, image)
			}
		}
	}
	return result, errors.Join(append(problems, errors.New("Behance 未提供完整作品图片，可能需要登录或完成验证"))...)
}

func behanceProfilePage(ctx context.Context, getter Getter, doc *html.Node, page *url.URL, owner string) (Discovery, error) {
	result := Discovery{IsPage: true}
	var work *behanceWork
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data != "script" || work != nil {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode {
				continue
			}
			text := child.Data
			for _, loc := range offer1688CodeMatches(text, behanceActivePattern) {
				var section struct {
					Work behanceWork `json:"work"`
				}
				if json.NewDecoder(strings.NewReader(text[loc[1]:])).Decode(&section) == nil && strings.EqualFold(section.Work.User.Username, owner) {
					work = &section.Work
					return
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if work == nil {
		return result, errors.New("Behance 主页作品数据未加载，可能需要登录或完成验证")
	}
	projects := work.ProfileProjects
	if projects == nil {
		projects = work.Projects
	}
	seen := make(map[string]bool)
	var problems []error
	if work.HasMore == nil || *work.HasMore {
		problems = append(problems, errors.New("Behance 主页还有未加载的作品，结果可能不完整"))
	}
	if len(projects) > maxCrawlPages {
		projects = projects[:maxCrawlPages]
		problems = append(problems, errors.New("Behance 主页已达到作品扫描上限"))
	}
	stop := false
	fallbacks := 0
	for _, project := range projects {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		id := behanceRawID(project.ID)
		projectURL, err := crawlURL(page, project.URL)
		if project.IsPrivate || id == "" || err != nil || behanceProjectID(projectURL) != id || (page.Scheme == "https" && projectURL.Scheme != "https") {
			problems = append(problems, errors.New("Behance 主页含无效或非公开作品"))
			continue
		}
		projectURL.RawQuery, projectURL.Fragment = "", ""
		var found Discovery
		var projectErr error
		if !stop {
			found, projectErr = discoverBehance(ctx, getter, projectURL)
			var status *HTTPStatusError
			if errors.As(projectErr, &status) && (status.StatusCode == 403 || status.StatusCode == 429) {
				stop = true
			}
		}
		for _, item := range found.Items {
			if !seen[item.URL] {
				seen[item.URL] = true
				result.Items = append(result.Items, item)
			}
		}
		if stop || projectErr != nil || len(found.Items) == 0 {
			fallbacks++
			if image := behanceCover(page, project); image != nil {
				behanceAddImage(&result, seen, page, image)
			}
		}
	}
	if fallbacks > 0 {
		problems = append(problems, fmt.Errorf("%d 个 Behance 作品未能完整读取，部分结果可能只有封面", fallbacks))
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, errors.Join(problems...)
}

func behanceRawID(raw json.RawMessage) string {
	var id string
	if json.Unmarshal(raw, &id) != nil {
		id = string(raw)
	}
	if !behanceIDPattern.MatchString(id) {
		return ""
	}
	return id
}

func behanceModuleImage(page *url.URL, module behanceModule) *url.URL {
	var sizes map[string]json.RawMessage
	var best *url.URL
	bestWidth := -1
	if json.Unmarshal(module.Sizes, &sizes) == nil {
		keys := make([]string, 0, len(sizes))
		for name := range sizes {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			raw := sizes[name]
			var candidate string
			width, _ := strconv.Atoi(name)
			if json.Unmarshal(raw, &candidate) != nil {
				var entry behanceImageSize
				if json.Unmarshal(raw, &entry) != nil {
					continue
				}
				candidate = entry.URL
				width = entry.Width
			}
			if name == "original" || name == "source" || name == "size_original" {
				width = 1 << 30
			}
			if image, ok := behanceImageURL(page, candidate); ok && width > bestWidth {
				best, bestWidth = image, width
			}
		}
	}
	for _, candidate := range module.ImageSizes {
		if image, ok := behanceImageURL(page, candidate.URL); ok && candidate.Width > bestWidth {
			best, bestWidth = image, candidate.Width
		}
	}
	return best
}

func behanceCover(page *url.URL, project behanceProject) *url.URL {
	for _, raw := range []string{project.Covers.Original, project.Covers.SizeOriginal} {
		if image, ok := behanceImageURL(page, raw); ok {
			return image
		}
	}
	var best *url.URL
	width := -1
	for _, candidate := range project.Covers.AllAvailable {
		image, ok := behanceImageURL(page, candidate.URL)
		if !ok {
			continue
		}
		rank := candidate.Width
		if strings.Contains(image.Path, "/projects/original/") {
			rank = 1 << 30
		} else if strings.Contains(image.Path, "/projects/original_webp/") {
			rank = (1 << 30) - 1
		}
		if rank > width {
			best, width = image, rank
		}
	}
	return best
}

func behanceImageURL(page *url.URL, raw string) (*url.URL, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Path == "" || path.Clean(parsed.Path) != parsed.Path || strings.Contains(parsed.Path, "//") {
		return nil, false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") || !strings.EqualFold(u.Hostname(), "mir-s3-cdn-cf.behance.net") || !(strings.HasPrefix(u.Path, "/project_modules/") || strings.HasPrefix(u.Path, "/projects/")) || !imagePath(u.Path) {
		return nil, false
	}
	return u, true
}

func behanceAddImage(result *Discovery, seen map[string]bool, page, image *url.URL) {
	if seen[image.String()] {
		return
	}
	seen[image.String()] = true
	item := Item{URL: image.String()}
	if ref := imageReferer(page.String(), item.URL); ref != "" {
		item.Headers = map[string]string{"Referer": ref}
	}
	result.Items = append(result.Items, item)
}
