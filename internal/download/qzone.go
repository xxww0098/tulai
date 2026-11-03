package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// qzonePageAuthority identifies QQ Space page hosts, not its picture CDNs.
func qzonePageAuthority(raw string) (string, bool) {
	i := strings.Index(raw, "://")
	if i < 0 || (!strings.EqualFold(raw[:i], "http") && !strings.EqualFold(raw[:i], "https")) {
		return "", false
	}
	authority := raw[i+3:]
	if end := strings.IndexAny(authority, "/?#"); end >= 0 {
		authority = authority[:end]
	}
	host := authority
	if at := strings.LastIndexByte(host, '@'); at >= 0 {
		host = host[at+1:]
	}
	if colon := strings.IndexByte(host, ':'); colon >= 0 {
		host = host[:colon]
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return authority, host == "i.qq.com" || host == "qzone.qq.com" || strings.HasSuffix(host, ".qzone.qq.com")
}

func qzoneCredentialKey(raw string, image bool) bool {
	key := raw
	for i := 0; i < 8; i++ {
		next, err := url.QueryUnescape(key)
		if err != nil {
			return false
		}
		if next == key {
			break
		}
		key = next
		if i == 7 {
			if deeper, err := url.QueryUnescape(key); err == nil && deeper != key {
				return true
			}
		}
	}
	switch strings.ToLower(key) {
	case "g_tk", "g_tk_new_20200303", "qzonetoken", "p_skey", "skey", "p_lskey", "lskey", "rv2", "access_token", "ptwebqq", "superkey", "pt4_token", "pt_local_token", "pt_login_sig", "ptcz", "zzpanelkey":
		return true
	case "token", "auth", "authorization", "cookie", "password":
		return !image
	}
	return false
}

// qzoneCleanFields removes credential parameters while keeping other fields.
// Encoded nested URLs containing credentials are removed as a whole.
func qzoneCleanFields(fields string) (string, bool) {
	return qzoneCleanFieldsDepth(fields, 0, false)
}

func qzoneCleanFieldsDepth(fields string, depth int, image bool) (string, bool) {
	var out strings.Builder
	changed := false
	separator := byte(0)
	for len(fields) > 0 {
		end := strings.IndexAny(fields, "&;?#")
		nextSeparator := byte(0)
		part := fields
		if end >= 0 {
			nextSeparator = fields[end]
			part = fields[:end]
			fields = fields[end+1:]
		} else {
			fields = ""
		}
		key, value, hasValue := strings.Cut(part, "=")
		remove := hasValue && qzoneCredentialKey(key, image)
		if !hasValue {
			if decoded, err := url.QueryUnescape(part); err == nil && decoded != part {
				if depth >= 8 {
					remove = true
				} else if clean, nestedChanged := qzoneCleanFieldsDepth(decoded, depth+1, image); nestedChanged {
					changed = true
					if clean == "" {
						remove = true
					} else {
						part = url.QueryEscape(clean)
					}
				}
			}
		}
		if hasValue && !remove {
			if decoded, err := url.QueryUnescape(value); err == nil && decoded != value && strings.ContainsAny(decoded, "?&;#=%") {
				if depth >= 8 {
					remove = true
				} else {
					_, remove = qzoneCleanFieldsDepth(decoded, depth+1, image)
				}
			}
		}
		if remove {
			changed = true
		} else if part != "" {
			if out.Len() > 0 && separator != 0 {
				out.WriteByte(separator)
			}
			out.WriteString(part)
		}
		if !remove || separator == 0 || nextSeparator == '?' || nextSeparator == '#' {
			separator = nextSeparator
		}
	}
	return out.String(), changed
}

// QzoneURLHasCredentials reports credentials in QQ page query/fragment fields.
// Normal image CDN URLs are deliberately outside this policy.
func QzoneURLHasCredentials(raw string) bool {
	return RedactQzoneCredentials(raw) != raw
}

// RedactQzoneCredentials removes authentication fields from QQ page URLs.
func RedactQzoneCredentials(raw string) string {
	authority, ok := qzonePageAuthority(raw)
	if !ok {
		return raw
	}
	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		pos := strings.Index(raw, "://") + 3
		raw = raw[:pos] + authority[at+1:] + raw[pos+len(authority):]
	}
	base, fragment, hasFragment := strings.Cut(raw, "#")
	path, query, hasQuery := strings.Cut(base, "?")
	if hasQuery {
		clean, changed := qzoneCleanFields(query)
		if !changed {
			clean = query
		}
		if clean != "" {
			path += "?" + clean
		} else if !changed {
			path += "?"
		}
	}
	if hasFragment {
		clean, changed := qzoneCleanFields(fragment)
		if !changed {
			clean = fragment
		}
		if clean != "" {
			path += "#" + clean
		} else if !changed {
			path += "#"
		}
	}
	return path
}

var (
	qzoneGroupID = regexp.MustCompile(`^[0-9]{1,20}$`)
	qzoneAlbumID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
	qzoneKey     = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
)

func qzoneGroupAlbumIDs(page *url.URL) (string, string, bool) {
	if page == nil || page.User != nil || page.Scheme != "https" || !strings.EqualFold(page.Hostname(), "h5.qzone.qq.com") || (page.Port() != "" && page.Port() != "443") || strings.Contains(page.EscapedPath(), "%") {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(page.Path, "/"), "/")
	if len(parts) != 5 && len(parts) != 6 {
		return "", "", false
	}
	if parts[0] != "groupphoto" || parts[1] != "inqq" || parts[2] != "photo" || (len(parts) == 6 && parts[5] != "groupphoto") || !qzoneGroupID.MatchString(parts[3]) || !qzoneAlbumID.MatchString(parts[4]) {
		return "", "", false
	}
	return parts[3], parts[4], true
}

type qzoneIdentity struct {
	Status string `json:"status"`
	GTk    string `json:"gtk"`
	Token  string `json:"token"`
	Uin    string `json:"uin"`
}

var errQzoneGroupLogin = errors.New("QQ 群相册需要登录，请在下载浏览器中完成登录后重试")

