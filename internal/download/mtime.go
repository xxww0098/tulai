package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
)

var (
	mtimeID        = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	mtimeImageHost = regexp.MustCompile(`^img[0-9]{1,2}\.mtime\.cn$`)
	mtimeCategory  = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
)

type mtimeRef struct {
	kind, id, category, imageID string
}

func mtimePageRef(u *url.URL) mtimeRef {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") || path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") {
		return mtimeRef{}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || len(parts) > 4 || !mtimeID.MatchString(parts[0]) {
		return mtimeRef{}
	}
	ref := mtimeRef{id: parts[0]}
	switch {
	case strings.EqualFold(u.Hostname(), "movie.mtime.com") && parts[1] == "posters_and_images":
		ref.kind = "movie"
	case strings.EqualFold(u.Hostname(), "people.mtime.com") && parts[1] == "images":
		ref.kind = "person"
	default:
		return mtimeRef{}
	}
	if len(parts) >= 3 {
		value := strings.TrimSuffix(strings.ToLower(parts[2]), ".html")
		if mtimeID.MatchString(value) {
			ref.imageID = value
		} else if mtimeCategory.MatchString(value) && value != "hot" && value != "new" {
			ref.category = value
		} else if value != "hot" && value != "new" {
			return mtimeRef{}
		}
	}
	if len(parts) == 4 && parts[3] != "hot.html" && parts[3] != "new.html" {
		return mtimeRef{}
	}
	return ref
}

func isMtimePage(u *url.URL) bool { return mtimePageRef(u).kind != "" }

type mtimeAPIResult struct {
	Code *int `json:"code"`
	Data *struct {
		Types []struct {
			Type  int `json:"type"`
			Count int `json:"imageCount"`
			Num   int `json:"num"`
		} `json:"imageTypes"`
		Images []struct {
			ID          int64  `json:"id"`
			Origin      string `json:"imageOrigin"`
			Type        string `json:"imageTypeUrlName"`
			Subtype     string `json:"imageSubtypeUrlName"`
			Description string `json:"imageTypeDes"`
		} `json:"images"`
	} `json:"data"`
}

func mtimeImageURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !mtimeImageHost.MatchString(strings.ToLower(u.Hostname())) ||
		(u.Port() != "" && u.Port() != "443") || !strings.HasPrefix(u.Path, "/pi/") || path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") {
		return "", false
	}
	u.Fragment = ""
	return u.String(), true
}

// shortcut: stop at 10,000 images and 8 MiB, until very large albums have a verified paginated API.
func discoverMtime(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := mtimePageRef(page)
	if getter == nil || ref.kind == "" {
		return result, errors.New("时光网图片地址或下载客户端无效")
	}
	endpoint := "https://front-gateway.mtime.com/library/" + ref.kind + "/imageAll.api?"
	if ref.kind == "movie" {
		endpoint += "tt=0&movieId=" + ref.id
		result.Title = "时光网电影 " + ref.id
	} else {
		endpoint += "personId=" + ref.id
		result.Title = "时光网影人 " + ref.id
	}
	resp, err := getter.Get(ctx, endpoint, func(r *http.Request) {
		r.Header.Set("Accept", "application/json")
		r.Header.Set("Accept-Encoding", "identity")
	})
	if err != nil {
		return result, fmt.Errorf("读取时光网图片失败: %w", err)
	}
	if resp == nil || resp.Body == nil {
		return result, errors.New("时光网图片接口没有响应")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || firstNonEmpty(resp.FinalURL, endpoint) != endpoint {
		return result, errors.New("时光网图片接口不可用或发生跳转")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return result, err
	}
	if len(body) > maxPageBytes {
		return result, errors.New("时光网图片数据超过大小限制")
	}
	var payload mtimeAPIResult
	if json.Unmarshal(body, &payload) != nil || payload.Code == nil || *payload.Code != 0 || payload.Data == nil || payload.Data.Images == nil {
		return result, errors.New("时光网没有返回有效的图片数据")
	}
	seenID, seenURL := make(map[int64]bool), make(map[string]bool)
	incomplete := false
	for _, image := range payload.Data.Images {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if ref.imageID != "" && strconv.FormatInt(image.ID, 10) != ref.imageID {
			continue
		}
		if ref.category != "" && !strings.EqualFold(image.Type, ref.category) && !strings.EqualFold(image.Subtype, ref.category) {
			continue
		}
		imageURL, ok := mtimeImageURL(image.Origin)
		if !ok || image.ID <= 0 {
			incomplete = true
			continue
		}
		if seenID[image.ID] || seenURL[imageURL] {
			continue
		}
		if len(result.Items) >= 10000 {
			return result, errors.New("时光网图片达到采集上限")
		}
		seenID[image.ID], seenURL[imageURL] = true, true
		item := Item{URL: imageURL, RequireImage: true, PublicOnly: true, Metadata: map[string]string{
			"mtime_id": ref.id, "mtime_image_id": strconv.FormatInt(image.ID, 10), "image_kind": image.Description,
		}}
		if referer := imageReferer(page.String(), imageURL); referer != "" {
			item.Headers = map[string]string{"Referer": referer}
		}
		result.Items = append(result.Items, item)
	}
	if len(result.Items) == 0 {
		return result, errors.New("时光网没有返回该页面的图片")
	}
	for _, t := range payload.Data.Types {
		if (ref.kind == "movie" && t.Type == -1 && t.Count > len(payload.Data.Images)) ||
			(ref.kind == "person" && t.Type == 0 && t.Num > len(payload.Data.Images)) {
			incomplete = true
		}
	}
	if incomplete {
		return result, errors.New("时光网图片可能不完整")
	}
	return result, nil
}
