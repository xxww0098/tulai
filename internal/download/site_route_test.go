package download

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestSiteRoutesAllowHTTPSUpgradeAndRejectDowngrade(t *testing.T) {
	tests := []struct {
		name, raw, upgraded, body string
	}{
		{"yande", "http://yande.re/post/show/123", "https://yande.re/post/show/123", `<script>Post.register_resp({"posts":[` + yandeTestPost(123, "files.yande.re") + `]});</script>`},
		{"ctrip", "http://you.ctrip.com/photos/sight/city2/r123.html", "https://you.ctrip.com/photos/sight/city2/r123.html", ctripTestHTML(123, ctripTestList)},
		{"fang", "http://xian.newhouse.fang.com/loupan/123/photo/", "https://xian.newhouse.fang.com/loupan/123/photo/", `<a id="xfptxq_B03_016" href="/loupan/123/photo/"></a><div id="album-viewer"><img class="bigImg" src="https://loupanw.soufunimg.com/viewimage/housepic/a/1/a.jpg"></div>`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			final := tc.upgraded
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				response := tiebaTestResponse(raw, 200, tc.body)
				response.FinalURL = final
				return response, nil
			})
			got, _ := Discover(context.Background(), getter, tc.raw, 1)
			if len(got.Items) == 0 {
				t.Fatal("HTTPS upgrade rejected")
			}
			final = tc.raw
			got, err := Discover(context.Background(), getter, tc.upgraded, 1)
			if err == nil || len(got.Items) != 0 {
				t.Fatalf("accepted HTTPS downgrade: %+v %v", got.Items, err)
			}
		})
	}
}

func TestDiscoverRoutesQzonePersonal(t *testing.T) {
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("QQ 空间需要通过浏览器采集，不应直接请求")
		return nil, nil
	})
	got, err := Discover(context.Background(), getter, "https://user.qzone.qq.com/123/photo/", 1)
	if !got.IsPage || err == nil || !strings.Contains(err.Error(), "下载浏览器") {
		t.Fatalf("QQ 空间路由 = %+v, %v", got, err)
	}
	got, err = Discover(context.Background(), getter, "https://user.qzone.qq.com/123/photo/album_1", 1)
	if !got.IsPage || err == nil || !strings.Contains(err.Error(), "暂不支持") {
		t.Fatalf("QQ 空间单相册路由 = %+v, %v", got, err)
	}
	got, err = Discover(context.Background(), getter, "https://h5.qzone.qq.com/groupphoto/inqq/album/123", 1)
	if !got.IsPage || err == nil || !strings.Contains(err.Error(), "下载浏览器") {
		t.Fatalf("QQ 群相册列表路由 = %+v, %v", got, err)
	}
}

