package download

import (
	"context"
	"errors"
	"fmt"
	"github.com/xxww0098/tulai/internal/camoufox"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

var xiaohongshuID = regexp.MustCompile(`(?i)^[0-9a-f]{24}$`)

func xiaohongshuNoteID(page *url.URL) (string, bool) {
	if page == nil || page.User != nil || page.Scheme != "https" {
		return "", false
	}
	host := strings.ToLower(page.Hostname())
	if host != "xiaohongshu.com" && !strings.HasSuffix(host, ".xiaohongshu.com") {
		return "", false
	}
	if port := page.Port(); port != "" && port != "443" {
		return "", false
	}
	parts := strings.Split(strings.Trim(page.Path, "/"), "/")
	var id string
	switch {
	case len(parts) == 2 && parts[0] == "explore":
		id = parts[1]
	case len(parts) == 3 && parts[0] == "discovery" && parts[1] == "item":
		id = parts[2]
	case len(parts) == 4 && parts[0] == "user" && parts[1] == "profile" && xiaohongshuID.MatchString(parts[2]):
		id = parts[3]
	}
	return strings.ToLower(id), xiaohongshuID.MatchString(id)
}

func xiaohongshuProfileID(page *url.URL) (string, bool) {
	if page == nil || page.User != nil || page.Scheme != "https" || !strings.EqualFold(page.Hostname(), "www.xiaohongshu.com") || (page.Port() != "" && page.Port() != "443") {
		return "", false
	}
	parts := strings.Split(strings.Trim(page.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "user" || parts[1] != "profile" || !xiaohongshuID.MatchString(parts[2]) {
		return "", false
	}
	return strings.ToLower(parts[2]), true
}

type xiaohongshuProfileLink struct {
	ID   string `json:"id"`
	Href string `json:"href"`
}

type xiaohongshuProfileSnapshot struct {
	Status    string                   `json:"status"`
	Title     string                   `json:"title"`
	Links     []xiaohongshuProfileLink `json:"links"`
	Truncated bool                     `json:"truncated"`
}

// shortcut: scan at most eight scrolls and 50 notes; larger profiles remain partial.
const (
	maxXiaohongshuProfileScrolls = 8
	maxXiaohongshuProfileNotes   = 50
	maxXiaohongshuProfileImages  = 10000
)

// The official UserPageFeeds component assigns this container to the posted-notes tab.
const xiaohongshuProfileScript = `(() => {
  if (location.href !== %q) return {status:"changed"};
  const tabs = new URL(location.href).searchParams.getAll("tab");
  if (tabs.length > 1 || (tabs.length && tabs[0] && tabs[0] !== "note")) return {status:"tab"};
  const page = document.getElementById("userPageContainer"), root = document.getElementById("userPostedFeeds");
  if (!page || !root || !page.contains(root)) return {status:"loading"};
  const cards = root.querySelectorAll(".note-item[data-note-id]");
  const links = [];
  for (let i = 0; i < cards.length && i < 200; i++) {
    const card = cards[i], anchor = card.querySelector("a.cover[href]");
    const id = card.getAttribute("data-note-id"), href = anchor && anchor.getAttribute("href");
    if (typeof id === "string" && id.length <= 64 && typeof href === "string" && href.length <= 4096) links.push({id,href});
  }
  return {status:"ok", title:(document.title || "").slice(0,512), links, truncated:cards.length > 200};
})()`

func xiaohongshuProfileNote(page *url.URL, profileID string, link xiaohongshuProfileLink) (*url.URL, bool) {
	if !xiaohongshuID.MatchString(link.ID) {
		return nil, false
	}
	u, err := crawlURL(page, link.Href)
	if err != nil || u.User != nil || !sameOrigin(page, u) || len(link.Href) > 4096 {
		return nil, false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "user" || parts[1] != "profile" || !strings.EqualFold(parts[2], profileID) || !strings.EqualFold(parts[3], link.ID) {
		return nil, false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, false
	}
	clean := url.Values{}
	for _, name := range []string{"xsec_token", "xsec_source"} {
		values := query[name]
		if len(values) > 1 || (len(values) == 1 && len(values[0]) > 2048) {
			return nil, false
		}
		if len(values) == 1 {
			clean.Set(name, values[0])
		}
	}
	u.RawQuery, u.ForceQuery = clean.Encode(), false
	return u, true
}

// captureXiaohongshuProfile uses a separate tab in the owned browser process.
func (s *RenderedSession) captureXiaohongshuProfile(ctx context.Context, page *url.URL, observedURL string) (Discovery, bool, error) {
	result := Discovery{IsPage: true}
	profileID, matched := xiaohongshuProfileID(page)
	if !matched {
		return result, false, nil
	}
	if tabs := page.Query()["tab"]; len(tabs) > 1 || (len(tabs) == 1 && tabs[0] != "" && tabs[0] != "note") {
		return result, true, errors.New("请切换到小红书主页的笔记标签后重试")
	}
	if err := ctx.Err(); err != nil {
		return result, true, err
	}
	checkCurrent := func() error {
		var currentRaw string
		if err := s.run(ctx, chromedp.Location(&currentRaw)); err != nil {
			return err
		}
		current, err := crawlURL(nil, currentRaw)
		currentID, ok := xiaohongshuProfileID(current)
		if err != nil || !ok || currentRaw != observedURL || currentID != profileID || !sameOrigin(page, current) {
			return errors.New("小红书主页已更改，已保留采集结果")
		}
		return nil
	}
	if err := checkCurrent(); err != nil {
		return result, true, err
	}
	child, closeChild, err := camoufox.NewPage(ctx, s.tab, false)
	if err != nil {
		return result, true, err
	}
	defer closeChild()
	run := func(ctx context.Context, actions ...camoufox.Action) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		op, cancel := context.WithTimeout(child, 45*time.Second)
		stop := context.AfterFunc(ctx, cancel)
		defer func() { stop(); cancel() }()
		err := camoufox.Run(op, actions...)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	var expected atomic.Value
	expected.Store("")
	camoufox.ListenTarget(child, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		u, err := crawlURL(nil, paused.Request.URL)
		allowed := err == nil && u.User == nil && sameOrigin(page, u)
		want := expected.Load().(string)
		if allowed && want == "" {
			id, ok := xiaohongshuProfileID(u)
			allowed = ok && id == profileID
		} else if allowed {
			id, ok := xiaohongshuNoteID(u)
			allowed = ok && id == want
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) == 4 && !strings.EqualFold(parts[2], profileID) {
				allowed = false
			}
		}
		go func() {
			var action chromedp.Action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			if allowed {
				action = fetch.ContinueRequest(paused.RequestID)
			}
			_ = camoufox.Run(child, action)
		}()
	})
	stopInit := context.AfterFunc(ctx, closeChild)
	initTimer := time.AfterFunc(45*time.Second, closeChild)
	initErr := camoufox.Run(child, fetch.Enable().WithPatterns([]*fetch.RequestPattern{{ResourceType: network.ResourceTypeDocument}}))
	stopInit()
	initTimer.Stop()
	if initErr != nil {
		if ctx.Err() != nil {
			return result, true, ctx.Err()
		}
		return result, true, errors.New("无法创建小红书主页采集标签")
	}
	if err := run(ctx, camoufox.Navigate(page.String())); err != nil {
		if ctx.Err() != nil {
			return result, true, ctx.Err()
		}
		return result, true, errors.New("小红书主页未加载，可能需要登录或安全验证")
	}
	var currentRaw string
	if err := run(ctx, chromedp.Location(&currentRaw)); err != nil {
		return result, true, err
	}
	current, err := crawlURL(nil, currentRaw)
	currentID, ok := xiaohongshuProfileID(current)
	if err != nil || !ok || currentID != profileID || !sameOrigin(page, current) {
		return result, true, errors.New("小红书主页跳转，请完成登录或安全验证后重试")
	}
	var urls []*url.URL
	seen := map[string]bool{}
	invalid, truncated := false, false
	for step := 0; step <= maxXiaohongshuProfileScrolls; step++ {
		var snapshot xiaohongshuProfileSnapshot
		for attempt := 0; attempt < 6; attempt++ {
			if err := run(ctx, chromedp.Evaluate(fmt.Sprintf(xiaohongshuProfileScript, currentRaw), &snapshot)); err != nil {
				return result, true, err
			}
			if snapshot.Status != "loading" {
				break
			}
			if attempt < 5 {
				if err := Wait(ctx, 250*time.Millisecond); err != nil {
					return result, true, err
				}
			}
		}
		switch snapshot.Status {
		case "changed":
			return result, true, errors.New("采集期间小红书主页已更改")
		case "tab":
			return result, true, errors.New("请切换到小红书主页的笔记标签后重试")
		case "ok":
		default:
			return result, true, errors.New("未加载小红书主页笔记列表，请确认已登录")
		}
		if result.Title == "" {
			result.Title = strings.TrimSpace(snapshot.Title)
		}
		truncated = truncated || snapshot.Truncated
		for _, link := range snapshot.Links {
			u, ok := xiaohongshuProfileNote(page, profileID, link)
			if !ok {
				invalid = true
				continue
			}
			id := strings.ToLower(link.ID)
			if seen[id] {
				continue
			}
			if len(urls) >= maxXiaohongshuProfileNotes {
				truncated = true
				break
			}
			seen[id] = true
			urls = append(urls, u)
		}
		if step == maxXiaohongshuProfileScrolls || len(urls) >= maxXiaohongshuProfileNotes {
			break
		}
		if err := run(ctx, chromedp.Evaluate(`window.scrollBy({top:window.innerHeight,behavior:"instant"})`, nil)); err != nil {
			return result, true, err
		}
		if err := Wait(ctx, 500*time.Millisecond); err != nil {
			return result, true, err
		}
	}
	var issues []error
	issues = append(issues, errors.New("小红书主页仅采集已加载的部分笔记，结果可能不完整"))
	if invalid {
		issues = append(issues, errors.New("部分小红书笔记链接与当前主页不匹配"))
	}
	if truncated {
		issues = append(issues, errors.New("小红书主页笔记超过扫描上限"))
	}
	if len(urls) == 0 {
		issues = append(issues, errors.New("主页没有可采集的笔记，可能需要登录或等待页面加载"))
	}
	for _, target := range urls {
		if err := ctx.Err(); err != nil {
			return result, true, err
		}
		if err := checkCurrent(); err != nil {
			return result, true, err
		}
		noteID, _ := xiaohongshuNoteID(target)
		expected.Store(noteID)
		if err := run(ctx, camoufox.Navigate(target.String())); err != nil {
			if ctx.Err() != nil {
				return result, true, ctx.Err()
			}
			issues = append(issues, errors.New("部分小红书笔记无法打开，可能需要登录或安全验证"))
			continue
		}
		if err := run(ctx, chromedp.Location(&currentRaw)); err != nil {
			return result, true, err
		}
		current, err := crawlURL(nil, currentRaw)
		currentNote, ok := xiaohongshuNoteID(current)
		if err != nil || !ok || currentNote != noteID || !sameOrigin(page, current) {
			issues = append(issues, errors.New("部分小红书笔记跳转到其他页面"))
			continue
		}
		snapshot, err := xiaohongshuReadNote(ctx, run, currentRaw, noteID)
		if ctx.Err() != nil {
			return result, true, ctx.Err()
		}
		if currentErr := checkCurrent(); currentErr != nil {
			return result, true, currentErr
		}
		if err != nil || !strings.EqualFold(snapshot.AuthorID, profileID) {
			issues = append(issues, errors.New("部分小红书笔记无法读取或作者与主页不匹配"))
			continue
		}
		note, err := xiaohongshuImages(current, snapshot)
		if err != nil {
			issues = append(issues, err)
		}
		for _, item := range note.Items {
			if len(result.Items) >= maxXiaohongshuProfileImages {
				issues = append(issues, errors.New("小红书主页图片超过采集上限"))
				return result, true, errors.Join(issues...)
			}
			item.Metadata = map[string]string{"album_title": firstNonEmpty(snapshot.Title, noteID), "album_id": noteID}
			result.Items = append(result.Items, item)
		}
	}
	return result, true, errors.Join(issues...)
}

type xiaohongshuSnapshot struct {
	Status    string                 `json:"status"`
	Title     string                 `json:"title"`
	AuthorID  string                 `json:"authorID"`
	Total     int                    `json:"total"`
	Truncated bool                   `json:"truncated"`
	Images    []xiaohongshuImageURLs `json:"images"`
}

type xiaohongshuImageURLs struct {
	Primary  []string `json:"primary"`
	Fallback []string `json:"fallback"`
}

// shortcut: inspect at most 512 images and eight variants per image; raise these
// bounds if Xiaohongshu publishes larger image notes.
const xiaohongshuNoteScript = `(() => {
  if (location.href !== %q) return {status:"changed"};
  const id = %q;
  const unwrap = v => v && typeof v === "object" && "_value" in v ? v._value : v;
  const state = window.__INITIAL_STATE__;
  const details = unwrap(state && state.note && state.note.noteDetailMap);
  const entry = details instanceof Map ? details.get(id) : details && details[id];
  const note = unwrap(entry && entry.note);
  const camel = unwrap(note && note.imageList), snake = unwrap(note && note.image_list);
  if (!Array.isArray(camel) && !Array.isArray(snake)) return {status:"loading"};
  const total = (Array.isArray(camel) ? camel.length : 0) + (Array.isArray(snake) ? snake.length : 0);
  const list = Array.isArray(camel) ? camel.slice(0, 512) : [];
  if (Array.isArray(snake)) list.push(...snake.slice(0, 512 - list.length));
  let truncated = total > list.length;
  const images = list.map(image => {
    const primary = [], fallback = [];
    const add = (target, value) => { if (typeof value === "string" && value.length > 0 && value.length <= 2048 && !target.includes(value)) target.push(value); };
    if (!image || typeof image !== "object") return {primary, fallback};
    if (Array.isArray(image.infoList)) {
      if (image.infoList.length > 8) truncated = true;
      for (const variant of image.infoList.slice(0, 8)) if (variant && (!variant.imageScene || variant.imageScene === "WB_DFT")) add(primary, variant.url);
    }
    if (Array.isArray(image.info_list)) {
      if (image.info_list.length > 8) truncated = true;
      for (const variant of image.info_list.slice(0, 8)) if (variant && variant.image_scene === "WB_DFT") add(primary, variant.url);
    }
    add(primary, image.url);
    add(fallback, image.urlDefault); add(fallback, image.url_default);
    add(fallback, image.urlPre); add(fallback, image.url_pre);
    if (primary.length > 8) truncated = true;
    return {primary:primary.slice(0, 8), fallback};
  });
  const author = unwrap(note && note.user);
  const authorID = typeof (author && author.userId) === "string" ? author.userId : typeof (author && author.user_id) === "string" ? author.user_id : "";
  return {status:"ok", title:typeof note.title === "string" ? note.title.slice(0, 512) : "", authorID, total, truncated, images};
})()`

// captureXiaohongshuNote reads only the current note's image fields from the
// logged-in page. It never exports the full page state or browser credentials.
func (s *RenderedSession) captureXiaohongshuNote(ctx context.Context, page *url.URL, observedURL string) (Discovery, bool, error) {
	var result Discovery
	id, matched := xiaohongshuNoteID(page)
	if !matched {
		return result, false, nil
	}
	result.IsPage = true
	snapshot, err := xiaohongshuReadNote(ctx, s.run, observedURL, id)
	if err != nil {
		return result, true, err
	}
	result, err = xiaohongshuImages(page, snapshot)
	return result, true, err
}

func xiaohongshuReadNote(ctx context.Context, run func(context.Context, ...camoufox.Action) error, observedURL, id string) (xiaohongshuSnapshot, error) {
	var snapshot xiaohongshuSnapshot
	for attempt := 0; attempt < 6; attempt++ {
		if err := run(ctx, chromedp.Evaluate(fmt.Sprintf(xiaohongshuNoteScript, observedURL, id), &snapshot)); err != nil {
			return snapshot, err
		}
		if snapshot.Status != "loading" {
			break
		}
		if attempt < 5 {
			if err := Wait(ctx, 250*time.Millisecond); err != nil {
				return snapshot, err
			}
		}
	}
	if snapshot.Status == "changed" {
		return snapshot, errors.New("采集期间小红书页面已更改，请重新打开目标笔记")
	}
	if snapshot.Status != "ok" {
		return snapshot, errors.New("未获取小红书笔记图集，请确认已登录且笔记已加载")
	}
	return snapshot, nil
}

func xiaohongshuImages(page *url.URL, snapshot xiaohongshuSnapshot) (Discovery, error) {
	result := Discovery{IsPage: true, Title: snapshot.Title}
	seen := map[string]bool{}
	missing := 0
	for _, choices := range snapshot.Images {
		found := false
		add := func(raw string) bool {
			u, err := crawlURL(nil, raw)
			if err != nil || u.User != nil || !xiaohongshuImageHost(u) {
				return false
			}
			image := u.String()
			if !seen[image] {
				seen[image] = true
				item := Item{URL: image}
				if referer := imageReferer(page.String(), image); referer != "" {
					item.Headers = map[string]string{"Referer": referer}
				}
				result.Items = append(result.Items, item)
			}
			return true
		}
		for _, raw := range choices.Primary {
			found = add(raw) || found
		}
		if !found {
			for _, raw := range choices.Fallback {
				if add(raw) {
					found = true
					break
				}
			}
		}
		if !found {
			missing++
		}
	}
	var problems []error
	if missing > 0 {
		problems = append(problems, fmt.Errorf("%d 张小红书笔记图片没有可用地址", missing))
	}
	if snapshot.Truncated || snapshot.Total > len(snapshot.Images) {
		problems = append(problems, errors.New("小红书笔记图片或图片变体超过扫描上限，结果可能不完整"))
	}
	if snapshot.Total == 0 {
		problems = append(problems, errors.New("小红书笔记没有可用图片"))
	}
	return result, errors.Join(problems...)
}

func xiaohongshuImageHost(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host != "xhscdn.com" && !strings.HasSuffix(host, ".xhscdn.com") && host != "xiaohongshu.com" && !strings.HasSuffix(host, ".xiaohongshu.com") {
		return false
	}
	return u.Port() == "" || (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80")
}
