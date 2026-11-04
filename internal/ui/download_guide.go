package ui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

type downloadGuideEntry struct{ site, pages, icons string }
type downloadGuideGroup struct {
	title   string
	entries []downloadGuideEntry
}

// Keep page types aligned with the download routes and docs/allpicdownloader-ida.md.
var downloadGuideGroups = []downloadGuideGroup{
	{"普通网页和图片搜索", []downloadGuideEntry{
		{"图片直链、普通网页", "图片直链或网页中可访问的图片；动态网页可尝试浏览器模式。", ""},
		{"百度图片、搜狗图片、360 图片", "图片搜索结果页。", "baidu.com sogou.com 360.cn"},
		{"Bing 图片", "搜索结果和部分带图片编号的单图详情页。", "bing.com"},
		{"Google 图片", "仅尝试历史搜索结果格式；当前实时搜索尚未验证。", "google.com"},
		{"yande.re、Gelbooru", "单图和搜索结果。", "yande.re gelbooru.com"},
		{"昵图", "作品和搜索结果的预览图。", "nipic.com"},
		{"汇图", "作品预览图。", "huitu.com"},
		{"Anime-Pictures", "作品和搜索结果；仅通过合成页面验证。", "anime-pictures.net"},
	}},
	{"电商商品与店铺", []downloadGuideEntry{
		{"亚马逊 Amazon", "商品详情和搜索结果；搜索页按商品保存主图。法国站尚未实测。", "amazon.com"},
		{"eBay 易贝", "商品详情和搜索结果；搜索页目前仅通过合成测试。", "ebay.com"},
		{"Temu", "商品详情及搜索结果；搜索使用下载浏览器，可自动加载或手动滚动后下载。", "temu.com"},
		{"Ozon", "商品图库和规格图；页面采集仅通过合成测试。", "ozon.ru"},
		{"1688", "商品详情、公开店铺相册及相册详情。", "1688.com"},
		{"淘宝、天猫", "商品详情的主图、部分规格图和详情图；可能需要登录。", "taobao.com tmall.com"},
		{"京东、当当、网易严选", "商品详情的主图、部分规格图和详情图。", "jd.com dangdang.com you.163.com"},
		{"拼多多", "商品详情；真实商品采集尚未验证。", "pinduoduo.com"},
		{"苏宁易购、义乌购、微店、有赞", "商品详情的主图、部分规格图和详情图。", "suning.com yiwugo.com weidian.com youzan.com"},
		{"唯品会、蘑菇街、网商园、母婴天下", "商品详情；当前页面或实时采集尚未完整验证。", "vip.com mogujie.com wsy.com mytx.cn"},
		{"速卖通 AliExpress、Alibaba 国际站", "商品详情；可能需要登录或安全验证。", "aliexpress.com alibaba.com"},
		{"Shopee、Etsy、SHEIN", "商品详情中的图库或部分规格图；部分页面尚未实测。", "shopee.com etsy.com shein.com"},
		{"Lazada、日本雅虎商城", "商品详情中的图库和部分详情图。", "lazada.com shopping.yahoo.co.jp"},
		{"敦煌网、中国制造网、兰亭 LightInTheBox", "商品详情；部分页面可能要求安全验证。", "dhgate.com made-in-china.com lightinthebox.com"},
		{"JJ's House", "商品详情的当前选中颜色图库。", "jjshouse.com"},
		{"百度爱采购", "商品详情和店铺商品；店铺完整分页尚未验证。", "b2b.baidu.com"},
		{"得物", "移动商品详情。", "dewu.com"},
		{"闲鱼、抖音商品", "商品详情；真实页面可能要求安全验证。", "goofish.com douyin.com"},
		{"小米有品、洋码头、搜款网、政采云", "商品详情；请使用下载浏览器并等待页面加载。", "youpin.mi.com ymatou.com vvic.com zcygov.cn"},
		{"Wellco", "商品详情和部分分类列表。", "wellco.ca"},
		{"Spoonflower", "单个设计和店铺当前页。", "spoonflower.com"},
		{"无印良品 MUJI", "旧版商品页面的部分展示图。", "muji.com"},
	}},
	{"社交、图文与文章", []downloadGuideEntry{
		{"小红书", "笔记及主页已加载的部分笔记；需下载浏览器，主页尚未实测。", "xiaohongshu.com"},
		{"QQ 空间、QQ群相册", "个人相册列表、群相册及列表；需下载浏览器，真实登录尚未验证。", "qzone.qq.com"},
		{"微博", "帖子、图片墙和相册；可能需要登录。", "weibo.com"},
		{"哔哩哔哩 B站", "图文动态和空间相册。", "bilibili.com"},
		{"抖音图文、快手", "单个图文作品中的图片。", "douyin.com kuaishou.com"},
		{"TikTok", "公开图文和作者主页；主页需使用下载浏览器。", "tiktok.com"},
		{"X / Twitter", "单个图片帖和作者主页；主页只读取已加载的部分内容。", "x.com"},
		{"Facebook", "主页照片列表预览；需浏览器，真实登录内容尚未验证。", "facebook.com"},
		{"Instagram、Tumblr", "单个帖子的图片。", "instagram.com tumblr.com"},
		{"百度贴吧", "帖子图片及部分分页。", "tieba.baidu.com"},
		{"微信公众号、知乎", "公众号文章、知乎文章及回答。", "mp.weixin.qq.com zhihu.com"},
		{"百度百家号、今日头条", "单篇文章正文图片。", "baijiahao.baidu.com toutiao.com"},
		{"新浪博客", "文章正文和作者文章目录。", "blog.sina.com.cn"},
		{"微商相册", "单品和基础店铺；可能需要登录。", "szwego.com"},
		{"OnlyFans", "已授权作者主页的可访问图片；需下载浏览器。", "onlyfans.com"},
	}},
	{"摄影、作品与相册", []downloadGuideEntry{
		{"Pixiv", "单个作品和作者作品列表；动图仅保存静态图片。", "pixiv.net"},
		{"Flickr、Imgur", "单图或帖子、相册。", "flickr.com imgur.com"},
		{"豆瓣", "单图和个人相册；电影、人物图片尚未实测。", "douban.com"},
		{"图虫", "作品和作者作品列表。", "tuchong.com"},
		{"花瓣、Pinterest", "单图和画板；画板可能只取得部分图片。", "huaban.com pinterest.com"},
		{"ArtStation、Behance", "作品和作者主页；部分主页只采集首批作品。", "artstation.com behance.net"},
		{"500px、500px 中国站", "单图或作品及相册；部分页面只能读取已加载内容。", "500px.com 500px.com.cn"},
		{"堆糖", "单图和专辑。", "duitang.com"},
		{"又拍 Yupoo", "用户主页、相册、分类和搜索；实时页面可能限制访问。", "yupoo.com"},
		{"DeviantArt、站酷、CNU", "单个作品或作品展示图。", "deviantart.com zcool.com.cn cnu.cc"},
		{"Rajče、ImgSrc", "相册图片；ImgSrc 浏览器模式只读取当前页。", "rajce.net imgsrc.ru"},
		{"35photo、Fur Affinity", "单个作品和作者作品列表。", "35photo.pro furaffinity.net"},
		{"1x", "单图和图库当前已加载的作品。", "1x.com"},
		{"Unsplash", "单图和搜索结果；搜索只采集部分已加载图片。", "unsplash.com"},
		{"Pexels", "单图页面；当前页面解析仅通过合成测试。", "pexels.com"},
		{"Photoplus、拍立享", "可公开查看和下载的活动图片；需下载浏览器。", "photoplus.cn plusx.cn"},
		{"米画师", "作品和作者页面；需下载浏览器，真实登录内容尚未验证。", "mihuashi.com"},
		{"太平洋摄影部落", "作品页面展示图。", "pconline.com.cn"},
		{"E-Hentai、ASMHentai、HentaiFox", "图片集及阅读器页面；E-Hentai 仅通过合成测试。", "e-hentai.org asmhentai.com hentaifox.com"},
	}},
	{"建筑、旅行与其他", []downloadGuideEntry{
		{"谷德 gooood、ArchDaily", "文章及部分分类列表。", "gooood.cn archdaily.com"},
		{"Houzz", "项目展示图。", "houzz.com"},
		{"WikiArt", "作者作品列表。", "wikiart.org"},
		{"IMDb", "影片图片索引；当前仅通过合成测试。", "imdb.com"},
		{"时光网 Mtime", "影片及影人图片页面。", "mtime.com"},
		{"携程、房天下", "携程景点相册、房天下新房楼盘相册。", "ctrip.com fang.com"},
		{"大众点评", "门店相册；部分页面可能要求验证。", "dianping.com"},
		{"中关村在线、汽车之家", "产品图集或已识别的车系单图页。", "zol.com.cn autohome.com.cn"},
		{"Issuu", "当前阅读器明确提供的页图；需下载浏览器。", "issuu.com"},
	}},
	{"历史页面或尚未提供专用采集", []downloadGuideEntry{
		{"国美、穷游相册、网易博客、美空、四座网", "仅验证过历史页面；当前站点或图片可能不可用。", "gome.com.cn qyer.com blog.163.com meikong.com sizuo.com"},
		{"Dribbble、IMHentai", "历史页面或合成页面已验证，实时采集尚未验证。", "dribbble.com imhentai.xxx"},
		{"网易摄影、天涯、喔图、微销通、YouTube、nhentai", "尚未提供对应的专用图片采集。", "163.com tianya.cn alltuu.com aliyizhan.com youtube.com nhentai.com"},
	}},
}

