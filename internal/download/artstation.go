package download

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	artstationProjectID = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)
	artstationUsername  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]{0,63}$`)
	artstationAlbumID   = regexp.MustCompile(`^[0-9]{1,20}$`)
	artstationCDN       = regexp.MustCompile(`^cdn[a-z0-9]*\.artstation\.com$`)
)

// shortcut: bound portfolio crawls and report when the limits truncate them.
const (
	maxArtstationPages    = 100
	maxArtstationProjects = 1000
	maxArtstationAssets   = 10000
)

type artstationRoute struct {
	kind, project, user, album string
}

func artstationRouteOf(u *url.URL) artstationRoute {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return artstationRoute{}
	}
	host := strings.ToLower(u.Hostname())
	mainHost := host == "www.artstation.com" || host == "artstation.com"
	var subdomain string
	if !mainHost {
		if !strings.HasSuffix(host, ".artstation.com") {
			return artstationRoute{}
		}
		subdomain = strings.TrimSuffix(host, ".artstation.com")
		if !artstationUsername.MatchString(subdomain) || strings.Contains(subdomain, "_") || subdomain == "magazine" || strings.HasPrefix(subdomain, "cdn") {
			return artstationRoute{}
		}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 2 && (parts[0] == "artwork" || parts[0] == "projects") && artstationProjectID.MatchString(parts[1]) {
		return artstationRoute{kind: "project", project: parts[1]}
	}
	user := subdomain
	if mainHost {
		if len(parts) < 1 || !artstationUsername.MatchString(parts[0]) {
			return artstationRoute{}
		}
		user, parts = parts[0], parts[1:]
		switch strings.ToLower(user) {
		case "about", "api", "artwork", "artists", "channels", "community", "contests", "explore", "feed", "jobs", "learning", "marketplace", "prints", "products", "projects", "search", "settings", "store", "users":
			return artstationRoute{}
		}
	} else if len(parts) == 1 && parts[0] == "projects" {
		parts = nil
	} else if len(parts) == 1 && parts[0] == "" {
		parts = nil
	}
	if len(parts) == 0 {
		return artstationRoute{kind: "portfolio", user: user, album: "all"}
	}
	if len(parts) == 2 && parts[0] == "albums" && (parts[1] == "all" || (artstationAlbumID.MatchString(parts[1]) && strings.Trim(parts[1], "0") != "")) {
		return artstationRoute{kind: "portfolio", user: user, album: parts[1]}
	}
	return artstationRoute{}
}

func isArtstationPage(u *url.URL) bool { return artstationRouteOf(u).kind != "" }

type artstationAsset struct {
	HasImage *bool  `json:"has_image"`
	ImageURL string `json:"image_url"`
	Title    string `json:"title"`
}

type artstationProject struct {
	HashID string          `json:"hash_id"`
	Title  string          `json:"title"`
	Assets json.RawMessage `json:"assets"`
}

type artstationList struct {
	Data []struct {
		HashID string `json:"hash_id"`
		Title  string `json:"title"`
	} `json:"data"`
	TotalCount *int `json:"total_count"`
}

// artstationJSON only requests the official API. The Getter owns redirects;
// its contract strips credentials on cross-origin hops.
func artstationJSON(ctx context.Context, getter Getter, path string, query url.Values, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if getter == nil {
		return errors.New("没有配置 ArtStation 下载客户端")
	}
	endpoint := &url.URL{Scheme: "https", Host: "www.artstation.com", Path: path, RawQuery: query.Encode()}
	response, err := getter.Get(ctx, endpoint.String(), func(r *http.Request) {
		r.Header.Set("Accept", "application/json, text/plain, */*")
		r.Header.Set("Accept-Encoding", "identity")
		r.Header.Set("Referer", "https://www.artstation.com/")
	})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &TransferError{Err: err}
	}
	defer response.Body.Close()
	final, err := crawlURL(nil, firstNonEmpty(response.FinalURL, endpoint.String()))
	if err != nil || final.User != nil || !sameOrigin(endpoint, final) || final.Path != endpoint.Path {
		return errors.New("ArtStation 接口跳转到其他页面，可能需要验证")
	}
	if response.StatusCode != http.StatusOK {
		return &HTTPStatusError{URL: final.String(), StatusCode: response.StatusCode, RetryAfter: ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())}
	}
	var reader io.Reader = io.LimitReader(response.Body, maxPageBytes+1)
	switch strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		decoded, err := gzip.NewReader(reader)
		if err != nil {
			return &TransferError{Err: err}
		}
		defer decoded.Close()
		reader = decoded
	case "deflate":
		decoded, err := zlib.NewReader(reader)
		if err != nil {
			return &TransferError{Err: err}
		}
		defer decoded.Close()
		reader = decoded
	default:
		return errors.New("不支持 ArtStation 接口的压缩格式")
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxPageBytes+1))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return &TransferError{Err: err}
	}
	if len(data) > maxPageBytes {
		return errors.New("ArtStation 接口响应超过 8 MiB 上限")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.New("ArtStation 接口数据无效，可能需要登录或安全验证")
	}
	return nil
}

func artstationImage(raw string) (string, bool) {
	u, err := crawlURL(nil, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !artstationCDN.MatchString(strings.ToLower(u.Hostname())) ||
		(u.Port() != "" && u.Port() != "443") || !strings.HasPrefix(u.Path, "/p/assets/") {
		return "", false
	}
	return u.String(), true
}

func discoverArtstation(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	route := artstationRouteOf(root)
	if route.kind == "" {
		return Discovery{IsPage: true}, errors.New("ArtStation 作品或相册地址无效")
	}
	if route.kind == "project" {
		return artstationProjectImages(ctx, getter, route.project)
	}
	return artstationPortfolio(ctx, getter, route)
}

func artstationProjectImages(ctx context.Context, getter Getter, id string) (Discovery, error) {
	result := Discovery{IsPage: true, Title: "ArtStation " + id}
	if !artstationProjectID.MatchString(id) {
		return result, errors.New("ArtStation 作品 ID 无效")
	}
	var project artstationProject
	if err := artstationJSON(ctx, getter, "/projects/"+id+".json", nil, &project); err != nil {
		return result, fmt.Errorf("读取 ArtStation 作品失败: %w", err)
	}
	if project.HashID != "" && project.HashID != id {
		return result, errors.New("ArtStation 作品 ID 不匹配")
	}
	if title := strings.TrimSpace(project.Title); title != "" {
		result.Title = title
	}
	if len(project.Assets) == 0 || bytes.Equal(bytes.TrimSpace(project.Assets), []byte("null")) {
		return result, errors.New("ArtStation 作品缺少素材列表")
	}
	var assets []artstationAsset
	if err := json.Unmarshal(project.Assets, &assets); err != nil {
		return result, errors.New("ArtStation 作品素材列表无效")
	}
	var problems []error
	if len(assets) > maxArtstationAssets {
		assets = assets[:maxArtstationAssets]
		problems = append(problems, fmt.Errorf("ArtStation 作品超过 %d 项素材，结果不完整", maxArtstationAssets))
	}
	seen := make(map[string]bool)
	invalid := 0
	for index, asset := range assets {
		if ctx.Err() != nil {
			return result, errors.Join(append(problems, ctx.Err())...)
		}
		if asset.HasImage == nil {
			invalid++
			continue
		}
		if !*asset.HasImage {
			continue
		}
		image, ok := artstationImage(asset.ImageURL)
		if !ok {
			invalid++
			continue
		}
		if seen[image] {
			continue
		}
		seen[image] = true
		result.Items = append(result.Items, Item{URL: image, RequireImage: true,
			Headers:  map[string]string{"Referer": "https://www.artstation.com/"},
			Metadata: map[string]string{"artstation_project": id, "page": strconv.Itoa(index + 1)}})
	}
	if invalid > 0 {
		problems = append(problems, fmt.Errorf("%d 项 ArtStation 图片素材无效，结果可能不完整", invalid))
	}
	if len(result.Items) == 0 {
		problems = append(problems, errors.New("ArtStation 作品未返回图片素材"))
	}
	return result, errors.Join(problems...)
}

func artstationPortfolio(ctx context.Context, getter Getter, route artstationRoute) (Discovery, error) {
	result := Discovery{IsPage: true, Title: "ArtStation " + route.user}
	if !artstationUsername.MatchString(route.user) || (route.album != "all" && (!artstationAlbumID.MatchString(route.album) || strings.Trim(route.album, "0") == "")) {
		return result, errors.New("ArtStation 用户或相册 ID 无效")
	}
	seenProjects, seenImages := make(map[string]bool), make(map[string]bool)
	var problems []error
	failed := 0
	var firstErr error
	finish := func(extra error) (Discovery, error) {
		all := append([]error(nil), problems...)
		if failed > 0 {
			all = append(all, fmt.Errorf("%d 个 ArtStation 作品读取失败或不完整: %w", failed, firstErr))
		}
		if len(result.Items) == 0 && extra == nil {
			all = append(all, errors.New("ArtStation 相册未返回可下载图片"))
		}
		return result, errors.Join(append(all, extra)...)
	}
	for page := 1; page <= maxArtstationPages; page++ {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		var list artstationList
		query := url.Values{"album_id": {route.album}, "page": {strconv.Itoa(page)}}
		if err := artstationJSON(ctx, getter, "/users/"+route.user+"/projects.json", query, &list); err != nil {
			return finish(fmt.Errorf("读取 ArtStation 相册失败，结果可能不完整: %w", err))
		}
		if list.TotalCount == nil || *list.TotalCount < 0 || list.Data == nil {
			return finish(errors.New("ArtStation 相册列表缺少有效数据或总数，结果不完整"))
		}
		if len(list.Data) == 0 {
			if len(seenProjects) < *list.TotalCount {
				return finish(errors.New("ArtStation 相册分页提前结束，结果不完整"))
			}
			return finish(nil)
		}
		newProjects := 0
		for _, item := range list.Data {
			if err := ctx.Err(); err != nil {
				return finish(err)
			}
			if !artstationProjectID.MatchString(item.HashID) {
				problems = append(problems, errors.New("ArtStation 相册含无效作品 ID，结果不完整"))
				continue
			}
			if seenProjects[item.HashID] {
				continue
			}
			if len(seenProjects) >= maxArtstationProjects {
				return finish(fmt.Errorf("ArtStation 相册超过 %d 个作品，结果不完整", maxArtstationProjects))
			}
			seenProjects[item.HashID] = true
			newProjects++
			work, err := artstationProjectImages(ctx, getter, item.HashID)
			for _, image := range work.Items {
				if seenImages[image.URL] {
					continue
				}
				if len(result.Items) >= maxArtstationAssets {
					return finish(fmt.Errorf("ArtStation 相册超过 %d 张图片，结果不完整", maxArtstationAssets))
				}
				seenImages[image.URL] = true
				image.Metadata["album_id"] = item.HashID
				image.Metadata["album_title"] = work.Title
				result.Items = append(result.Items, image)
			}
			if err != nil {
				failed++
				if firstErr == nil {
					firstErr = fmt.Errorf("作品 %s: %w", item.HashID, err)
				}
				if ctx.Err() != nil {
					return finish(ctx.Err())
				}
				var status *HTTPStatusError
				if errors.As(err, &status) && (status.StatusCode == 401 || status.StatusCode == 403 || status.StatusCode == 429) {
					return finish(err)
				}
			}
		}
		if newProjects == 0 {
			return finish(errors.New("ArtStation 相册分页重复，结果可能不完整"))
		}
		if len(seenProjects) >= *list.TotalCount {
			return finish(nil)
		}
	}
	return finish(fmt.Errorf("ArtStation 相册超过 %d 页，结果不完整", maxArtstationPages))
}
