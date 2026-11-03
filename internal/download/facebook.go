package download

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

var (
	facebookNumericID   = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	facebookOwner       = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	facebookLegacyPhoto = regexp.MustCompile(`^/([A-Za-z0-9._-]+)/photos/a\.[0-9]+/([1-9][0-9]{0,19})/?$`)
	facebookCDNHost     = regexp.MustCompile(`^scontent(?:[.-][a-z0-9-]+)*\.fbcdn\.net$`)
	facebookSetSuffix   = regexp.MustCompile(`^-?[0-9]{1,20}$`)
)

// The single-photo and album shapes are recognized to report that their
// content cannot yet be verified. Only a profile's loaded photo grid can be
// collected, and even those images are previews rather than originals.
func facebookRoute(u *url.URL) (kind, id string) {
	if u == nil || len(u.String()) > 8192 || u.User != nil || u.RawPath != "" ||
		(u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return "", ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "facebook.com" && host != "www.facebook.com" {
		return "", ""
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", ""
	}
	if u.Path == "/photo.php" || u.Path == "/photo" || u.Path == "/photo/" {
		if len(q["fbid"]) == 1 && facebookNumericID.MatchString(q.Get("fbid")) {
			return "photo", q.Get("fbid")
		}
		return "", ""
	}
	if m := facebookLegacyPhoto.FindStringSubmatch(u.Path); m != nil {
		return "photo", m[2]
	}
	if strings.TrimSuffix(u.Path, "/") == "/media/set" {
		if len(q["set"]) == 1 && strings.HasPrefix(q.Get("set"), "a.") && facebookNumericID.MatchString(strings.TrimPrefix(q.Get("set"), "a.")) {
			return "album", strings.TrimPrefix(q.Get("set"), "a.")
		}
		return "", ""
	}
	if u.Path == "/profile.php" {
		if len(q["id"]) == 1 && facebookNumericID.MatchString(q.Get("id")) && len(q["sk"]) == 1 &&
			(q.Get("sk") == "photos" || q.Get("sk") == "photos_albums" || q.Get("sk") == "photos_by") {
			return "profile", q.Get("id")
		}
		return "", ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || !facebookOwner.MatchString(parts[0]) || (parts[1] != "photos" && parts[1] != "photos_albums" && parts[1] != "photos_all") {
		return "", ""
	}
	if len(q["tab"]) > 1 || len(q["album_id"]) > 1 {
		return "", ""
	}
	if parts[1] != "photos" || q.Get("tab") != "" || q.Get("album_id") != "" {
		return "profile", strings.ToLower(parts[0])
	}
	return "grid", strings.ToLower(parts[0])
}

func isFacebookPage(u *url.URL) bool {
	kind, _ := facebookRoute(u)
	return kind != ""
}

func facebookSamePage(a, b *url.URL) bool {
	ak, ai := facebookRoute(a)
	bk, bi := facebookRoute(b)
	return ak != "" && ak == bk && ai == bi && sameOrigin(a, b)
}

// Accept only the exact image URL supplied by the page, including its signed
// query. The observed grid images are resized/cropped previews.
func facebookImage(raw string) string {
	if len(raw) > 8192 {
		return ""
	}
	u, err := crawlURL(nil, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") ||
		!facebookCDNHost.MatchString(strings.ToLower(u.Hostname())) || !strings.HasPrefix(u.Path, "/v/") ||
		strings.Contains(u.Path, "..") || strings.Contains(u.Path, "\\") || strings.Contains(u.Path, "//") {
		return ""
	}
	ext := strings.ToLower(path.Ext(u.Path))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
		return ""
	}
	base := strings.TrimSuffix(strings.ToLower(u.Path), ext)
	if !strings.HasSuffix(base, "_n") && !strings.HasSuffix(base, "_o") {
		return ""
	}
	return u.String()
}

type facebookTile struct {
	Link string `json:"link"`
	URL  string `json:"url"`
}

type facebookState struct {
	Location     string         `json:"location"`
	Canonical    string         `json:"canonical"`
	ProfileID    string         `json:"profileId"`
	IOSProfileID string         `json:"iosProfileId"`
	Blocked      bool           `json:"blocked"`
	Invalid      bool           `json:"invalid"`
	Truncated    bool           `json:"truncated"`
	Tiles        []facebookTile `json:"tiles"`
}

// Inspect the already-loaded grid and public profile metadata only. Do not
// read cookies, account details, storage or hidden GraphQL payloads. A visible
// password prompt stops collection before looking at any photo nodes.
const facebookStateScript = `(() => {
  const out={location:location.href.length<=8192?location.href:'',canonical:'',profileId:'',iosProfileId:'',blocked:false,invalid:false,truncated:false,tiles:[]};
  function shown(el){for(let p=el,n=0;p&&n++<64;p=p.parentElement){const s=getComputedStyle(p);if(p.hidden||p.getAttribute('aria-hidden')==='true'||s.display==='none'||s.visibility==='hidden'||s.visibility==='collapse'||s.opacity==='0')return false}return !!(el.offsetWidth||el.offsetHeight||el.getClientRects().length);}
  if([...document.querySelectorAll('input[type="password"]')].some(shown)){out.blocked=true;return out;}
  const canon=document.querySelector('meta[property="og:url"]')?.getAttribute('content')||'';
  const app=document.querySelector('meta[property="al:android:url"]')?.getAttribute('content')||'';
  const ios=document.querySelector('meta[property="al:ios:url"]')?.getAttribute('content')||'';
  out.canonical=canon.length<=8192?canon:'';
  const m=/^fb:\/\/profile\/([1-9][0-9]{0,19})$/.exec(app);if(m)out.profileId=m[1];
  const im=/^fb:\/\/profile\/([1-9][0-9]{0,19})$/.exec(ios);if(im)out.iosProfileId=im[1];
  if(!out.canonical||!out.profileId||out.profileId!==out.iosProfileId)return out;
  const nodes=document.querySelectorAll('a[href*="photo.php?"]');
  if(nodes.length>10000)out.truncated=true;
  let bytes=0;
  for(let i=0;i<Math.min(nodes.length,10000);i++){
    const a=nodes[i];if(!shown(a)||a.closest('[role="dialog"]'))continue;
    const imgs=a.querySelectorAll('img[src]');if(imgs.length!==1){out.invalid=true;continue}
    const link=a.getAttribute('href')||'',src=imgs[0].getAttribute('src')||'';
    if(!link||!src||link.length>8192||src.length>8192){out.invalid=true;continue}
    if((bytes+=link.length+src.length)>2097152){out.truncated=true;break}
    out.tiles.push({link,url:src});
  }
  return out;
})()`

func facebookCollectState(ctx context.Context, page *url.URL, state facebookState) (Discovery, error) {
	result := Discovery{IsPage: true, Title: "Facebook"}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	kind, owner := facebookRoute(page)
	current, err := crawlURL(nil, state.Location)
	if kind != "grid" || page.Scheme != "https" || err != nil || !facebookSamePage(page, current) {
		return result, errors.New("Facebook 页面已跳转或相册身份不匹配")
	}
	if state.Blocked {
		return result, errors.New("Facebook 需要登录后才能查看照片")
	}
	canonical, err := crawlURL(nil, state.Canonical)
	if err != nil || !sameOrigin(page, canonical) || !strings.EqualFold(strings.Trim(canonical.Path, "/"), owner) || !facebookNumericID.MatchString(state.ProfileID) || state.ProfileID != state.IOSProfileID {
		return result, errors.New("Facebook 照片所属主页尚无法确认")
	}
	seen := map[string]bool{}
	for _, tile := range state.Tiles {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		link, err := crawlURL(page, tile.Link)
		if err != nil || link.User != nil || !sameOrigin(page, link) || link.Path != "/photo.php" {
			continue
		}
		q, err := url.ParseQuery(link.RawQuery)
		if err != nil || len(q["fbid"]) != 1 || !facebookNumericID.MatchString(q.Get("fbid")) || len(q["set"]) != 1 {
			continue
		}
		set := strings.Split(q.Get("set"), ".")
		if len(set) != 3 || set[0] != "pb" || set[1] != state.ProfileID || !facebookSetSuffix.MatchString(set[2]) {
			continue
		}
		image := facebookImage(tile.URL)
		id := q.Get("fbid")
		if image == "" || seen[id] {
			continue
		}
		seen[id] = true
		item := Item{URL: image, Filename: "facebook-" + id, RequireImage: true, PublicOnly: true,
			Metadata: map[string]string{"facebook_photo_id": id, "facebook_profile_id": state.ProfileID, "image_kind": "preview"}}
		if ref := imageReferer(page.String(), image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	if len(result.Items) == 0 {
		return result, errors.New("Facebook 未找到身份可确认的照片预览；页面可能尚未加载")
	}
	return result, errors.New("Facebook 仅采集当前页面加载的预览图，原图及相册分页尚未确认，结果不完整")
}

func discoverFacebook(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !isFacebookPage(page) || getter == nil {
		return result, errors.New("Facebook 页面地址或下载客户端无效")
	}
	if page.Scheme != "https" {
		return result, errors.New("Facebook 请使用 HTTPS 地址")
	}
	return result, errors.New("Facebook 请在下载浏览器中打开并按需登录")
}

func facebookCapture(ctx context.Context, session *RenderedSession, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	kind, _ := facebookRoute(page)
	if kind == "" || page.Scheme != "https" || session == nil {
		return result, errors.New("Facebook 页面地址或下载浏览器无效")
	}
	if kind != "grid" {
		return result, errors.New("Facebook 单图或相册的图片归属与分页尚无法确认")
	}
	var state facebookState
	for attempt := 0; attempt < 16; attempt++ {
		state = facebookState{}
		if err := session.run(ctx, chromedp.Evaluate(facebookStateScript, &state)); err != nil {
			return result, fmt.Errorf("读取 Facebook 页面: %w", err)
		}
		current, err := crawlURL(nil, state.Location)
		if err != nil || !facebookSamePage(page, current) {
			return result, errors.New("Facebook 页面已跳转，请确认是否需要登录")
		}
		if state.Blocked || (state.Canonical != "" && state.ProfileID != "" && len(state.Tiles) > 0) {
			break
		}
		if attempt < 15 {
			if err := Wait(ctx, 250*time.Millisecond); err != nil {
				return result, err
			}
		}
	}
	return facebookCollectState(ctx, page, state)
}