func matchingDownloadGuide(query string) []downloadGuideGroup {
	query = strings.ToLower(strings.TrimSpace(query))
	var groups []downloadGuideGroup
	for _, group := range downloadGuideGroups {
		var entries []downloadGuideEntry
		for _, entry := range group.entries {
			if query == "" || strings.Contains(strings.ToLower(group.title+" "+entry.site+" "+entry.pages+" "+entry.icons), query) {
				entries = append(entries, entry)
			}
		}
		if len(entries) != 0 {
			groups = append(groups, downloadGuideGroup{group.title, entries})
		}
	}
	return groups
}

func downloadGuideURL(host string) string {
	switch host {
	case "baidu.com":
		return "https://image.baidu.com/"
	case "sogou.com":
		return "https://pic.sogou.com/"
	case "360.cn":
		return "https://image.so.com/"
	case "bing.com":
		return "https://www.bing.com/images"
	case "google.com":
		return "https://www.google.com/imghp"
	default:
		return "https://" + host
	}
}

func (a *App) downloadGuide(c *ui.Context, tok tokens) {
	w, h := c.Size()
	width := min(float32(560), max(float32(240), w-80))
	listHeight := min(float32(360), max(float32(120), h-312))
	groups := matchingDownloadGuide(a.dlGuideSearch)
	count := 0
	for _, group := range groups {
		count += len(group.entries)
	}
	ui.Column(c.Key("dl-guide")).Width(width).Gap(12).Padding(4, 4, 4, 4).Children(func() {
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			icon := ui.Column(c).Size(40, 40).Radius(12).Center().Background(dlTransferSoft(tok))
			icon.Children(func() { ui.Icon(c, iconGlobe).FontSize(20).TextColor(dlTransferStrong(tok)) })
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, "支持哪些页面？").FontSize(18).FontWeight(600).TextColor(tok.text)
				ui.Text(c, "点击站点打开专用浏览器，找到目标页面后复制网址。部分站点需要登录或验证。").
					FontSize(12).TextColor(tok.muted).FillWidth().Wrap()
			})
		})
		ui.TextInput(c.Key("dl-guide-search"), &a.dlGuideSearch).FillWidth().Height(36).
			Label("搜索网站或页面类型").Placeholder("搜索网站或页面类型，例如亚马逊、相册").
			OnChange(func() { a.dlGuideScroll.Y = 0 })
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "网站与页面").FontSize(13).FontWeight(600).TextColor(tok.text)
			ui.Spacer(c)
			ui.Text(c, fmt.Sprintf("%d 条说明", count)).FontSize(12).TextColor(tok.muted)
		})
		ui.Scroll(c.Key("dl-guide-list")).FillWidth().Height(listHeight).Label("支持的页面列表").
			TrackScroll(&a.dlGuideScroll).Children(func() {
			ui.Column(c).FillWidth().Gap(12).Children(func() {
				if len(groups) == 0 {
					ui.Row(c).FillWidth().Gap(10).Padding(16, 14).Radius(10).Background(dlQuiet(tok)).Children(func() {
						ui.Icon(c, iconSearch).FontSize(16).TextColor(tok.muted)
						ui.Text(c, "没有找到匹配的站点；也可以尝试粘贴普通网页地址。").
							FontSize(13).TextColor(tok.muted).Grow(1).MinWidth(0).Wrap()
					})
					return
				}
				for i, group := range groups {
					ui.Column(c.Key(fmt.Sprintf("dl-guide-group-%d", i))).FillWidth().Children(func() {
						ui.Row(c).FillWidth().Padding(8, 10).Radius(8).Background(dlQuiet(tok)).AlignItems(ui.Center).Children(func() {
							ui.Text(c, group.title).FontSize(13).FontWeight(600).TextColor(tok.text).Grow(1).MinWidth(0).Wrap()
							ui.Text(c, fmt.Sprintf("%d 条", len(group.entries))).FontSize(12).TextColor(tok.muted).Shrink(0)
						})
						for j, entry := range group.entries {
							ui.Row(c.Key(fmt.Sprintf("dl-guide-entry-%d", j))).FillWidth().Gap(10).Padding(9, 10).
								BorderWidth(0, 0, 1, 0).BorderColor(tok.border.Alpha(0.7)).AlignItems(ui.Center).Children(func() {
								downloadGuideSiteIcons(c, entry.icons, tok)
								ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
									ui.Text(c, entry.site).FontSize(13).FontWeight(500).TextColor(tok.text).FillWidth().Wrap()
									ui.Text(c, entry.pages).FontSize(12).TextColor(tok.muted).FillWidth().Wrap()
									if entry.icons != "" {
										ui.Row(c).FillWidth().Gap(5).Wrap().Children(func() {
											for _, host := range strings.Fields(entry.icons) {
												ui.Button(c.Key("dl-guide-open-"+host), "打开 "+host).FontSize(11).Height(30).Padding(4, 8).
													Disabled(a.dlBrowserBusy).OnClick(func() {
													a.dlBrowserMode = true
													a.openDownloadBrowserURL(downloadGuideURL(host))
												})
											}
										})
									}
								})
							})
						}
					})
				}
			})
		})
		if a.dlBrowserErr != "" {
			ui.Text(c, a.dlBrowserErr).FontSize(12).TextColor(tok.danger).FillWidth().Wrap()
		} else if a.dlBrowserBusy {
			ui.Text(c, "正在打开下载浏览器…").FontSize(12).TextColor(tok.muted)
		}
		ui.Row(c).FillWidth().Padding(8, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(tok.border).AlignItems(ui.Center).Children(func() {
			if len(groups) > 0 && strings.TrimSpace(a.dlGuideSearch) == "" {
				ui.Text(c, "向下滚动查看更多站点").FontSize(12).TextColor(tok.muted)
			}
			ui.Spacer(c)
			ui.Button(c.Key("dl-guide-back"), "返回新建任务").OnClick(func() { a.dlGuideOn = false })
		})
	})
}