func TestDiscoverRoutesDouyinProduct(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<div class="head-figure__media-view__content" style="background-image:url('https://p3-aio.ecombdimg.com/obj/temai/main')"></div>`
		return tiebaTestResponse(raw, http.StatusOK, body), nil
	})
	got, err := Discover(context.Background(), getter, "https://haohuo.jinritemai.com/ecommerce/trade/detail/index.html?id=123", 1)
	if err == nil || !strings.Contains(err.Error(), "可能不完整") || len(got.Items) != 1 || got.Items[0].URL != "https://p3-aio.ecombdimg.com/obj/temai/main" {
		t.Fatalf("抖音商品路由 = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesSinaBlogArticle(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<div id="sina_keyword_ad_area2"><img real_src="https://s11.sinaimg.cn/bmiddle/abc&amp;690"></div>`
		return tiebaTestResponse(raw, http.StatusOK, body), nil
	})
	got, err := Discover(context.Background(), getter, "https://blog.sina.com.cn/s/blog_517646d10100ecb7.html", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://s11.sinaimg.cn/orignal/abc" {
		t.Fatalf("新浪博客路由 = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesIMDbIndex(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return imdbTestResponse(raw, imdbFixture), nil
	})
	got, err := Discover(context.Background(), getter, imdbIndex, 1)
	if err != nil || len(got.Items) != 2 || got.Items[0].URL != "https://m.media-amazon.com/images/M/MV5Bfirst.jpg" {
		t.Fatalf("IMDb 路由 = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesYouzan(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return tiebaTestResponse(raw, http.StatusOK, youzanMobileHTML), nil
	})
	got, err := Discover(context.Background(), getter, youzanTestM, 1)
	if err == nil || !strings.Contains(err.Error(), "描述图片可能不完整") || len(got.Items) != 2 || got.Items[0].URL != "https://img01.yzcdn.cn/upload_files/main.png" {
		t.Fatalf("有赞路由 = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesTiebaThread(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return tiebaTestResponse(raw, 200, `<div class="l_post"><img class="BDE_Image" src="https://imgsa.baidu.com/forum/w580/sign/a.jpg"></div>`), nil
	})
	got, err := Discover(context.Background(), getter, "https://tieba.baidu.com/p/123", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://imgsa.baidu.com/forum/pic/item/a.jpg" {
		t.Fatalf("tieba route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutes1688Offer(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return album1688Response(raw, 200, `<div id="desc-lazyload-container"><img src="https://cbu01.alicdn.com/a.jpg"></div>`, nil), nil
	})
	got, err := Discover(context.Background(), getter, "https://detail.1688.com/offer/123.html", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://cbu01.alicdn.com/a.jpg" {
		t.Fatalf("1688 offer route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesBilibiliPost(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<script>window.__INITIAL_STATE__={"detail":{"id_str":"123","modules":[{"pic":{"pics":[{"url":"https://i0.hdslb.com/a.jpg"}]}}]}};</script>`
		return tiebaTestResponse(raw, 200, body), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.bilibili.com/opus/123", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://i0.hdslb.com/a.jpg" {
		t.Fatalf("bilibili route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesBilibiliAlbum(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if u.Query().Get("page_num") != "0" {
			return tiebaTestResponse(raw, 200, `{"code":0,"data":{"items":[]}}`), nil
		}
		body := `{"code":0,"data":{"items":[{"dyn_id":"42","pictures":[{"img_src":"https://i0.hdslb.com/a.jpg"}]}]}}`
		return tiebaTestResponse(raw, 200, body), nil
	})
	got, err := Discover(context.Background(), getter, "https://space.bilibili.com/123/album", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://i0.hdslb.com/a.jpg" {
		t.Fatalf("bilibili album route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesImgurPost(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return tiebaTestResponse(raw, 200, `<meta property="og:image" content="https://i.imgur.com/abcde.jpg">`), nil
	})
	got, err := Discover(context.Background(), getter, "https://imgur.com/abcde", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://i.imgur.com/abcde.jpg" {
		t.Fatalf("imgur route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesTaobaoItem(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<div id="J_UlThumb"><img src="https://img.alicdn.com/main.jpg"></div><div class="descV8-container"><img src="https://img.alicdn.com/detail.jpg"></div>`
		return tiebaTestResponse(raw, 200, body), nil
	})
	got, err := Discover(context.Background(), getter, "https://item.taobao.com/item.htm?id=123", 1)
	if err != nil || len(got.Items) != 2 {
		t.Fatalf("taobao route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesPixivArtwork(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if strings.HasSuffix(raw, "/pages") {
			return pixivTestResponse(raw, 200, `{"error":false,"body":[{"urls":{"original":"https://i.pximg.net/img-original/a.jpg"}}]}`), nil
		}
		return pixivTestResponse(raw, 200, `{"error":false,"body":{"id":"123","pageCount":1}}`), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.pixiv.net/artworks/123", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://i.pximg.net/img-original/a.jpg" {
		t.Fatalf("pixiv route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesFlickrPhoto(t *testing.T) {
	photo := flickrTestPhoto("123", map[string]string{"o": "https://live.staticflickr.com/1/123_original_o.jpg"})
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return flickrTestResponse(raw, flickrTestHTML(false, "123", []map[string]any{photo}, 1, true, false), "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.flickr.com/photos/name/123", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://live.staticflickr.com/1/123_original_o.jpg" {
		t.Fatalf("flickr route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesWeiboPost(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `{"mblogid":"AbC123","pic_infos":{"a":{"original":{"url":"https://wx1.sinaimg.cn/large/a.jpg"}}}}`
		return flickrTestResponse(raw, body, "application/json"), nil
	})
	got, err := Discover(context.Background(), getter, "https://weibo.com/1/AbC123", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://wx1.sinaimg.cn/large/a.jpg" {
		t.Fatalf("weibo route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesDoubanPhoto(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<div class="mainphoto"><img src="https://img3.doubanio.com/view/photo/l/public/p123.jpg"></div>`
		return doubanTestResponse(raw, http.StatusOK, body), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.douban.com/photos/photo/123/", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://img3.doubanio.com/view/photo/raw/public/p123.jpg" {
		t.Fatalf("douban route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesHuabanPin(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<script>app.page["pin"]={"pin_id":123,"file":{"key":"image-key","type":"image/jpeg"}};</script>`
		return flickrTestResponse(raw, body, "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, "https://huaban.com/pins/123", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://hbimg.huaban.com/image-key" {
		t.Fatalf("huaban route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesWechatArticle(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<div id="js_content"><img data-src="https://mmbiz.qpic.cn/mmbiz_jpg/image/640"></div>`
		return flickrTestResponse(raw, body, "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, "https://mp.weixin.qq.com/s/example", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://mmbiz.qpic.cn/mmbiz_jpg/image/640" {
		t.Fatalf("wechat route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesArtstationProject(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `{"hash_id":"abc123","title":"work","assets":[{"has_image":true,"image_url":"https://cdna.artstation.com/p/assets/images/001/image.jpg"}]}`
		return flickrTestResponse(raw, body, "application/json"), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.artstation.com/artwork/abc123", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://cdna.artstation.com/p/assets/images/001/image.jpg" {
		t.Fatalf("artstation route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesJDProduct(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<div class="spec-items"><img src="https://img14.360buyimg.com/n5/jfs/t1/a.jpg"></div><script>var pageConfig = {product:{skuid:123}};</script>`
		if strings.Contains(raw, "/product/graphext/") {
			body = `<div id="detail-main"><img src="https://img10.360buyimg.com/sku/jfs/t1/b.jpg"></div>`
		}
		return flickrTestResponse(raw, body, "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, "https://item.jd.com/123.html", 1)
	if err != nil || len(got.Items) != 2 || got.Items[0].URL != "https://img10.360buyimg.com/n5/s1440x1440_jfs/t1/a.jpg" {
		t.Fatalf("jd route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesAmazonProduct(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<div id="imageBlock_feature_div"><img id="landingImage" data-old-hires="https://m.media-amazon.com/images/I/main.jpg"></div><div id="aplus_feature_div"><img src="https://m.media-amazon.com/images/S/detail.jpg"></div>`
		return flickrTestResponse(raw, body, "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.amazon.com/dp/B0B5RJHYFD", 1)
	if err != nil || len(got.Items) != 2 || got.Items[0].URL != "https://m.media-amazon.com/images/I/main.jpg" {
		t.Fatalf("amazon route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesAliExpressProduct(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<div class="slider--img--one"><img src="https://ae01.alicdn.com/kf/main.jpg"></div><div class="description--product-description--one"><img src="https://ae01.alicdn.com/kf/detail.jpg"></div>`
		return flickrTestResponse(raw, body, "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.aliexpress.com/item/123.html", 1)
	if err != nil || len(got.Items) != 2 || got.Items[0].URL != "https://ae01.alicdn.com/kf/main.jpg" {
		t.Fatalf("aliexpress route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesInstagramPost(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<script>{"shortcode_media":{"shortcode":"AbC123","__typename":"GraphImage","display_url":"https://scontent.cdninstagram.com/photo.jpg"}}</script>`
		return flickrTestResponse(raw, body, "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.instagram.com/p/AbC123/", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://scontent.cdninstagram.com/photo.jpg" {
		t.Fatalf("instagram route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesPinduoduoProduct(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<script>window.rawData={"store":{"initDataObj":{"goodsId":"123","viewImageData":[{"url":"https://img.pddpic.com/main.jpg"}],"skus":[],"detailGallery":[{"url":"https://img.pddpic.com/detail.jpg"}]}}};</script>`
		return flickrTestResponse(raw, body, "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, "https://mobile.yangkeduo.com/goods.html?goods_id=123", 1)
	if err != nil || len(got.Items) != 2 || got.Items[0].URL != "https://img.pddpic.com/main.jpg" {
		t.Fatalf("pinduoduo route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesTuchongPost(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<script>window.post={"post_id":"456","site_id":"123"};</script><article class="post-content"><img src="https://photo.tuchong.com/123/t/1.jpg"></article>`
		return flickrTestResponse(raw, body, "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, "https://tuchong.com/123/456/", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://photo.tuchong.com/123/f/1.jpg" {
		t.Fatalf("tuchong route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesBehanceProject(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<script>var state={"project":{"id":123,"modules":[{"type":"image","sizes":{"original":"https://mir-s3-cdn-cf.behance.net/project_modules/original/image.jpg"}}]}};</script>`
		return flickrTestResponse(raw, body, "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.behance.net/gallery/123/work", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://mir-s3-cdn-cf.behance.net/project_modules/original/image.jpg" {
		t.Fatalf("behance route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesZcoolWork(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"data":{"productImages":[{"urlBig":"https://img.zcool.cn/community/image.jpg"}]}}}}</script>`
		return flickrTestResponse(raw, body, "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.zcool.com.cn/work/ZMTIz.html", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://img.zcool.cn/community/image.jpg" {
		t.Fatalf("zcool route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutes500pxPhoto(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<script>{"getPhotoById":{"id":"1234","urls":{"size_4k":"https://drscdn.500px.org/photo/1234/original/image.jpg"}}}</script>`
		return flickrTestResponse(raw, body, "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, "https://500px.com/photo/1234", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://drscdn.500px.org/photo/1234/original/image.jpg" {
		t.Fatalf("500px route = %+v, %v", got.Items, err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `{"data":{"getPhotoById":{"id":"1234","urls":{"size_4k":"https://drscdn.500px.org/photo/1234/original/image.jpg"}}}}`
		return flickrTestResponse(raw, body, "application/json"), nil
	})
	got, err = Discover(context.Background(), getter, "https://www.500px.com.cn/photo/1234", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://drscdn.500px.org/photo/1234/original/image.jpg" {
		t.Fatalf("500px China route = %+v, %v", got.Items, err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `{"data":{"getPhotoById":{"id":"AbCd12","urls":{"size_4k":"https://cdn-resize-prod-cn.500px.me/photo/1234/0.jpg"}}}}`
		return flickrTestResponse(raw, body, "application/json"), nil
	})
	got, err = Discover(context.Background(), getter, "https://www.500px.com.cn/photo/AbCd12", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://cdn-resize-prod-cn.500px.me/photo/1234/0.jpg" {
		t.Fatalf("500px China CDN route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesAlibabaProduct(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<div class="id-overflow-hidden id-h-full"><img src="https://s.alicdn.com/main.jpg"></div><div id="J-rich-text-description"><img src="https://s.alicdn.com/detail.jpg"></div><script>var product={"globalData":{"sku":null},"productHtmlDescription":"<img src=\"https://s.alicdn.com/detail.jpg\">"};</script>`
		return flickrTestResponse(raw, body, "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.alibaba.com/product-detail/item_12345.html", 1)
	if err != nil || len(got.Items) != 2 || got.Items[0].URL != "https://s.alicdn.com/main.jpg" {
		t.Fatalf("alibaba route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesDuitangPost(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<div id="detailcnt"><a class="vieworg" href="https://c-ssl.duitang.com/uploads/item/image.jpg">查看原图</a></div>`
		return flickrTestResponse(raw, body, "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.duitang.com/blog/?id=123", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://c-ssl.duitang.com/uploads/item/image.jpg" {
		t.Fatalf("duitang route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesWeishangFragment(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `{"errcode":0,"result":{"commodity":{"imgsSrc":["https://xcimg.szwego.com/img/1.jpg"]}}}`
		return flickrTestResponse(raw, body, "application/json"), nil
	})
	got, err := Discover(context.Background(), getter, weishangTestPage, 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://xcimg.szwego.com/img/1.jpg" {
		t.Fatalf("weishang route = %+v, %v", got.Items, err)
	}
	ordinary, err := crawlInputURL("https://example.test/page#section")
	if err != nil {
		t.Fatal(err)
	}
	if ordinary.Fragment != "" {
		t.Fatalf("ordinary fragment = %q", ordinary.Fragment)
	}
}

func TestDiscoverRoutesPinterestPin(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<meta property="og:url" content="https://www.pinterest.com/pin/123456/"><meta property="og:image" content="https://i.pinimg.com/736x/aa/image.jpg">`
		return flickrTestResponse(raw, body, "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.pinterest.com/pin/123456/", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://i.pinimg.com/originals/aa/image.jpg" || len(got.Items[0].AlternateURLs) != 1 {
		t.Fatalf("pinterest route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesEbayProduct(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<div class="ux-image-carousel-container"><div class="ux-image-carousel-item"><img data-zoom-src="https://i.ebayimg.com/images/g/A/s-l1600.jpg"></div></div><iframe id="desc_ifr" src="https://itm.ebaydesc.com/itmdesc/123456789012"></iframe>`
		if strings.Contains(raw, "ebaydesc.com") {
			body = `<img src="https://i.ebayimg.com/images/g/B/s-l1600.jpg">`
		}
		return flickrTestResponse(raw, body, "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.ebay.com/itm/123456789012", 1)
	if err != nil || len(got.Items) != 2 || got.Items[0].URL != "https://i.ebayimg.com/images/g/A/s-l1600.jpg" {
		t.Fatalf("ebay route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesDeviantArtWork(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return deviantArtResponse(raw, deviantArtHTML(deviantArtStateJSON(deviantArtWork, `{}`))), nil
	})
	got, err := Discover(context.Background(), getter, deviantArtTestPage, 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].Metadata["image_kind"] != "fullview" {
		t.Fatalf("deviantart route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesShopeeProduct(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<script type="application/json">{"data":{"item":{"item_id":7065872218,"shop_id":314649750},"product_images":{"images":["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"],"first_tier_variations":[{"image":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]}}}</script>`
		return flickrTestResponse(raw, body, "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, shopeeBase, 1)
	if err != nil || len(got.Items) != 2 || !strings.HasPrefix(got.Items[0].URL, "https://down-sg.img.susercontent.com/file/") {
		t.Fatalf("shopee route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesEtsyProduct(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<div class="listing-page-image-carousel-component"><img class="carousel-image" data-src-zoom-image="https://i.etsystatic.com/1/image.jpg"></div>`
		return flickrTestResponse(raw, body, "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.etsy.com/listing/87673424/item", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://i.etsystatic.com/1/image.jpg" {
		t.Fatalf("etsy route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesTumblrPost(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return tumblrTestResponse(raw, tumblrSampleBody), nil
	})
	got, err := Discover(context.Background(), getter, tumblrSampleURL, 1)
	if err != nil || len(got.Items) != 4 || got.Items[0].URL != "https://64.media.tumblr.com/post/s1280/large.jpg" {
		t.Fatalf("tumblr route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesYahooJPProduct(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<div id="itmbasic"><div class="splide__list"><img src="https://item-shopping.c.yimg.jp/i/n/sample"></div></div>`
		return flickrTestResponse(raw, body, "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, "https://store.shopping.yahoo.co.jp/shop/item.html", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://item-shopping.c.yimg.jp/i/f/sample" {
		t.Fatalf("yahoo jp route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverRoutesLazadaProduct(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<script>var __moduleData__={"data":{"root":{"fields":{"primaryKey":{"itemId":"111650098"},"skuGalleries":{"0":[{"type":"img","src":"https://laz-img-sg.alicdn.com/p/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg"}]}}}}};</script>`
		return flickrTestResponse(raw, body, "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, lazadaSG, 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://laz-img-sg.alicdn.com/p/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg" {
		t.Fatalf("lazada route = %+v, %v", got.Items, err)
	}
}

func TestDiscoverSiteFailureDoesNotSaveUnrelatedImages(t *testing.T) {
	requests := 0
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		requests++
		if strings.Contains(raw, "/ajax/statuses/show") {
			return &Response{StatusCode: http.StatusForbidden, FinalURL: raw, Header: http.Header{}, Body: http.NoBody}, nil
		}
		return flickrTestResponse(raw, `<img src="https://example.org/avatar.jpg">`, "text/html"), nil
	})
	got, err := Discover(context.Background(), getter, "https://weibo.com/1/AbC123", 1)
	if err == nil || len(got.Items) != 0 || requests != 1 {
		t.Fatalf("site failure: items=%+v, requests=%d, err=%v", got.Items, requests, err)
	}
}
