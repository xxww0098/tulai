package download

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

var (
	ymatouShopPath = regexp.MustCompile(`^/shop/([1-9][0-9]{0,17})/item$`)
	ymatouID       = regexp.MustCompile(`^p[1-9][0-9]{0,18}$`)
	ymatouOldPath  = regexp.MustCompile(`^/item/(p[1-9][0-9]{0,18})(?:\.html)?$`)
	ymatouWebP     = regexp.MustCompile(`^webp[1-9][0-9]{0,4}$`)
)

// The current shop share page is verified. The reference's historical route
// is recognized only to report that its current form cannot be confirmed.
func ymatouRoute(u *url.URL) (string, string) {
	if u == nil || len(u.String()) > 8192 || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) || u.RawPath != "" {
		return "", ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "m.ymatou.com" && host != "www.ymatou.com" && host != "ymatou.com" {
		return "", ""
	}
	if m := ymatouOldPath.FindStringSubmatch(u.Path); m != nil {
		return "legacy", m[1]
	}
	if host != "m.ymatou.com" || !ymatouShopPath.MatchString(u.Path) {
		return "", ""
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query["productId"]) != 1 || !ymatouID.MatchString(query.Get("productId")) {
		return "", ""
	}
	return "shop", query.Get("productId")
}

func isYmatouProduct(u *url.URL) bool {
	kind, _ := ymatouRoute(u)
	return kind != ""
}

func ymatouSameProduct(a, b *url.URL) bool {
	aKind, aID := ymatouRoute(a)
	bKind, bID := ymatouRoute(b)
	return aKind != "" && aKind == bKind && aID == bID && sameOrigin(a, b)
}

