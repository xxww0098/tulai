package download

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// The Chinese site's current frontend resolves its historical URLs with these
// public GraphQL operations before loading the photo by its new ID.
const (
	fiveHundredChinaAPI            = "https://api-neo.500px.com.cn/graphql"
	fiveHundredChinaResolve        = `query resolveLegacyPhotoOrVideo($legacyId:String!){photo:getPhotoByLegacyId(legacyId:$legacyId){id} video:getVideoByLegacyId(legacyId:$legacyId){id}}`
	fiveHundredChinaPhoto          = `query getPhotoById($id:ID!){getPhotoById(id:$id){id title urls{size_600 size_1024 size_2048 size_4k}}}`
	fiveHundredChinaResolveGallery = `query resolveLegacyGallery($legacyId:String!){target:getGalleryByLegacyId(legacyId:$legacyId){id}}`
	fiveHundredChinaGallery        = `query GetGalleryById($id:ID!){getGalleryById(id:$id){id name itemCount}}`
	fiveHundredChinaGalleryItems   = `query PageGalleryItems($galleryId:ID!,$first:Int!,$after:String){pageGalleryItems(galleryId:$galleryId,first:$first,after:$after){edges{node{__typename ... on Photo{id title urls{size_600 size_1024 size_2048}} ... on PhotoGroup{id title}}} pageInfo{endCursor hasNextPage}}}`
	fiveHundredChinaGroupPhotos    = `query getPhotosByGroupId($groupId:ID!){getPhotosByGroupId(groupId:$groupId){id title urls{size_600 size_1024 size_2048 size_4k}}}`
	fiveHundredChinaMaxJSON        = 2 << 20
	fiveHundredChinaMaxItems       = 10000
)

type fiveHundredChinaRef struct {
	id     string
	legacy bool
}

func fiveHundredChinaHost(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "500px.com.cn", "www.500px.com.cn", "500px.me":
	default:
		return false
	}
	if p := u.Port(); p != "" && !(u.Scheme == "https" && p == "443") && !(u.Scheme == "http" && p == "80") {
		return false
	}
	return true
}

