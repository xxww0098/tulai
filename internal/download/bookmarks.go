package download

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DownloadBookmarkItem represents a single supported-site bookmark entry.
type DownloadBookmarkItem struct {
	Name string
	URL  string
}

// DownloadBookmarkFolder groups related download-site bookmarks in the browser bar.
type DownloadBookmarkFolder struct {
	Title string
	Items []DownloadBookmarkItem
}

// DownloadBookmarkFolders lists all currently supported image download sites
// grouped into compact bookmark-bar folders for the managed DownloadBrowser.
var DownloadBookmarkFolders = []DownloadBookmarkFolder{
	{
		Title: "图片搜索",
		Items: []DownloadBookmarkItem{
			{Name: "百度图片", URL: "https://image.baidu.com/"},
			{Name: "搜狗图片", URL: "https://pic.sogou.com/"},
			{Name: "360 图片", URL: "https://image.so.com/"},
			{Name: "Bing 图片", URL: "https://www.bing.com/images"},
			{Name: "Google 图片", URL: "https://images.google.com/"},
			{Name: "yande.re", URL: "https://yande.re/"},
			{Name: "Gelbooru", URL: "https://gelbooru.com/"},
			{Name: "昵图", URL: "https://www.nipic.com/"},
			{Name: "汇图", URL: "https://www.huitu.com/"},
			{Name: "Anime-Pictures", URL: "https://anime-pictures.net/"},
		},
	},
	{
		Title: "国内电商",
		Items: []DownloadBookmarkItem{
			{Name: "1688", URL: "https://www.1688.com/"},
			{Name: "淘宝", URL: "https://www.taobao.com/"},
			{Name: "天猫", URL: "https://www.tmall.com/"},
			{Name: "京东", URL: "https://www.jd.com/"},
			{Name: "拼多多", URL: "https://mobile.yangkeduo.com/"},
			{Name: "唯品会", URL: "https://www.vip.com/"},
			{Name: "当当", URL: "https://www.dangdang.com/"},
			{Name: "网易严选", URL: "https://you.163.com/"},
			{Name: "苏宁易购", URL: "https://www.suning.com/"},
			{Name: "得物", URL: "https://www.dewu.com/"},
			{Name: "闲鱼", URL: "https://www.goofish.com/"},
			{Name: "抖音商品", URL: "https://haohuo.jinritemai.com/"},
			{Name: "小米有品", URL: "https://www.xiaomiyoupin.com/"},
			{Name: "义乌购", URL: "https://www.yiwugo.com/"},
			{Name: "微店", URL: "https://weidian.com/"},
			{Name: "有赞", URL: "https://www.youzan.com/"},
			{Name: "百度爱采购", URL: "https://b2b.baidu.com/"},
			{Name: "蘑菇街", URL: "https://www.mogujie.com/"},
			{Name: "网商园", URL: "https://www.wsy.com/"},
			{Name: "搜款网", URL: "https://www.vvic.com/"},
			{Name: "洋码头", URL: "https://www.ymatou.com/"},
			{Name: "母婴天下", URL: "https://wybbej.mytx.cn/"},
			{Name: "政采云", URL: "https://www.zcygov.cn/"},
		},
	},
	{
		Title: "海外电商",
		Items: []DownloadBookmarkItem{
			{Name: "亚马逊 Amazon", URL: "https://www.amazon.com/"},
			{Name: "eBay 易贝", URL: "https://www.ebay.com/"},
			{Name: "Temu", URL: "https://www.temu.com/"},
			{Name: "SHEIN", URL: "https://www.shein.com/"},
			{Name: "速卖通 AliExpress", URL: "https://www.aliexpress.com/"},
			{Name: "Alibaba 国际站", URL: "https://www.alibaba.com/"},
			{Name: "Shopee", URL: "https://shopee.com/"},
			{Name: "Lazada", URL: "https://www.lazada.com/"},
			{Name: "Etsy", URL: "https://www.etsy.com/"},
			{Name: "Ozon", URL: "https://www.ozon.ru/"},
			{Name: "日本雅虎商城", URL: "https://shopping.yahoo.co.jp/"},
			{Name: "敦煌网", URL: "https://www.dhgate.com/"},
			{Name: "中国制造网", URL: "https://www.made-in-china.com/"},
			{Name: "兰亭 LightInTheBox", URL: "https://www.lightinthebox.com/"},
			{Name: "JJ's House", URL: "https://www.jjshouse.com/"},
			{Name: "Spoonflower", URL: "https://www.spoonflower.com/"},
			{Name: "Wellco", URL: "https://www.wellco.ca/"},
			{Name: "无印良品 MUJI", URL: "https://www.muji.com/"},
		},
	},
	{
		Title: "社交图文",
		Items: []DownloadBookmarkItem{
			{Name: "小红书", URL: "https://www.xiaohongshu.com/"},
			{Name: "微博", URL: "https://weibo.com/"},
			{Name: "哔哩哔哩 B站", URL: "https://www.bilibili.com/"},
			{Name: "抖音图文", URL: "https://www.douyin.com/"},
			{Name: "快手", URL: "https://www.kuaishou.com/"},
			{Name: "TikTok", URL: "https://www.tiktok.com/"},
			{Name: "X / Twitter", URL: "https://x.com/"},
			{Name: "Facebook", URL: "https://www.facebook.com/"},
			{Name: "Instagram", URL: "https://www.instagram.com/"},
			{Name: "Tumblr", URL: "https://www.tumblr.com/"},
			{Name: "知乎", URL: "https://www.zhihu.com/"},
			{Name: "微信公众号", URL: "https://mp.weixin.qq.com/"},
			{Name: "百度贴吧", URL: "https://tieba.baidu.com/"},
			{Name: "百度百家号", URL: "https://baijiahao.baidu.com/"},
			{Name: "今日头条", URL: "https://www.toutiao.com/"},
			{Name: "新浪博客", URL: "https://blog.sina.com.cn/"},
			{Name: "QQ 空间", URL: "https://qzone.qq.com/"},
			{Name: "QQ 群相册", URL: "https://h5.qzone.qq.com/groupphoto/inqq"},
			{Name: "微商相册", URL: "https://www.szwego.com/"},
			{Name: "OnlyFans", URL: "https://onlyfans.com/"},
		},
	},
	{
		Title: "摄影作品",
		Items: []DownloadBookmarkItem{
			{Name: "Pixiv", URL: "https://www.pixiv.net/"},
			{Name: "Flickr", URL: "https://www.flickr.com/"},
			{Name: "Imgur", URL: "https://imgur.com/"},
			{Name: "Pinterest", URL: "https://www.pinterest.com/"},
			{Name: "花瓣", URL: "https://huaban.com/"},
			{Name: "豆瓣", URL: "https://www.douban.com/"},
			{Name: "图虫", URL: "https://tuchong.com/"},
			{Name: "ArtStation", URL: "https://www.artstation.com/"},
			{Name: "Behance", URL: "https://www.behance.net/"},
			{Name: "Dribbble", URL: "https://dribbble.com/"},
			{Name: "站酷", URL: "https://www.zcool.com.cn/"},
			{Name: "500px", URL: "https://500px.com/"},
			{Name: "500px 中国站", URL: "https://500px.com.cn/"},
			{Name: "堆糖", URL: "https://www.duitang.com/"},
			{Name: "又拍 Yupoo", URL: "https://x.yupoo.com/"},
			{Name: "DeviantArt", URL: "https://www.deviantart.com/"},
			{Name: "Unsplash", URL: "https://unsplash.com/"},
			{Name: "Pexels", URL: "https://www.pexels.com/"},
			{Name: "1x", URL: "https://1x.com/"},
			{Name: "35photo", URL: "https://35photo.pro/"},
			{Name: "CNU", URL: "https://www.cnu.cc/"},
			{Name: "Rajče", URL: "https://www.rajce.net/"},
			{Name: "ImgSrc", URL: "https://imgsrc.ru/"},
			{Name: "Fur Affinity", URL: "https://www.furaffinity.net/"},
			{Name: "Photoplus", URL: "https://live.photoplus.cn/"},
			{Name: "拍立享", URL: "https://www.pailixiang.com/"},
			{Name: "米画师", URL: "https://www.mihuashi.com/"},
			{Name: "太平洋摄影部落", URL: "https://dp.pconline.com.cn/"},
			{Name: "E-Hentai", URL: "https://e-hentai.org/"},
			{Name: "ASMHentai", URL: "https://asmhentai.com/"},
			{Name: "HentaiFox", URL: "https://hentaifox.com/"},
			{Name: "IMHentai", URL: "https://imhentai.xxx/"},
		},
	},
	{
		Title: "建筑生活",
		Items: []DownloadBookmarkItem{
			{Name: "谷德 gooood", URL: "https://www.gooood.cn/"},
			{Name: "ArchDaily", URL: "https://www.archdaily.cn/"},
			{Name: "Houzz", URL: "https://www.houzz.com/"},
			{Name: "WikiArt", URL: "https://www.wikiart.org/"},
			{Name: "IMDb", URL: "https://www.imdb.com/"},
			{Name: "时光网 Mtime", URL: "https://www.mtime.com/"},
			{Name: "携程", URL: "https://you.ctrip.com/"},
			{Name: "房天下", URL: "https://www.fang.com/"},
			{Name: "大众点评", URL: "https://www.dianping.com/"},
			{Name: "中关村在线", URL: "https://detail.zol.com.cn/"},
			{Name: "汽车之家", URL: "https://car.autohome.com.cn/"},
			{Name: "Issuu", URL: "https://issuu.com/"},
		},
	},
}