// The official group's ajax.js hashes p_skey/skey/rv2/access_token in the page.
// The POST session forwards browser cookies only to the exact page origin.
const qzoneIdentityScript = `(() => {
  if (location.href !== %q) return {status:"changed"};
  const p=window.params || {}, d=window.syncData || {};
  const expectedGroup=%q, expectedAlbum=%q;
  if (String(p.qunid || "") !== expectedGroup || (expectedAlbum && String(p.albumid || "") !== expectedAlbum)) return {status:"mismatch"};
  if (+d.code === -3000 || +d.ret === -3000) return {status:"login"};
  function cookie(k) { const m=document.cookie.match(new RegExp("(?:^|;\\s*)"+k+"=([^;]*)")); return m ? m[1] : ""; }
  function normalize(s) { s=String(s || "").replace(/^o/, ""); return /^\d{1,20}$/.test(s) ? s.replace(/^0+(?=\d)/, "") : ""; }
  const secret=cookie("p_skey") || cookie("skey") || cookie("rv2") || cookie("access_token");
  const uin=normalize(cookie("uin") || cookie("p_uin")), pageUin=normalize(p.loginUin || d.loginUin);
  if (!secret || secret.length > 8192 || !uin || uin === "0" || !pageUin || uin !== pageUin) return {status:"login"};
  let hash=5381; for (let i=0; i<secret.length; i++) hash += (hash << 5) + secret.charCodeAt(i);
  let token=typeof window.shine0callback === "string" ? window.shine0callback : "";
  if (token.length > 2048 || /[\x00-\x1f\x7f]/.test(token)) return {status:"invalid"};
  return {status:"ok", gtk:String(hash & 2147483647), token, uin};
})()`

func (s *RenderedSession) qzoneGroupIdentity(ctx context.Context, observedURL, groupID, albumID string) (qzoneIdentity, error) {
	var identity qzoneIdentity
	if err := s.run(ctx, chromedp.Evaluate(fmt.Sprintf(qzoneIdentityScript, observedURL, groupID, albumID), &identity)); err != nil {
		if ctx.Err() != nil {
			return identity, ctx.Err()
		}
		return identity, errors.New("读取 QQ 群相册页面失败，请重试")
	}
	switch identity.Status {
	case "login":
		return identity, errQzoneGroupLogin
	case "changed", "mismatch":
		return identity, errors.New("QQ 群相册页面或身份已变化，请重新打开相册")
	case "ok":
		return identity, nil
	default:
		return identity, errors.New("无法确认 QQ 群相册登录状态")
	}
}

func (s *RenderedSession) captureQzoneGroupAlbum(ctx context.Context, page *url.URL, observedURL string) (Discovery, bool, error) {
	result := Discovery{IsPage: true}
	groupID, albumID, matched := qzoneGroupAlbumIDs(page)
	if !matched {
		if page != nil && strings.EqualFold(page.Hostname(), "h5.qzone.qq.com") && strings.HasPrefix(page.Path, "/groupphoto/inqq/photo/") {
			return result, true, errors.New("QQ 群相册地址格式不受支持")
		}
		return result, false, nil
	}
	if QzoneURLHasCredentials(observedURL) {
		return result, true, errors.New("QQ 相册地址包含登录凭据，请移除后重试")
	}
	if err := ctx.Err(); err != nil {
		return result, true, err
	}
	identity, err := s.qzoneGroupIdentity(ctx, observedURL, groupID, albumID)
	if err != nil {
		return result, true, err
	}
	checkPage := func() error {
		var current string
		if err := s.run(ctx, chromedp.Location(&current)); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("读取 QQ 群相册页面失败")
		}
		if current != observedURL {
			return errors.New("QQ 群相册页面已变化，已保留采集结果")
		}
		return nil
	}
	result, err = collectQzoneGroupAlbum(ctx, s, groupID, albumID, identity, checkPage)
	return result, true, err
}