func fiveHundredChinaPageRef(u *url.URL) fiveHundredChinaRef {
	if !fiveHundredChinaHost(u) {
		return fiveHundredChinaRef{}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	var id string
	var legacy bool
	switch {
	case len(parts) == 3 && parts[0] == "community" && parts[1] == "photo-details":
		id, legacy = parts[2], true
	case len(parts) == 2 && parts[0] == "photo-details":
		id, legacy = parts[1], true
	case (len(parts) == 2 || len(parts) == 3) && parts[0] == "photo":
		id = parts[1]
	}
	if !fiveHundredIDPattern.MatchString(id) {
		return fiveHundredChinaRef{}
	}
	return fiveHundredChinaRef{id: id, legacy: legacy}
}

func is500pxChinaPhoto(u *url.URL) bool { return fiveHundredChinaPageRef(u).id != "" }

func fiveHundredChinaGalleryRef(u *url.URL) fiveHundredChinaRef {
	if !fiveHundredChinaHost(u) {
		return fiveHundredChinaRef{}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	var ref fiveHundredChinaRef
	switch {
	case len(parts) == 2 && parts[0] == "gallery":
		ref.id = parts[1]
	case (len(parts) == 3 || (len(parts) == 4 && parts[3] == "details")) && parts[0] == "community" && parts[1] == "set":
		ref.id, ref.legacy = parts[2], true
	}
	if !fiveHundredIDPattern.MatchString(ref.id) {
		return fiveHundredChinaRef{}
	}
	return ref
}

func is500pxChinaGallery(u *url.URL) bool { return fiveHundredChinaGalleryRef(u).id != "" }

func is500pxChinaPage(u *url.URL) bool {
	return is500pxChinaPhoto(u) || is500pxChinaGallery(u)
}

func is500pxChinaLegacyPage(u *url.URL) bool {
	return fiveHundredChinaPageRef(u).legacy || fiveHundredChinaGalleryRef(u).legacy
}

func discover500pxChina(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	if is500pxChinaPhoto(root) {
		return discover500pxChinaPhoto(ctx, getter, root)
	}
	return discover500pxChinaGallery(ctx, getter, root)
}

func fiveHundredChinaDeviceID() (string, error) {
	var device [16]byte
	if _, err := rand.Read(device[:]); err != nil {
		return "", fmt.Errorf("创建 500px 请求标识: %w", err)
	}
	device[6] = (device[6] & 0x0f) | 0x40
	device[8] = (device[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", device[:4], device[4:6], device[6:8], device[8:10], device[10:]), nil
}

func discover500pxChinaPhoto(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := fiveHundredChinaPageRef(root)
	if getter == nil || ref.id == "" {
		return result, errors.New("500px 中国站作品地址或下载客户端无效")
	}
	deviceID, err := fiveHundredChinaDeviceID()
	if err != nil {
		return result, err
	}
	var warnings []error
	if ref.legacy {
		var data struct {
			Photo *struct {
				ID string `json:"id"`
			} `json:"photo"`
			Video *struct {
				ID string `json:"id"`
			} `json:"video"`
		}
		partial, err := fiveHundredChinaQuery(ctx, getter, deviceID, "resolveLegacyPhotoOrVideo", fiveHundredChinaResolve, map[string]string{"legacyId": ref.id}, &data)
		if err != nil {
			return result, err
		}
		if partial {
			warnings = append(warnings, errors.New("500px 中国站旧作品查询部分失败"))
		}
		if data.Photo == nil || !fiveHundredIDPattern.MatchString(data.Photo.ID) {
			return result, errors.Join(append(warnings, errors.New("500px 中国站旧作品不可用或不是图片"))...)
		}
		ref.id = data.Photo.ID
	}
	var data struct {
		Photo *fiveHundredPhoto `json:"getPhotoById"`
	}
	partial, err := fiveHundredChinaQuery(ctx, getter, deviceID, "getPhotoById", fiveHundredChinaPhoto, map[string]string{"id": ref.id}, &data)
	if err != nil {
		return result, errors.Join(append(warnings, err)...)
	}
	if partial {
		warnings = append(warnings, errors.New("500px 中国站作品查询部分失败"))
	}
	photo := data.Photo
	if photo == nil || photo.ID != ref.id {
		return result, errors.Join(append(warnings, errors.New("500px 中国站作品不存在或与请求不匹配"))...)
	}
	result.Title = photo.Title
	if item, ok := fiveHundredChinaItem(root, *photo); ok {
		result.Items = append(result.Items, item)
		return result, errors.Join(warnings...)
	}
	return result, errors.Join(append(warnings, errors.New("500px 中国站未返回可信图片地址，可能需要登录或授权"))...)
}

func fiveHundredChinaItem(root *url.URL, photo fiveHundredPhoto) (Item, bool) {
	for _, raw := range []string{photo.URLs.Size4K, photo.URLs.Size2048, photo.URLs.Size1024, photo.URLs.Size600} {
		image, _, ok := fiveHundredImageURL(root, raw)
		if !ok {
			continue
		}
		item := Item{URL: image.String()}
		if referer := imageReferer(root.String(), item.URL); referer != "" {
			item.Headers = map[string]string{"Referer": referer}
		}
		return item, true
	}
	return Item{}, false
}

// discover500pxChinaGallery follows the site's public cursor pagination. Each
// PhotoGroup is expanded with the same query used by the official photo viewer.
func discover500pxChinaGallery(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := fiveHundredChinaGalleryRef(root)
	if getter == nil || ref.id == "" {
		return result, errors.New("500px 中国站相册地址或下载客户端无效")
	}
	deviceID, err := fiveHundredChinaDeviceID()
	if err != nil {
		return result, err
	}
	var warnings []error
	if ref.legacy {
		var data struct {
			Target *struct {
				ID string `json:"id"`
			} `json:"target"`
		}
		partial, err := fiveHundredChinaQuery(ctx, getter, deviceID, "resolveLegacyGallery", fiveHundredChinaResolveGallery, map[string]string{"legacyId": ref.id}, &data)
		if err != nil {
			return result, err
		}
		if partial {
			warnings = append(warnings, errors.New("500px 中国站旧相册查询部分失败"))
		}
		if data.Target == nil || !fiveHundredIDPattern.MatchString(data.Target.ID) {
			return result, errors.Join(append(warnings, errors.New("500px 中国站旧相册不可用"))...)
		}
		ref.id = data.Target.ID
	}
	var metadata struct {
		Gallery *struct{ ID, Name string } `json:"getGalleryById"`
	}
	partial, err := fiveHundredChinaQuery(ctx, getter, deviceID, "GetGalleryById", fiveHundredChinaGallery, map[string]string{"id": ref.id}, &metadata)
	if err != nil {
		return result, errors.Join(append(warnings, err)...)
	}
	if partial {
		warnings = append(warnings, errors.New("500px 中国站相册信息查询部分失败"))
	}
	if metadata.Gallery == nil || metadata.Gallery.ID != ref.id {
		return result, errors.Join(append(warnings, errors.New("500px 中国站相册不存在或与请求不匹配"))...)
	}
	result.Title = metadata.Gallery.Name
	seenIDs, seenURLs, seenCursors, seenGroups := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	var after string
	groupCount := 0
	groupsLimited, itemsLimited, badItem := false, false, false
	add := func(photo fiveHundredPhoto) {
		if !fiveHundredIDPattern.MatchString(photo.ID) {
			badItem = true
			return
		}
		if seenIDs[photo.ID] {
			return
		}
		item, ok := fiveHundredChinaItem(root, photo)
		if !ok {
			badItem = true
			return
		}
		seenIDs[photo.ID] = true
		if !seenURLs[item.URL] {
			if len(result.Items) >= fiveHundredChinaMaxItems {
				itemsLimited = true
				return
			}
			seenURLs[item.URL] = true
			result.Items = append(result.Items, item)
		}
	}
	for page := 0; page < maxCrawlPages; page++ {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(append(warnings, err)...)
		}
		var data struct {
			Page *struct {
				Edges []struct {
					Node *struct {
						Type string `json:"__typename"`
						fiveHundredPhoto
					} `json:"node"`
				} `json:"edges"`
				PageInfo *struct {
					EndCursor   string `json:"endCursor"`
					HasNextPage *bool  `json:"hasNextPage"`
				} `json:"pageInfo"`
			} `json:"pageGalleryItems"`
		}
		variables := map[string]any{"galleryId": ref.id, "first": 20}
		if after != "" {
			variables["after"] = after
		}
		partial, err := fiveHundredChinaQuery(ctx, getter, deviceID, "PageGalleryItems", fiveHundredChinaGalleryItems, variables, &data)
		if err != nil {
			return result, errors.Join(append(warnings, err)...)
		}
		if partial {
			warnings = append(warnings, errors.New("500px 中国站相册分页部分失败"))
		}
		if data.Page == nil {
			return result, errors.Join(append(warnings, errors.New("500px 中国站相册分页信息不完整"))...)
		}
		for _, edge := range data.Page.Edges {
			if err := ctx.Err(); err != nil {
				return result, errors.Join(append(warnings, err)...)
			}
			if edge.Node == nil {
				badItem = true
				continue
			}
			switch edge.Node.Type {
			case "Photo":
				add(edge.Node.fiveHundredPhoto)
			case "PhotoGroup":
				id := edge.Node.ID
				if !fiveHundredIDPattern.MatchString(id) {
					badItem = true
					continue
				}
				if seenGroups[id] {
					continue
				}
				seenGroups[id] = true
				if groupCount >= maxCrawlPages {
					groupsLimited = true
					continue
				}
				groupCount++
				var group struct {
					Photos []fiveHundredPhoto `json:"getPhotosByGroupId"`
				}
				groupPartial, err := fiveHundredChinaQuery(ctx, getter, deviceID, "getPhotosByGroupId", fiveHundredChinaGroupPhotos, map[string]string{"groupId": id}, &group)
				if ctx.Err() != nil {
					return result, errors.Join(append(warnings, ctx.Err())...)
				}
				if err != nil {
					warnings = append(warnings, fmt.Errorf("500px 中国站图组采集失败: %w", err))
					continue
				}
				if groupPartial || group.Photos == nil {
					badItem = true
				}
				for _, photo := range group.Photos {
					add(photo)
					if itemsLimited {
						break
					}
				}
			case "Video": // The gallery can contain videos, which have no image to download.
			default:
				badItem = true
			}
			if itemsLimited {
				return result, errors.Join(append(warnings, errors.New("500px 中国站图片超过采集上限，结果不完整"))...)
			}
		}
		if data.Page.PageInfo == nil || data.Page.PageInfo.HasNextPage == nil {
			return result, errors.Join(append(warnings, errors.New("500px 中国站相册分页信息不完整"))...)
		}
		if !*data.Page.PageInfo.HasNextPage {
			if badItem {
				warnings = append(warnings, errors.New("500px 中国站部分相册作品未能取得图片"))
			}
			if groupsLimited {
				warnings = append(warnings, errors.New("500px 中国站图组超过采集上限，结果不完整"))
			}
			return result, errors.Join(warnings...)
		}
		next := data.Page.PageInfo.EndCursor
		if next == "" || len(next) > 2048 || seenCursors[next] {
			return result, errors.Join(append(warnings, errors.New("500px 中国站相册分页游标无效，结果不完整"))...)
		}
		seenCursors[next] = true
		after = next
	}
	return result, errors.Join(append(warnings, errors.New("500px 中国站相册超过分页上限，结果不完整"))...)
}

func fiveHundredChinaQuery(ctx context.Context, getter Getter, deviceID, operation, query string, variables any, out any) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	encoded, err := json.Marshal(variables)
	if err != nil {
		return false, err
	}
	values := url.Values{"operationName": {operation}, "query": {query}, "variables": {string(encoded)}}
	endpoint := fiveHundredChinaAPI + "?" + values.Encode()
	// HTTPGetter normally follows redirects. Keep its proxy/transport but stop
	// redirects and disable its cookie jar for this cross-origin API request.
	if original, ok := getter.(*HTTPGetter); ok {
		copyGetter := *original
		copyClient := *original.client
		copyClient.Jar = nil
		copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		copyGetter.client = &copyClient
		getter = &copyGetter
	}
	response, err := getter.Get(ctx, endpoint, func(r *http.Request) {
		r.Header.Set("Accept", "application/json")
		r.Header.Set("Accept-Encoding", "identity")
		r.Header.Set("x-500px-device-id", deviceID)
		r.Header.Del("Cookie")
		r.Header.Del("Authorization")
		r.Header.Del("Referer")
	})
	if err != nil {
		return false, err
	}
	if response == nil || response.Body == nil {
		return false, errors.New("500px 中国站未返回接口响应")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 || response.FinalURL != endpoint {
		return false, errors.New("500px 中国站接口不可用或发生重定向")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, fiveHundredChinaMaxJSON+1))
	if err != nil {
		return false, err
	}
	if len(body) > fiveHundredChinaMaxJSON {
		return false, errors.New("500px 中国站接口响应过大")
	}
	var result struct {
		Data   json.RawMessage   `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(body, &result); err != nil || len(result.Data) == 0 || string(result.Data) == "null" {
		return false, errors.New("500px 中国站接口未返回作品数据，可能需要登录或验证")
	}
	if err := json.Unmarshal(result.Data, out); err != nil {
		return false, errors.New("500px 中国站作品数据格式无效")
	}
	return len(result.Errors) > 0, nil
}