const (
	chromeBookmarkBarGUID = "0bc5d13f-2cba-5d74-951f-3f233fe6c908"
	chromeOtherGUID       = "82b081ec-3dd3-529c-8475-ab6c344590dd"
	chromeSyncedGUID      = "4cf2e351-0e85-532b-bb37-df045d8f8d0f"
)

// EnsureDownloadBrowserBookmarks seeds Chromium's Default/Bookmarks with all
// supported download-site categories and enables the bookmark bar on all tabs.
func EnsureDownloadBrowserBookmarks(profileDir string) error {
	if strings.TrimSpace(profileDir) == "" {
		return nil
	}
	defaultDir := filepath.Join(profileDir, "Default")
	if err := os.MkdirAll(defaultDir, 0o700); err != nil {
		return err
	}
	if err := ensureBookmarksFile(defaultDir, chromeTimestamp(time.Now())); err != nil {
		return err
	}
	return ensurePreferencesBookmarkBar(defaultDir)
}

func chromeTimestamp(t time.Time) string {
	const unixToWinMicros int64 = 11644473600 * 1_000_000
	return strconv.FormatInt(t.UnixMicro()+unixToWinMicros, 10)
}

func deterministicBookmarkGUID(key string) string {
	sum := sha256.Sum256([]byte("tulai:download-bookmark:" + key))
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

func normalizeBookmarkURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func readJSONObject(path string) (map[string]any, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, false, nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return nil, true, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return obj, true, nil
}

func writeAtomicJSON(path string, v any) error {
	encoded, err := json.MarshalIndent(v, "", "   ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tulai-bm-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(encoded); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func newChromeFolderNode(id int, name, guid, ts string, children []any) map[string]any {
	if children == nil {
		children = []any{}
	}
	return map[string]any{
		"children":       children,
		"date_added":     ts,
		"date_last_used": "0",
		"date_modified":  ts,
		"guid":           guid,
		"id":             strconv.Itoa(id),
		"name":           name,
		"type":           "folder",
	}
}

func newChromeURLNode(id int, name, rawURL, guid, ts string) map[string]any {
	return map[string]any{
		"date_added":     ts,
		"date_last_used": "0",
		"guid":           guid,
		"id":             strconv.Itoa(id),
		"name":           name,
		"type":           "url",
		"url":            rawURL,
	}
}

func scanBookmarkTree(node any, maxID *int, urls map[string]bool) {
	m, ok := node.(map[string]any)
	if !ok {
		return
	}
	if rawID, ok := m["id"].(string); ok {
		if n, err := strconv.Atoi(rawID); err == nil && n > *maxID {
			*maxID = n
		}
	}
	if nodeType, _ := m["type"].(string); nodeType == "url" {
		if rawURL, _ := m["url"].(string); rawURL != "" && urls != nil {
			urls[normalizeBookmarkURL(rawURL)] = true
		}
	}
	if children, ok := m["children"].([]any); ok {
		for _, child := range children {
			scanBookmarkTree(child, maxID, urls)
		}
	}
}

func ensureBookmarksFile(defaultDir, ts string) error {
	bookmarksPath := filepath.Join(defaultDir, "Bookmarks")
	doc, exists, err := readJSONObject(bookmarksPath)
	if err != nil {
		return err
	}
	changed := false
	if !exists || doc["roots"] == nil {
		doc = map[string]any{
			"checksum": "",
			"roots": map[string]any{
				"bookmark_bar": newChromeFolderNode(1, "书签栏", chromeBookmarkBarGUID, ts, []any{}),
				"other":        newChromeFolderNode(2, "其他书签", chromeOtherGUID, ts, []any{}),
				"synced":       newChromeFolderNode(3, "移动设备书签", chromeSyncedGUID, ts, []any{}),
			},
			"version": 1,
		}
		changed = true
	}

	roots, ok := doc["roots"].(map[string]any)
	if !ok {
		return fmt.Errorf("invalid Bookmarks roots")
	}
	if _, ok := roots["bookmark_bar"].(map[string]any); !ok {
		roots["bookmark_bar"] = newChromeFolderNode(1, "书签栏", chromeBookmarkBarGUID, ts, []any{})
		changed = true
	}
	if _, ok := roots["other"].(map[string]any); !ok {
		roots["other"] = newChromeFolderNode(2, "其他书签", chromeOtherGUID, ts, []any{})
		changed = true
	}
	if _, ok := roots["synced"].(map[string]any); !ok {
		roots["synced"] = newChromeFolderNode(3, "移动设备书签", chromeSyncedGUID, ts, []any{})
		changed = true
	}

	maxID := 0
	for key, rootNode := range roots {
		if key == "bookmark_bar" {
			continue
		}
		scanBookmarkTree(rootNode, &maxID, nil)
	}
	bar := roots["bookmark_bar"].(map[string]any)
	existingURLs := make(map[string]bool)
	scanBookmarkTree(bar, &maxID, existingURLs)

	barChildren, _ := bar["children"].([]any)
	if barChildren == nil {
		barChildren = []any{}
	}
	existingFolders := make(map[string]map[string]any)
	for _, child := range barChildren {
		folder, ok := child.(map[string]any)
		if !ok {
			continue
		}
		if typ, _ := folder["type"].(string); typ == "folder" {
			if name, _ := folder["name"].(string); name != "" {
				existingFolders[name] = folder
			}
		}
	}

	nextID := maxID + 1
	for _, spec := range DownloadBookmarkFolders {
		folder, hasFolder := existingFolders[spec.Title]
		var children []any
		if hasFolder {
			children, _ = folder["children"].([]any)
			if children == nil {
				children = []any{}
			}
		}
		folderChanged := false
		for _, item := range spec.Items {
			norm := normalizeBookmarkURL(item.URL)
			if existingURLs[norm] {
				continue
			}
			children = append(children, newChromeURLNode(
				nextID,
				item.Name,
				item.URL,
				deterministicBookmarkGUID(spec.Title+":"+item.URL),
				ts,
			))
			nextID++
			existingURLs[norm] = true
			folderChanged = true
		}
		if !folderChanged {
			continue
		}
		if !hasFolder {
			folder = newChromeFolderNode(
				nextID,
				spec.Title,
				deterministicBookmarkGUID("folder:"+spec.Title),
				ts,
				children,
			)
			nextID++
			barChildren = append(barChildren, folder)
			existingFolders[spec.Title] = folder
		} else {
			folder["children"] = children
			folder["date_modified"] = ts
		}
		changed = true
	}

	if !changed {
		return nil
	}
	bar["children"] = barChildren
	bar["date_modified"] = ts
	doc["checksum"] = ""
	if _, ok := doc["version"]; !ok {
		doc["version"] = 1
	}
	return writeAtomicJSON(bookmarksPath, doc)
}

func ensurePreferencesBookmarkBar(defaultDir string) error {
	prefsPath := filepath.Join(defaultDir, "Preferences")
	prefs, exists, err := readJSONObject(prefsPath)
	if err != nil {
		return err
	}
	if !exists {
		prefs = make(map[string]any)
	}
	barPref, ok := prefs["bookmark_bar"].(map[string]any)
	if !ok {
		prefs["bookmark_bar"] = map[string]any{"show_on_all_tabs": true}
		return writeAtomicJSON(prefsPath, prefs)
	}
	if _, hasShow := barPref["show_on_all_tabs"]; hasShow {
		return nil
	}
	barPref["show_on_all_tabs"] = true
	return writeAtomicJSON(prefsPath, prefs)
}