func ymatouImage(raw string) string {
	if len(raw) > 8192 {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443")) ||
		strings.Contains(u.Path, "\\") || strings.Contains(u.Path, "..") {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "pic1.yangmatou.com" && host != "pic2.yangmatou.com" {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	for _, segment := range parts {
		if segment == "" || segment == "." || segment == ".." {
			return ""
		}
	}
	name := parts[len(parts)-1]
	if ymatouWebP.MatchString(name) {
		name = parts[len(parts)-2]
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".bmp":
	default:
		return ""
	}
	// Current product data returns HTTP for these hosts, while the site's HTTPS
	// page upgrades the same image paths in the browser. Both hosts were tested.
	u.Scheme, u.Fragment = "https", ""
	return u.String()
}

type ymatouVariant struct {
	URL   string `json:"url"`
	Value string `json:"value"`
}

type ymatouState struct {
	Location         string          `json:"location"`
	ProductID        string          `json:"productId"`
	DetailID         string          `json:"detailId"`
	DescriptionID    string          `json:"descriptionId"`
	Title            string          `json:"title"`
	Main             []string        `json:"main"`
	Variant          []ymatouVariant `json:"variant"`
	Description      []string        `json:"description"`
	MainReady        bool            `json:"mainReady"`
	VariantReady     bool            `json:"variantReady"`
	DescriptionReady bool            `json:"descriptionReady"`
	Invalid          bool            `json:"invalid"`
	Truncated        bool            `json:"truncated"`
}

// Read only the current product's image fields from the already-loaded page.
// No account, cookie, browser storage, signature, or network response is read.
// The total number and size of returned strings are bounded.
const ymatouStateScript = `(() => {
  const loc = location.href.length <= 8192 ? location.href : '';
  const v = document.querySelector('.item')?.__vue__;
  if (!v) return {location:loc};
  const p = v.productDetailInfo || {}, d = v.detailInfo;
  const out = {location:loc,productId:String(v.productId || '').slice(0,64),detailId:String(p.id || '').slice(0,64),
    descriptionId:String(d?.productId || '').slice(0,64),title:typeof p.title === 'string' ? p.title.slice(0,4096) : '',
    main:[],variant:[],description:[],mainReady:Array.isArray(p.pics),variantReady:Array.isArray(p.skus),
    descriptionReady:!!d && Array.isArray(d.moduleList),invalid:false,truncated:false};
  let count=0,bytes=0,steps=0;
  function add(list,raw,value) {
    if (raw == null || raw === '') return;
    if (typeof raw !== 'string') { out.invalid=true; return; }
    if (++count > 10000 || raw.length > 8192 || (bytes += raw.length) > 2097152) { out.truncated=true; return; }
    if (list === out.variant) list.push({url:raw,value:typeof value === 'string' ? value.slice(0,512) : ''});
    else list.push(raw);
  }
  function bounded(a,fn) {
    if (a == null) return;
    if (!Array.isArray(a)) { out.invalid=true; return; }
    if (a.length > 10000) out.truncated=true;
    for (let i=0;i<Math.min(a.length,10000)&&!out.truncated;i++) {
      if (++steps > 20000) {out.truncated=true; break;}
      fn(a[i]);
    }
  }
  bounded(p.pics,x=>add(out.main,x));
  bounded(p.skus,s=>{
    if (!s || typeof s !== 'object') {out.invalid=true; return;}
    bounded(s.properties,q=>{
      if (!q || typeof q !== 'object') {out.invalid=true; return;}
      add(out.variant,q.originalPic,q.value);
    });
  });
  if (d && Array.isArray(d.moduleList)) bounded(d.moduleList,m=>{
    if (!m || typeof m !== 'object') {out.invalid=true; return;}
    bounded(m.picList,x=>add(out.description,x));
    bounded(m.mixedContentList,x=>{
      if (!x || typeof x !== 'object') {out.invalid=true; return;}
      if (x.type === 'img') add(out.description,x.value);
    });
  });
  return out;
})()`

func ymatouCollectState(ctx context.Context, page *url.URL, state ymatouState) (Discovery, error) {
	result := Discovery{IsPage: true, Title: state.Title}
	kind, id := ymatouRoute(page)
	current, err := crawlURL(nil, state.Location)
	if kind != "shop" || page.Scheme != "https" || err != nil || !ymatouSameProduct(page, current) || state.ProductID != id || state.DetailID != id {
		return result, errors.New("洋码头商品页面尚未加载或商品身份不匹配")
	}
	seen := map[string]bool{}
	invalid := state.Invalid
	add := func(raw, category, value string, index int) {
		image := ymatouImage(raw)
		if image == "" {
			invalid = true
			return
		}
		if seen[image] {
			return
		}
		seen[image] = true
		item := Item{URL: image, Filename: "ymatou-" + id + "-" + category + "-" + strconv.Itoa(index+1), RequireImage: true, PublicOnly: true,
			Metadata: map[string]string{"ymatou_product_id": id, "image_kind": category}}
		if value != "" {
			item.Metadata["sku_value"] = value
		}
		if ref := imageReferer(page.String(), image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	for i, raw := range state.Main {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		add(raw, "main", "", i)
	}
	for i, variant := range state.Variant {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		add(variant.URL, "sku", variant.Value, i)
	}
	var partial error
	if state.DescriptionReady && state.DescriptionID == id {
		for i, raw := range state.Description {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			add(raw, "description", "", i)
		}
	} else {
		partial = errors.Join(partial, errors.New("洋码头详情图片尚未加载或身份不匹配，结果可能不完整"))
	}
	if !state.MainReady || len(state.Main) == 0 || !state.VariantReady {
		partial = errors.Join(partial, errors.New("洋码头主图或规格数据尚未加载，结果可能不完整"))
	}
	if invalid {
		partial = errors.Join(partial, errors.New("洋码头部分图片地址或数据格式无法确认，结果可能不完整"))
	}
	if state.Truncated {
		partial = errors.Join(partial, errors.New("洋码头图片超过采集上限，结果可能不完整"))
	}
	if len(result.Items) == 0 && partial == nil {
		partial = errors.New("洋码头未返回可确认的商品图片")
	}
	return result, partial
}

func discoverYmatou(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	kind, _ := ymatouRoute(page)
	if kind == "" || getter == nil {
		return result, errors.New("洋码头商品地址或下载客户端无效")
	}
	if kind == "legacy" {
		return result, errors.New("洋码头旧商品页面已无法确认，请使用当前分享页面")
	}
	if page.Scheme != "https" {
		return result, errors.New("洋码头请使用 HTTPS 页面地址")
	}
	return result, errors.New("洋码头商品需在下载浏览器中打开后采集")
}

func ymatouCapture(ctx context.Context, session *RenderedSession, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	kind, id := ymatouRoute(page)
	if kind == "legacy" {
		return result, errors.New("洋码头旧商品页面已无法确认，请使用当前分享页面")
	}
	if kind != "shop" || page.Scheme != "https" || session == nil {
		return result, errors.New("洋码头商品地址或下载浏览器无效")
	}
	var state ymatouState
	for attempt := 0; attempt < 16; attempt++ {
		state = ymatouState{}
		if err := session.run(ctx, chromedp.Evaluate(ymatouStateScript, &state)); err != nil {
			return result, fmt.Errorf("读取洋码头商品页面: %w", err)
		}
		current, err := crawlURL(nil, state.Location)
		if err != nil || !ymatouSameProduct(page, current) {
			return result, errors.New("洋码头商品页面已跳转")
		}
		if state.ProductID != "" && state.ProductID != id || state.DetailID != "" && state.DetailID != id {
			return result, errors.New("洋码头商品身份不匹配")
		}
		if state.ProductID == id && state.DetailID == id && state.MainReady && state.VariantReady && state.DescriptionReady {
			break
		}
		if attempt < 15 {
			if err := Wait(ctx, 250*time.Millisecond); err != nil {
				return result, err
			}
		}
	}
	return ymatouCollectState(ctx, page, state)
}