// The first-party front end flattens nested cursor objects using dotted keys.
func qzoneFlattenCursor(dst url.Values, prefix string, raw any, depth int) error {
	if depth > 8 || len(dst) >= 128 {
		return errors.New("QQ 相册分页游标过大")
	}
	switch value := raw.(type) {
	case string:
		if len(value) > 4096 {
			return errors.New("QQ 相册分页游标过大")
		}
		dst.Set(prefix, value)
	case json.Number:
		dst.Set(prefix, value.String())
	case bool:
		dst.Set(prefix, strconv.FormatBool(value))
	case nil:
		dst.Set(prefix, "null")
	case []any:
		for i, v := range value {
			if err := qzoneFlattenCursor(dst, fmt.Sprintf("%s._Array%d", prefix, i), v, depth+1); err != nil {
				return err
			}
		}
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if !qzoneKey.MatchString(key) {
				return errors.New("QQ 相册分页游标格式不支持")
			}
			if err := qzoneFlattenCursor(dst, prefix+"."+key, value[key], depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("QQ 相册分页游标格式不支持")
	}
	return nil
}

func qzoneImage(raw string) (string, bool) {
	if len(raw) == 0 || len(raw) > 8192 {
		return "", false
	}
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || (u.Port() != "" && !((u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443"))) {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if host != "qpic.cn" && !strings.HasSuffix(host, ".qpic.cn") && host != "photo.store.qq.com" && !strings.HasSuffix(host, ".photo.store.qq.com") {
		return "", false
	}
	if _, containsCredentials := qzoneCleanFieldsDepth(u.RawQuery, 0, true); containsCredentials {
		return "", false
	}
	if _, containsCredentials := qzoneCleanFieldsDepth(u.EscapedFragment(), 0, true); containsCredentials {
		return "", false
	}
	// This is the first-party page's fixPicUrl transformation.
	if u.Scheme == "http" {
		u.Scheme = "https"
		if u.Port() == "80" {
			u.Host = u.Hostname()
		}
	}
	u.Fragment, u.RawFragment = "", ""
	return u.String(), true
}

type qzonePhotoPage struct {
	AlbumInfo *struct {
		AlbumID json.RawMessage `json:"albumid"`
		Name    string          `json:"name"`
	} `json:"albuminfo"`
	Photos *[]struct {
		URLs      json.RawMessage `json:"photourl"`
		VideoFlag int             `json:"videoflag"`
	} `json:"photolist"`
	HasMore    *int            `json:"hasmore"`
	AttachInfo json.RawMessage `json:"attach_info"`
}

func qzoneScalarID(raw json.RawMessage) string {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

func qzonePhotoURLs(raw json.RawMessage) [2]string {
	var entries map[string]struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(raw, &entries) == nil {
		return [2]string{entries["0"].URL, entries["1"].URL}
	}
	var list []struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(raw, &list) == nil {
		var found [2]string
		for i := 0; i < len(found) && i < len(list); i++ {
			found[i] = list[i].URL
		}
		return found
	}
	return [2]string{}
}

const qzoneEndpoint = "https://h5.qzone.qq.com/groupphoto/inqq"

// Only the album itself is fetched; group lists and QQ Space use different flows.
func collectQzoneGroupAlbum(ctx context.Context, poster Poster, groupID, albumID string, identity qzoneIdentity, checkPage func() error) (Discovery, error) {
	result := Discovery{IsPage: true}
	if !qzoneGroupID.MatchString(groupID) || !qzoneAlbumID.MatchString(albumID) || !qzoneGroupID.MatchString(identity.Uin) || !qzoneGroupID.MatchString(identity.GTk) || poster == nil || checkPage == nil {
		return result, errors.New("QQ 群相册参数无效")
	}
	endpoint := qzoneEndpoint + "?" + url.Values{"g_tk": {identity.GTk}}.Encode()
	if identity.Token != "" {
		endpoint += "&" + url.Values{"qzonetoken": {identity.Token}}.Encode()
	}
	seenImages, seenCursors := map[string]bool{}, map[string]bool{}
	cursor := any("")
	var incomplete error
	for pageNumber := 0; pageNumber < maxCrawlPages; pageNumber++ {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(incomplete, err)
		}
		if err := checkPage(); err != nil {
			return result, errors.Join(incomplete, err)
		}
		form := url.Values{
			"cmd": {"qunGetPhotoList"}, "qunid": {groupID}, "albumid": {albumID}, "uin": {identity.Uin},
			"format": {"json"}, "inCharset": {"utf-8"}, "outCharset": {"utf-8"},
			"refresh_type": {strconv.Itoa(1 + min(pageNumber, 1))},
		}
		if err := qzoneFlattenCursor(form, "attach_info", cursor, 0); err != nil {
			return result, errors.Join(incomplete, err)
		}
		headers := http.Header{"Accept": {"application/json"}, "Content-Type": {"application/x-www-form-urlencoded; charset=utf-8"}, "X-Requested-With": {"XMLHttpRequest"}}
		response, err := poster.Post(ctx, endpoint, []byte(form.Encode()), headers)
		if err != nil {
			if ctx.Err() != nil {
				return result, errors.Join(incomplete, ctx.Err())
			}
			return result, errors.Join(incomplete, errors.New("QQ 群相册请求失败，已保留采集结果"))
		}
		if response == nil || response.Body == nil {
			return result, errors.Join(incomplete, errors.New("QQ 群相册响应无效"))
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			response.Body.Close()
			return result, errors.Join(incomplete, &HTTPStatusError{URL: qzoneEndpoint, StatusCode: response.StatusCode})
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxPageBytes+1))
		response.Body.Close()
		if readErr != nil || len(body) > maxPageBytes {
			if ctx.Err() != nil {
				return result, errors.Join(incomplete, ctx.Err())
			}
			return result, errors.Join(incomplete, errors.New("QQ 群相册响应读取失败或超过 8 MiB 上限"))
		}
		var envelope struct {
			Ret  *int            `json:"ret"`
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(body, &envelope) != nil || envelope.Ret == nil {
			return result, errors.Join(incomplete, errors.New("QQ 群相册响应格式不支持"))
		}
		if *envelope.Ret == -3000 {
			return result, errors.Join(incomplete, errQzoneGroupLogin)
		}
		if *envelope.Ret != 0 {
			return result, errors.Join(incomplete, errors.New("QQ 群相册未返回图片，可能需要登录或访问权限"))
		}
		var parsed qzonePhotoPage
		if json.Unmarshal(envelope.Data, &parsed) != nil || parsed.Photos == nil || (pageNumber == 0 && parsed.AlbumInfo == nil) {
			return result, errors.Join(incomplete, errors.New("QQ 群相册图片数据不完整"))
		}
		if parsed.AlbumInfo != nil {
			if qzoneScalarID(parsed.AlbumInfo.AlbumID) != albumID {
				return result, errors.Join(incomplete, errors.New("QQ 群相册响应的相册不匹配"))
			}
			if result.Title == "" {
				result.Title = strings.TrimSpace(parsed.AlbumInfo.Name)
			}
		}
		for _, photo := range *parsed.Photos {
			if err := ctx.Err(); err != nil {
				return result, errors.Join(incomplete, err)
			}
			if photo.VideoFlag == 1 {
				continue
			}
			var selected string
			for _, candidate := range qzonePhotoURLs(photo.URLs) {
				if candidate == "" {
					continue
				}
				clean, ok := qzoneImage(candidate)
				if !ok {
					incomplete = errors.New("部分 QQ 群相册图片来源或地址格式未获确认，采集可能不完整")
					continue
				}
				selected = clean
				break
			}
			if selected == "" {
				incomplete = errors.New("部分 QQ 群相册图片地址缺失或不受支持，采集可能不完整")
				continue
			}
			if !seenImages[selected] {
				seenImages[selected] = true
				result.Items = append(result.Items, Item{URL: selected, RequireImage: true, Headers: map[string]string{"Referer": "https://h5.qzone.qq.com/"}})
				if len(result.Items) >= 10000 {
					return result, errors.Join(incomplete, errors.New("QQ 群相册达到 10000 张图片上限，采集可能不完整"))
				}
			}
		}
		if parsed.HasMore == nil || (*parsed.HasMore != 0 && *parsed.HasMore != 1) {
			return result, errors.Join(incomplete, errors.New("QQ 群相册分页状态未知，采集可能不完整"))
		}
		if *parsed.HasMore == 0 {
			return result, incomplete
		}
		if len(parsed.AttachInfo) == 0 || string(parsed.AttachInfo) == "null" || len(parsed.AttachInfo) > 4096 {
			return result, errors.Join(incomplete, errors.New("QQ 群相册分页游标缺失，采集可能不完整"))
		}
		cursorKey := string(parsed.AttachInfo)
		if seenCursors[cursorKey] {
			return result, errors.Join(incomplete, errors.New("QQ 群相册分页游标重复，采集可能不完整"))
		}
		seenCursors[cursorKey] = true
		decoder := json.NewDecoder(strings.NewReader(cursorKey))
		decoder.UseNumber()
		if decoder.Decode(&cursor) != nil {
			return result, errors.Join(incomplete, errors.New("QQ 群相册分页游标格式不支持"))
		}
	}
	return result, errors.Join(incomplete, errors.New("QQ 群相册达到 100 页上限，采集可能不完整"))
}

// The official group album tab builds /album/<qunid>[/groupphoto].
func qzoneGroupAlbumListID(page *url.URL) (string, bool) {
	if page == nil || page.User != nil || page.Scheme != "https" || !strings.EqualFold(page.Hostname(), "h5.qzone.qq.com") || (page.Port() != "" && page.Port() != "443") || strings.Contains(page.EscapedPath(), "%") {
		return "", false
	}
	parts := strings.Split(strings.Trim(page.Path, "/"), "/")
	if (len(parts) != 4 && len(parts) != 5) || parts[0] != "groupphoto" || parts[1] != "inqq" || parts[2] != "album" || !qzoneGroupID.MatchString(parts[3]) || (len(parts) == 5 && parts[4] != "groupphoto") {
		return "", false
	}
	return parts[3], true
}

type qzoneGroupBoundedPoster struct {
	poster Poster
	check  func() error
	count  int
}

func (p *qzoneGroupBoundedPoster) Post(ctx context.Context, rawURL string, body []byte, headers http.Header) (*Response, error) {
	if p.count >= 500 {
		return nil, errors.New("QQ 群相册达到 500 次请求上限")
	}
	if err := p.check(); err != nil {
		return nil, err
	}
	p.count++
	return p.poster.Post(ctx, rawURL, body, headers)
}

func (s *RenderedSession) captureQzoneGroupList(ctx context.Context, page *url.URL, observedURL string) (Discovery, bool, error) {
	result := Discovery{IsPage: true}
	groupID, matched := qzoneGroupAlbumListID(page)
	if !matched {
		if page != nil && strings.EqualFold(page.Hostname(), "h5.qzone.qq.com") && strings.HasPrefix(page.Path, "/groupphoto/inqq/album/") {
			return result, true, errors.New("QQ 群相册列表地址格式不受支持")
		}
		return result, false, nil
	}
	if QzoneURLHasCredentials(observedURL) {
		return result, true, errors.New("QQ 群相册地址包含登录凭据，请移除后重试")
	}
	identity, err := s.qzoneGroupIdentity(ctx, observedURL, groupID, "")
	if err != nil {
		return result, true, err
	}
	checkPage := func() error {
		current, err := s.qzoneGroupIdentity(ctx, observedURL, groupID, "")
		if err != nil {
			return err
		}
		if current != identity {
			return errors.New("QQ 群相册登录状态已变化，已保留采集结果")
		}
		return nil
	}
	result, err = collectQzoneGroupList(ctx, s, groupID, identity, checkPage)
	return result, true, err
}

type qzoneGroupListPage struct {
	Entries *[]struct {
		SingleFeed json.RawMessage `json:"singlefeed"`
	} `json:"all_album_list_data"`
	HasMore    *int            `json:"hasmore"`
	AttachInfo json.RawMessage `json:"attach_info"`
}

func qzoneGroupListFeed(raw json.RawMessage) json.RawMessage {
	var indexed map[string]json.RawMessage
	if json.Unmarshal(raw, &indexed) == nil {
		return indexed["5"]
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil && len(list) > 5 {
		return list[5]
	}
	return nil
}

func collectQzoneGroupList(ctx context.Context, poster Poster, groupID string, identity qzoneIdentity, checkPage func() error) (Discovery, error) {
	result := Discovery{IsPage: true}
	if poster == nil || checkPage == nil || !qzoneGroupID.MatchString(groupID) || !qzoneGroupID.MatchString(identity.GTk) || !qzoneGroupID.MatchString(identity.Uin) {
		return result, errors.New("QQ 群相册列表参数无效")
	}
	bounded := &qzoneGroupBoundedPoster{poster: poster, check: checkPage}
	endpoint := qzoneEndpoint + "?" + url.Values{"g_tk": {identity.GTk}}.Encode()
	if identity.Token != "" {
		endpoint += "&" + url.Values{"qzonetoken": {identity.Token}}.Encode()
	}
	headers := http.Header{"Accept": {"application/json"}, "Content-Type": {"application/x-www-form-urlencoded; charset=utf-8"}, "X-Requested-With": {"XMLHttpRequest"}}
	seenAlbums, seenCursors := map[string]bool{}, map[string]bool{}
	var partial error
	warnings := map[string]bool{}
	warn := func(message string) {
		if !warnings[message] {
			warnings[message] = true
			partial = errors.Join(partial, errors.New(message))
		}
	}
	cursor := any("")
	for pageNumber := 0; pageNumber < maxCrawlPages; pageNumber++ {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(partial, err)
		}
		if err := checkPage(); err != nil {
			return result, errors.Join(partial, err)
		}
		form := url.Values{"cmd": {"qunGetAlbumList,qunGetMainPage"}, "qunid": {groupID}, "uin": {identity.Uin}, "format": {"json"}, "inCharset": {"utf-8"}, "outCharset": {"utf-8"}, "refresh_type": {strconv.Itoa(1 + min(pageNumber, 1))}}
		if err := qzoneFlattenCursor(form, "attach_info", cursor, 0); err != nil {
			return result, errors.Join(partial, err)
		}
		response, err := bounded.Post(ctx, endpoint, []byte(form.Encode()), headers)
		if err != nil {
			if ctx.Err() != nil {
				return result, errors.Join(partial, ctx.Err())
			}
			if bounded.count >= 500 {
				return result, errors.Join(partial, errors.New("QQ 群相册达到 500 次请求上限，采集可能不完整"))
			}
			if pageErr := checkPage(); pageErr != nil {
				return result, errors.Join(partial, pageErr)
			}
			return result, errors.Join(partial, errors.New("QQ 群相册列表请求失败，已保留采集结果"))
		}
		if response == nil || response.Body == nil {
			return result, errors.Join(partial, errors.New("QQ 群相册列表响应无效"))
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			response.Body.Close()
			return result, errors.Join(partial, &HTTPStatusError{URL: qzoneEndpoint, StatusCode: response.StatusCode})
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxPageBytes+1))
		response.Body.Close()
		if readErr != nil || len(body) > maxPageBytes {
			if ctx.Err() != nil {
				return result, errors.Join(partial, ctx.Err())
			}
			return result, errors.Join(partial, errors.New("QQ 群相册列表响应读取失败或超过 8 MiB 上限"))
		}
		var envelope struct {
			Ret  *int `json:"ret"`
			Data struct {
				List qzoneGroupListPage `json:"qunGetAlbumList"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &envelope) != nil || envelope.Ret == nil {
			return result, errors.Join(partial, errors.New("QQ 群相册列表响应格式不支持"))
		}
		if *envelope.Ret == -3000 {
			return result, errors.Join(partial, errQzoneGroupLogin)
		}
		if *envelope.Ret != 0 || envelope.Data.List.Entries == nil {
			return result, errors.Join(partial, errors.New("QQ 群相册列表数据不完整，可能需要登录或访问权限"))
		}
		for _, entry := range *envelope.Data.List.Entries {
			var album struct {
				ID      json.RawMessage `json:"albumid"`
				GroupID json.RawMessage `json:"qunid"`
				Name    string          `json:"albumname"`
				Count   json.RawMessage `json:"albumnum"`
			}
			if json.Unmarshal(qzoneGroupListFeed(entry.SingleFeed), &album) != nil {
				warn("部分 QQ 群相册列表项目格式不受支持")
				continue
			}
			albumID := qzoneScalarID(album.ID)
			if !qzoneAlbumID.MatchString(albumID) || len(album.Name) > 2048 || (len(album.GroupID) != 0 && qzoneScalarID(album.GroupID) != groupID) {
				warn("部分 QQ 群相册与当前群不匹配或格式不受支持")
				continue
			}
			if seenAlbums[albumID] {
				continue
			}
			seenAlbums[albumID] = true
			if len(seenAlbums) > 1000 {
				return result, errors.Join(partial, errors.New("QQ 群相册达到 1000 个相册上限，采集可能不完整"))
			}
			if count, ok := qzonePersonalInt(album.Count); ok && count == 0 {
				continue
			}
			images, albumErr := collectQzoneGroupAlbum(ctx, bounded, groupID, albumID, identity, checkPage)
			for _, item := range images.Items {
				item.Metadata = map[string]string{"album_id": albumID, "album_title": strings.TrimSpace(album.Name)}
				result.Items = append(result.Items, item)
				if len(result.Items) >= 10000 {
					return result, errors.Join(partial, errors.New("QQ 群相册达到 10000 张图片上限，采集可能不完整"))
				}
			}
			if albumErr != nil {
				if ctx.Err() != nil {
					return result, errors.Join(partial, ctx.Err())
				}
				if bounded.count >= 500 {
					return result, errors.Join(partial, errors.New("QQ 群相册达到 500 次请求上限，采集可能不完整"))
				}
				if errors.Is(albumErr, errQzoneGroupLogin) {
					return result, errors.Join(partial, albumErr)
				}
				if pageErr := checkPage(); pageErr != nil {
					return result, errors.Join(partial, pageErr)
				}
				warn("部分 QQ 群相册读取失败，采集可能不完整")
			}
		}
		list := envelope.Data.List
		if list.HasMore == nil || (*list.HasMore != 0 && *list.HasMore != 1) {
			return result, errors.Join(partial, errors.New("QQ 群相册列表分页状态未知，采集可能不完整"))
		}
		if *list.HasMore == 0 {
			return result, partial
		}
		if len(list.AttachInfo) == 0 || string(list.AttachInfo) == "null" || len(list.AttachInfo) > 4096 {
			return result, errors.Join(partial, errors.New("QQ 群相册列表分页游标缺失，采集可能不完整"))
		}
		cursorKey := string(list.AttachInfo)
		if seenCursors[cursorKey] {
			return result, errors.Join(partial, errors.New("QQ 群相册列表分页游标重复，采集可能不完整"))
		}
		seenCursors[cursorKey] = true
		decoder := json.NewDecoder(strings.NewReader(cursorKey))
		decoder.UseNumber()
		if decoder.Decode(&cursor) != nil {
			return result, errors.Join(partial, errors.New("QQ 群相册列表分页游标格式不支持"))
		}
	}
	return result, errors.Join(partial, errors.New("QQ 群相册列表达到 100 页上限，采集可能不完整"))
}

// The reference's QQ Space entry and sub_10006B85C's album-list page.
func qzonePersonalOwner(page *url.URL) (string, bool) {
	if page == nil || page.User != nil || page.Scheme != "https" || !strings.EqualFold(page.Hostname(), "user.qzone.qq.com") || (page.Port() != "" && page.Port() != "443") || strings.Contains(page.EscapedPath(), "%") {
		return "", false
	}
	parts := strings.Split(strings.Trim(page.Path, "/"), "/")
	if len(parts) < 1 || len(parts) > 2 || !qzoneGroupID.MatchString(parts[0]) || (len(parts) == 2 && parts[1] != "photo") {
		return "", false
	}
	return parts[0], true
}

func qzonePersonalAlbumDetail(page *url.URL) bool {
	if page == nil || page.User != nil || page.Scheme != "https" || !strings.EqualFold(page.Hostname(), "user.qzone.qq.com") || (page.Port() != "" && page.Port() != "443") || strings.Contains(page.EscapedPath(), "%") {
		return false
	}
	parts := strings.Split(strings.Trim(page.Path, "/"), "/")
	return len(parts) == 3 && qzoneGroupID.MatchString(parts[0]) && parts[1] == "photo" && qzoneAlbumID.MatchString(parts[2])
}

type qzonePersonalIdentity struct {
	uin string
	gtk string
}

func qzonePersonalCookieIdentity(cookies []*network.Cookie) (qzonePersonalIdentity, error) {
	var uin, skey string
	for _, c := range cookies {
		if c == nil {
			continue
		}
		if c.Name == "uin" && uin == "" {
			uin = c.Value
		}
		if c.Name == "skey" && skey == "" {
			skey = c.Value
		}
	}
	uin = strings.TrimLeft(strings.TrimPrefix(uin, "o"), "0")
	if !qzoneGroupID.MatchString(uin) || len(skey) == 0 || len(skey) > 8192 {
		return qzonePersonalIdentity{}, errors.New("QQ 空间需要登录，请在下载浏览器中完成登录后重试")
	}
	var hash uint32 = 5381
	for _, b := range []byte(skey) {
		hash += (hash << 5) + uint32(b)
	}
	return qzonePersonalIdentity{uin: uin, gtk: strconv.FormatUint(uint64(hash&0x7fffffff), 10)}, nil
}

// The reference uses GET here, so POST cannot be substituted. Keep the same
// exact-origin, no-redirect and credential-lifetime rules as RenderedSession.Post.
func (s *RenderedSession) qzonePersonalGet(ctx context.Context, page *url.URL, observedURL, endpoint string, params url.Values, expected qzonePersonalIdentity) ([]byte, error) {
	allowed := endpoint == "/proxy/domain/route.store.qq.com/GetRoute" ||
		endpoint == "/proxy/domain/photo.qzone.qq.com/fcgi-bin/fcg_list_album_v3" ||
		endpoint == "/proxy/domain/photo.qzone.qq.com/fcgi-bin/fcg_list_photo_v2" ||
		endpoint == "/proxy/domain/photo.qzone.qq.com/fcgi-bin/cgi_floatview_photo_list_v2"
	if !allowed || s == nil || s.tab == nil || s.getter == nil || QzoneURLHasCredentials(observedURL) {
		return nil, errors.New("QQ 空间请求地址无效")
	}
	target := &url.URL{Scheme: "https", Host: "user.qzone.qq.com", Path: endpoint, RawQuery: params.Encode()}
	s.mu.RLock()
	captured, pageIP, startIP := s.pageURL, s.pageIP, s.startIP
	s.mu.RUnlock()
	if captured == nil || !sameOrigin(captured, page) || !sameOrigin(page, target) {
		return nil, errors.New("QQ 空间请求只允许当前页面的同源代理")
	}
	requestCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.tab, cancel)
	defer func() { stop(); cancel() }()
	checkPage := func() error {
		var current string
		if err := s.run(requestCtx, chromedp.Location(&current)); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("读取 QQ 空间页面失败")
		}
		if current != observedURL {
			return errors.New("QQ 空间页面已变化，已保留采集结果")
		}
		return nil
	}
	if err := checkPage(); err != nil {
		return nil, err
	}
	cookies, err := s.cookies(requestCtx, target)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("读取 QQ 空间登录状态失败")
	}
	identity, err := qzonePersonalCookieIdentity(cookies)
	if err != nil || identity != expected {
		return nil, errors.New("QQ 空间登录状态已变化，请重新打开空间")
	}
	if err := checkPage(); err != nil {
		return nil, err
	}
	policy := safeDownloadPolicy{}
	if pageIP.IsValid() && pageIP == startIP && sameOrigin(s.start, page) && !isPublicDownloadIP(pageIP) {
		policy.allowedLocal = pageIP
	}
	request, err := http.NewRequestWithContext(context.WithValue(requestCtx, safeDownloadPolicyKey{}, policy), http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, errors.New("QQ 空间请求地址无效")
	}
	request.Header.Set("Accept", "application/json, text/plain, */*")
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", s.getter.userAgent)
	request.Header.Set("Referer", page.Scheme+"://"+page.Host+page.EscapedPath())
	for _, cookie := range cookies {
		request.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	}
	client := *s.getter.client
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("QQ 空间请求失败，已保留采集结果")
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return nil, errors.New("QQ 空间请求被重定向，采集可能不完整")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &HTTPStatusError{URL: target.Scheme + "://" + target.Host + target.Path, StatusCode: response.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPageBytes+1))
	if err != nil || len(body) > maxPageBytes {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("QQ 空间响应读取失败或超过 8 MiB 上限")
	}
	return body, nil
}

func (s *RenderedSession) captureQzonePersonal(ctx context.Context, page *url.URL, observedURL string) (Discovery, bool, error) {
	result := Discovery{IsPage: true}
	owner, matched := qzonePersonalOwner(page)
	if !matched {
		if qzonePersonalAlbumDetail(page) {
			return result, true, errors.New("QQ 空间单相册详情暂不支持，请打开相册列表后重试")
		}
		return result, false, nil
	}
	if QzoneURLHasCredentials(observedURL) {
		return result, true, errors.New("QQ 空间地址包含登录凭据，请移除后重试")
	}
	target := &url.URL{Scheme: "https", Host: "user.qzone.qq.com", Path: "/proxy/domain/photo.qzone.qq.com/fcgi-bin/fcg_list_album_v3"}
	cookies, err := s.cookies(ctx, target)
	if err != nil {
		if ctx.Err() != nil {
			return result, true, ctx.Err()
		}
		return result, true, errors.New("读取 QQ 空间登录状态失败")
	}
	identity, err := qzonePersonalCookieIdentity(cookies)
	if err != nil {
		return result, true, err
	}
	get := func(ctx context.Context, endpoint string, params url.Values) ([]byte, error) {
		return s.qzonePersonalGet(ctx, page, observedURL, endpoint, params, identity)
	}
	result, err = collectQzonePersonal(ctx, owner, identity, get)
	return result, true, err
}

// Only JSONP wrappers actually emitted by the reference endpoints are accepted.
func qzonePersonalJSON(body []byte, callbacks ...string) (json.RawMessage, error) {
	s := strings.TrimSpace(strings.TrimPrefix(string(body), "\ufeff"))
	for _, callback := range callbacks {
		prefix := callback + "("
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimSpace(strings.TrimPrefix(s, prefix))
			s = strings.TrimSpace(strings.TrimSuffix(s, ";"))
			if !strings.HasSuffix(s, ")") {
				return nil, errors.New("QQ 空间响应格式不支持")
			}
			s = strings.TrimSpace(strings.TrimSuffix(s, ")"))
			if json.Valid([]byte(s)) {
				return json.RawMessage(s), nil
			}
		}
	}
	return nil, errors.New("QQ 空间响应格式不支持")
}

type qzonePersonalGetFunc func(context.Context, string, url.Values) ([]byte, error)

type qzonePersonalAlbum struct {
	ID    string
	Title string
}

type qzonePersonalPhotos struct {
	Last   json.RawMessage `json:"last"`
	Photos []struct {
		Raw       string          `json:"raw"`
		Origin    string          `json:"origin"`
		OriginURL string          `json:"origin_url"`
		URL       string          `json:"url"`
		PicKey    string          `json:"picKey"`
		ShootTime json.RawMessage `json:"shootTime"`
		IsVideo   json.RawMessage `json:"is_video"`
	} `json:"photos"`
}

var errQzonePersonalLogin = errors.New("QQ 空间需要登录，请在下载浏览器中完成登录后重试")

func qzonePersonalInt(raw json.RawMessage) (int, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var str string
	if json.Unmarshal(raw, &str) != nil {
		str = string(raw)
	}
	n, err := strconv.Atoi(str)
	return n, err == nil && n >= 0
}

func qzonePersonalResponse(body []byte, callbacks ...string) (json.RawMessage, error) {
	raw, err := qzonePersonalJSON(body, callbacks...)
	if err != nil {
		return nil, err
	}
	var response struct {
		Code *int            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Code == nil {
		return nil, errors.New("QQ 空间响应格式不支持")
	}
	if *response.Code == -3000 {
		return nil, errQzonePersonalLogin
	}
	if *response.Code != 0 {
		return nil, errors.New("QQ 空间相册不可访问，可能需要权限或密码")
	}
	if len(response.Data) == 0 || string(response.Data) == "null" {
		return nil, errors.New("QQ 空间相册响应缺少数据")
	}
	return response.Data, nil
}

func qzonePersonalResponseMatches(raw json.RawMessage, owner, album string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	for _, name := range []string{"hostUin", "hostuin"} {
		if value, ok := fields[name]; ok && qzoneScalarID(value) != owner {
			return false
		}
	}
	if album != "" {
		for _, name := range []string{"albumid", "topicId"} {
			if value, ok := fields[name]; ok && qzoneScalarID(value) != album {
				return false
			}
		}
	}
	return true
}

func qzonePersonalAlbumEntries(raw json.RawMessage) ([]qzonePersonalAlbum, bool, error) {
	var data map[string]json.RawMessage
	if json.Unmarshal(raw, &data) != nil || data == nil {
		return nil, false, errors.New("QQ 空间相册列表格式不支持")
	}
	var albums []qzonePersonalAlbum
	incomplete := false
	var parseList func(json.RawMessage, int) error
	parseList = func(encoded json.RawMessage, depth int) error {
		if depth > 2 {
			return errors.New("QQ 空间相册列表层数过多")
		}
		var entries []json.RawMessage
		if json.Unmarshal(encoded, &entries) != nil {
			return nil
		}
		for _, entry := range entries {
			var item map[string]json.RawMessage
			if json.Unmarshal(entry, &item) != nil {
				incomplete = true
				continue
			}
			if nested := item["albumList"]; len(nested) != 0 {
				if err := parseList(nested, depth+1); err != nil {
					return err
				}
			}
			var id, title string
			_ = json.Unmarshal(item["id"], &id)
			_ = json.Unmarshal(item["name"], &title)
			if id == "" {
				if len(item["albumList"]) == 0 {
					incomplete = true
				}
				continue
			}
			if !qzoneAlbumID.MatchString(id) || len(title) > 2048 {
				incomplete = true
				continue
			}
			allowed := strings.TrimSpace(string(item["allowAccess"]))
			if allowed != "1" && allowed != `"1"` && allowed != "true" {
				incomplete = true
				continue
			}
			count, ok := qzonePersonalInt(item["total"])
			if !ok {
				incomplete = true
				continue
			}
			if count == 0 {
				continue
			}
			albums = append(albums, qzonePersonalAlbum{ID: id, Title: strings.TrimSpace(title)})
			if len(albums) > 1000 {
				return errors.New("QQ 空间相册数量超过上限")
			}
		}
		return nil
	}
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := parseList(data[key], 0); err != nil {
			return nil, false, err
		}
	}
	return albums, incomplete, nil
}

func collectQzonePersonal(ctx context.Context, owner string, identity qzonePersonalIdentity, get qzonePersonalGetFunc) (Discovery, error) {
	result := Discovery{IsPage: true}
	if !qzoneGroupID.MatchString(owner) || !qzoneGroupID.MatchString(identity.uin) || !qzoneGroupID.MatchString(identity.gtk) || get == nil {
		return result, errors.New("QQ 空间参数无效")
	}
	requestCount := 0
	request := func(endpoint string, values url.Values) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if requestCount >= 500 {
			return nil, errors.New("QQ 空间达到 500 次请求上限，采集可能不完整")
		}
		requestCount++
		return get(ctx, endpoint, values)
	}
	const routePath = "/proxy/domain/route.store.qq.com/GetRoute"
	const listPath = "/proxy/domain/photo.qzone.qq.com/fcgi-bin/fcg_list_album_v3"
	const photoPath = "/proxy/domain/photo.qzone.qq.com/fcgi-bin/fcg_list_photo_v2"
	const floatPath = "/proxy/domain/photo.qzone.qq.com/fcgi-bin/cgi_floatview_photo_list_v2"
	body, err := request(routePath, url.Values{"UIN": {owner}, "version": {"2"}})
	if err != nil {
		return result, err
	}
	raw, err := qzonePersonalJSON(body, "photoDomainNameCallback")
	if err != nil {
		return result, err
	}
	var route map[string]json.RawMessage
	var key string
	if json.Unmarshal(raw, &route) != nil || json.Unmarshal(route["default"], &key) != nil || !qzoneKey.MatchString(key) {
		return result, errors.New("QQ 空间相册路由格式不支持")
	}
	var domain struct {
		Album, Photo string `json:"-"`
	}
	var domains map[string]string
	if json.Unmarshal(route[key], &domains) != nil {
		return result, errors.New("QQ 空间相册路由缺失")
	}
	domain.Album, domain.Photo = domains["p"], domains["s"]
	if domain.Album != "photo.qzone.qq.com" || domain.Photo != "photo.qzone.qq.com" {
		return result, errors.New("QQ 空间相册路由来源未获确认，采集可能不完整")
	}
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	listQuery := url.Values{"g_tk": {identity.gtk}, "callback": {"shine0_Callback"}, "t": {stamp}, "hostUin": {owner}, "uin": {identity.uin}, "appid": {"4"}, "inCharset": {"utf-8"}, "outCharset": {"utf-8"}, "source": {"qzone"}, "plat": {"qzone"}, "format": {"jsonp"}, "notice": {"0"}, "filter": {"1"}, "handset": {"4"}, "pageNumModeSort": {"40"}, "pageNumModeClass": {"15"}, "needUserInfo": {"1"}, "idcNum": {"4"}, "callbackFun": {"shine0"}}
	var albums []qzonePersonalAlbum
	seenAlbum := map[string]bool{}
	seenPosition := map[int]bool{}
	var partial error
	warnings := map[string]bool{}
	warn := func(message string) {
		if !warnings[message] {
			warnings[message] = true
			partial = errors.Join(partial, errors.New(message))
		}
	}
	for pageNumber := 0; pageNumber < maxCrawlPages; pageNumber++ {
		body, err = request(listPath, listQuery)
		if err != nil {
			if ctx.Err() != nil || len(albums) == 0 {
				return result, errors.Join(partial, err)
			}
			partial = errors.Join(partial, err)
			break
		}
		data, err := qzonePersonalResponse(body, "shine0_Callback", "_Callback")
		if err != nil {
			if errors.Is(err, errQzonePersonalLogin) || len(albums) == 0 {
				return result, errors.Join(partial, err)
			}
			partial = errors.Join(partial, err)
			break
		}
		if !qzonePersonalResponseMatches(data, owner, "") {
			return result, errors.Join(partial, errors.New("QQ 空间相册列表与当前空间不匹配"))
		}
		pageAlbums, incomplete, err := qzonePersonalAlbumEntries(data)
		if err != nil {
			return result, errors.Join(partial, err)
		}
		if incomplete {
			warn("部分 QQ 空间相册不可访问或格式不受支持")
		}
		for _, album := range pageAlbums {
			if !seenAlbum[album.ID] {
				seenAlbum[album.ID] = true
				albums = append(albums, album)
			}
		}
		var info map[string]json.RawMessage
		_ = json.Unmarshal(data, &info)
		total, hasTotal := qzonePersonalInt(info["albumsInUser"])
		pos, hasPos := qzonePersonalInt(info["nextPageStart"])
		sortPos, hasSortPos := qzonePersonalInt(info["nextPageStartModeSort"])
		if sortPos > pos {
			pos = sortPos
		}
		if !hasTotal || (!hasPos && !hasSortPos) {
			warn("QQ 空间相册列表分页信息不完整")
			break
		}
		if total > 0 && len(pageAlbums) == 0 && !incomplete {
			warn("QQ 空间相册列表缺少可读取的相册")
		}
		if pos == 0 || pos >= total {
			break
		}
		mode, hasMode := qzonePersonalInt(info["mode"])
		order, hasOrder := qzonePersonalInt(info["ordertype"])
		if !hasMode || !hasOrder || seenPosition[pos] {
			warn("QQ 空间相册列表分页游标无效")
			break
		}
		seenPosition[pos] = true
		listQuery.Set("mode", strconv.Itoa(mode))
		listQuery.Set("sortOrder", strconv.Itoa(order))
		listQuery.Set("pageStart", strconv.Itoa(pos))
		listQuery.Set("pageNum", "40")
		if pageNumber == maxCrawlPages-1 {
			warn("QQ 空间相册列表达到 100 页上限")
		}
	}
	seenImages := map[string]bool{}
	for _, album := range albums {
		photoQuery := url.Values{"inCharset": {"gbk"}, "outCharset": {"gbk"}, "hostUin": {owner}, "notice": {"0"}, "callbackFun": {""}, "format": {"jsonp"}, "plat": {"qzone"}, "source": {"qzone"}, "appid": {"4"}, "uin": {"0"}, "albumid": {album.ID}, "t": {"0.9763213575351983"}, "g_tk": {"5381"}}
		body, err = request(photoPath, photoQuery)
		if err != nil {
			return result, errors.Join(partial, err)
		}
		data, err := qzonePersonalResponse(body, "_Callback")
		if err != nil {
			if errors.Is(err, errQzonePersonalLogin) {
				return result, errors.Join(partial, err)
			}
			warn("部分 QQ 空间相册无法读取")
			continue
		}
		if !qzonePersonalResponseMatches(data, owner, album.ID) {
			warn("部分 QQ 空间相册响应与当前空间或相册不匹配")
			continue
		}
		var first struct {
			Photos []struct {
				Lloc string `json:"lloc"`
			} `json:"photoList"`
		}
		if json.Unmarshal(data, &first) != nil || len(first.Photos) == 0 || !qzoneAlbumID.MatchString(first.Photos[0].Lloc) {
			warn("部分 QQ 空间相册的首图信息不完整")
			continue
		}
		floatQuery := url.Values{"g_tk": {identity.gtk}, "callback": {"viewer_Callback"}, "topicId": {album.ID}, "picKey": {first.Photos[0].Lloc}, "shootTime": {""}, "cmtOrder": {"1"}, "fupdate": {"1"}, "plat": {"qzone"}, "source": {"qzone"}, "cmtNum": {"10"}, "likeNum": {"5"}, "inCharset": {"utf-8"}, "outCharset": {"utf-8"}, "callbackFun": {"viewer"}, "offset": {"0"}, "number": {"15"}, "uin": {identity.uin}, "appid": {"4"}, "isFirst": {"1"}, "hostUin": {owner}, "showMode": {"1"}, "need_private_comment": {"1"}, "prevNum": {"9"}, "postNum": {"18"}}
		seenKeys := map[string]bool{}
		for photoPage := 0; photoPage < maxCrawlPages; photoPage++ {
			body, err = request(floatPath, floatQuery)
			if err != nil {
				return result, errors.Join(partial, err)
			}
			data, err = qzonePersonalResponse(body, "viewer_Callback", "_Callback")
			if err != nil {
				if errors.Is(err, errQzonePersonalLogin) {
					return result, errors.Join(partial, err)
				}
				warn("部分 QQ 空间相册图片无法读取")
				break
			}
			if !qzonePersonalResponseMatches(data, owner, album.ID) {
				warn("部分 QQ 空间图片响应与当前空间或相册不匹配")
				break
			}
			var photos qzonePersonalPhotos
			if json.Unmarshal(data, &photos) != nil || photos.Photos == nil {
				warn("QQ 空间图片列表格式不支持")
				break
			}
			for _, photo := range photos.Photos {
				flag := string(photo.IsVideo)
				if flag == "true" || flag == "1" {
					continue
				}
				if flag != "" && flag != "false" && flag != "0" && flag != "null" {
					warn("QQ 空间图片类型不明确")
					continue
				}
				var imageURL string
				for _, candidate := range []string{photo.Raw, photo.Origin, photo.OriginURL, photo.URL} {
					if candidate == "" {
						continue
					}
					clean, ok := qzoneImage(candidate)
					if !ok {
						warn("部分 QQ 空间图片来源未获确认")
						continue
					}
					imageURL = clean
					break
				}
				if imageURL == "" {
					warn("部分 QQ 空间图片地址不完整")
					continue
				}
				imageKey := album.ID + "\x00" + imageURL
				if !seenImages[imageKey] {
					seenImages[imageKey] = true
					result.Items = append(result.Items, Item{URL: imageURL, RequireImage: true, Headers: map[string]string{"Referer": "https://user.qzone.qq.com/"}, Metadata: map[string]string{"album_id": album.ID, "album_title": album.Title}})
					if len(result.Items) >= 10000 {
						return result, errors.Join(partial, errors.New("QQ 空间达到 10000 张图片上限，采集可能不完整"))
					}
				}
			}
			last, valid := qzonePersonalInt(photos.Last)
			if !valid || (last != 0 && last != 1) {
				warn("QQ 空间图片分页状态不完整")
				break
			}
			if last == 1 {
				break
			}
			if len(photos.Photos) == 0 {
				warn("QQ 空间图片分页游标缺失")
				break
			}
			lastPhoto := photos.Photos[len(photos.Photos)-1]
			if !qzoneAlbumID.MatchString(lastPhoto.PicKey) || seenKeys[lastPhoto.PicKey] {
				warn("QQ 空间图片分页游标无效")
				break
			}
			seenKeys[lastPhoto.PicKey] = true
			floatQuery.Set("picKey", lastPhoto.PicKey)
			floatQuery.Set("shootTime", qzoneScalarID(lastPhoto.ShootTime))
			offset, _ := strconv.Atoi(floatQuery.Get("offset"))
			floatQuery.Set("offset", strconv.Itoa(offset+len(photos.Photos)))
			floatQuery.Set("prevNum", "0")
			floatQuery.Set("postNum", "20")
			if photoPage == maxCrawlPages-1 {
				warn("QQ 空间单相册达到 100 页上限")
			}
		}
	}
	return result, partial
}
