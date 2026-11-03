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

var zcygovItemPath = regexp.MustCompile(`^/items/([1-9][0-9]{0,18})$`)
var zcygovDetailPath = regexp.MustCompile(`^/[0-9]+[A-Z]{2}/[^/]+/[^/]+/[^/]+/[^/]+$`)

func zcygovID(u *url.URL) string {
	if u == nil || len(u.String()) > 8192 || u.User != nil || u.RawPath != "" ||
		(u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "zcygov.cn" && !strings.HasSuffix(host, ".zcygov.cn") {
		return ""
	}
	m := zcygovItemPath.FindStringSubmatch(u.Path)
	if m == nil {
		return ""
	}
	return m[1]
}

func isZcygovProduct(u *url.URL) bool { return zcygovID(u) != "" }

func zcygovSameProduct(a, b *url.URL) bool {
	id := zcygovID(a)
	return id != "" && id == zcygovID(b) && sameOrigin(a, b)
}

// Only the current product's own URLs on the two CDNs seen in the public
// sample are accepted. Detail-image URLs may have no filename extension.
func zcygovImage(page *url.URL, raw string) string {
	if len(raw) > 8192 {
		return ""
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") ||
		strings.Contains(u.Path, "..") || strings.Contains(u.Path, "\\") || strings.Contains(u.Path, "//") {
		return ""
	}
	ext := strings.ToLower(path.Ext(u.Path))
	isImageExt := ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" || ext == ".gif" || ext == ".bmp" || ext == ".avif"
	switch strings.ToLower(u.Hostname()) {
	case "itemcdn.zcycdn.com":
		parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" || !isImageExt {
			return ""
		}
	case "sitecdn.zcycdn.com":
		if !zcygovDetailPath.MatchString(u.Path) || (ext != "" && !isImageExt) {
			return ""
		}
	default:
		return ""
	}
	return u.String()
}

type zcygovVariant struct {
	URL   string `json:"url"`
	Value string `json:"value"`
}

type zcygovState struct {
	Location         string          `json:"location"`
	ProductID        string          `json:"productId"`
	Title            string          `json:"title"`
	Main             []string        `json:"main"`
	Variant          []zcygovVariant `json:"variant"`
	Description      []string        `json:"description"`
	MainReady        bool            `json:"mainReady"`
	VariantReady     bool            `json:"variantReady"`
	DescriptionReady bool            `json:"descriptionReady"`
	Incomplete       bool            `json:"incomplete"`
	Invalid          bool            `json:"invalid"`
	Truncated        bool            `json:"truncated"`
}

// The site supplies these fields to its product components. Read only this
// product's rendered image areas and public image fields, not browser state,
// cookies or account information. Lists and the returned data are bounded.
const zcygovStateScript = `(() => {
  const out={location:location.href.length<=8192?location.href:'',productId:'',title:'',
    main:[],variant:[],description:[],mainReady:false,variantReady:false,descriptionReady:false,
    incomplete:false,invalid:false,truncated:false};
  const roots=document.querySelectorAll('.img-thumb-change,.new-img-thumb-change');
  if(roots.length!==1)return out;
  const idValue=x=>typeof x==='number'&&!Number.isSafeInteger(x)?'':String(x||'');
  function parentVMs(el){const a=[];for(let p=el,n=0;p&&n++<32;p=p.parentElement)if(p.__vue__)a.push(p.__vue__);return a;}
  const vms=parentVMs(roots[0]);
  const owner=vms.find(v=>v.$store?.state?.item?.item?.item?.id);
  const p=owner?.$store?.state?.item?.item;
  if(!p || !p.item)return out;
  out.productId=idValue(p.item.id).slice(0,64);
  if(typeof p.item.name==='string')out.title=p.item.name.slice(0,4096);
  if(!out.productId || out.productId!==location.pathname.split('/').pop())return out;
  let count=0,bytes=0,steps=0;
  function add(a,url,value){
    if(typeof url!=='string'||!url||url.length>8192){out.invalid=true;return;}
    if(++count>10000||(bytes+=url.length)>2097152){out.truncated=true;return;}
    if(a===out.variant)a.push({url,value:typeof value==='string'?value.slice(0,512):''});else a.push(url);
  }
  function bounded(a,f){
    if(!Array.isArray(a)){out.invalid=true;return;}
    if(a.length>10000)out.truncated=true;
    for(let i=0;i<Math.min(a.length,10000);i++){if(++steps>20000){out.truncated=true;break}f(a[i]);}
  }
  function owned(el){return parentVMs(el).some(v=>idValue(v.$store?.state?.item?.item?.item?.id)===out.productId);}
  function shown(el){for(let p=el,n=0;p&&n++<64;p=p.parentElement){const s=getComputedStyle(p);if(p.hidden||p.getAttribute('aria-hidden')==='true'||s.display==='none'||s.visibility==='hidden'||s.visibility==='collapse')return false}return true;}
  function nodeURLs(el){const set=new Set();const nodes=el.querySelectorAll('img');if(nodes.length>10000)out.truncated=true;
    for(let i=0;i<Math.min(nodes.length,10000);i++){if(++steps>20000){out.truncated=true;break}const n=nodes[i];for(const key of ['data-src','src']){const x=n.getAttribute(key);if(x&&x.length<=8192)set.add(x)}}return set;}
  const mainVM=vms.find(v=>Array.isArray(v.imageInfos));
  if(mainVM && Array.isArray(p.imageInfos)){
    out.mainReady=true;
    const onPage=nodeURLs(roots[0]), permitted=new Set();
    bounded(p.imageInfos,x=>{if(x && typeof x.url==='string')permitted.add(x.url);else out.invalid=true});
    bounded(mainVM.imageInfos,x=>{
      if(!x||typeof x.url!=='string'||!permitted.has(x.url)){out.invalid=true;return}
      if(x.mp4Url){out.incomplete=true;return}
      if(!onPage.has(x.url+'?x-oss-process=image/resize,l_124')&&!onPage.has(x.url)){out.incomplete=true;return}
      add(out.main,x.url);
    });
  }
  const sku=[...document.querySelectorAll('.sku,.new-sku')].filter(shown);
  if(sku.length===1 && owned(sku[0])){
    const v=parentVMs(sku[0]).find(x=>Array.isArray(x.groupedSkuAttrs));
    if(v){out.variantReady=true;const onPage=nodeURLs(sku[0]);
      bounded(v.groupedSkuAttrs,g=>{
        if(!g||!Array.isArray(g.skuAttributes)){out.invalid=true;return}
        bounded(g.skuAttributes,x=>{
          if(!x||typeof x!=='object'){out.invalid=true;return}
          if(!x.image)return;
          if(typeof x.image!=='string'||(!onPage.has(x.image+'?x-oss-process=image/resize,l_76')&&!onPage.has(x.image))){out.incomplete=true;return}
          add(out.variant,x.image,x.attrVal);
        });
      });
    }
  } else if(sku.length>1)out.invalid=true;
  const desc=document.querySelectorAll('.item-tab-detail-display');
  if(desc.length===1 && owned(desc[0])){
    out.descriptionReady=true;const imgs=desc[0].querySelectorAll('img');
    if(imgs.length>10000)out.truncated=true;
    for(let i=0;i<Math.min(imgs.length,10000);i++){
      if(++steps>20000){out.truncated=true;break}
      const raw=imgs[i].getAttribute('data-src');
      if(!raw){out.incomplete=true;continue}add(out.description,raw);
    }
  }else if(desc.length>1)out.invalid=true;
  else if(typeof p.itemDetail?.detail==='string' && !p.itemDetail.detail)out.descriptionReady=true;
  return out;
})()`

func zcygovCollectState(ctx context.Context, page *url.URL, state zcygovState) (Discovery, error) {
	result := Discovery{IsPage: true, Title: state.Title}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := zcygovID(page)
	current, err := crawlURL(nil, state.Location)
	if id == "" || page.Scheme != "https" || err != nil || !zcygovSameProduct(page, current) || state.ProductID != id {
		return result, errors.New("政采云商品尚未加载、需要验证或商品身份不匹配")
	}
	seen := map[string]bool{}
	invalid := state.Invalid
	add := func(raw, kind, label string) {
		image := zcygovImage(page, raw)
		if image == "" {
			invalid = true
			return
		}
		if seen[image] {
			return
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"zcygov_product_id": id, "image_kind": kind}}
		if label != "" {
			item.Metadata["sku_value"] = label
		}
		if ref := imageReferer(page.String(), image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	if state.MainReady {
		for _, raw := range state.Main {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			add(raw, "main", "")
		}
	}
	if state.VariantReady {
		for _, variant := range state.Variant {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			add(variant.URL, "sku", variant.Value)
		}
	}
	if state.DescriptionReady {
		for _, raw := range state.Description {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			add(raw, "description", "")
		}
	}
	var partial error
	if !state.MainReady || len(state.Main) == 0 {
		partial = errors.Join(partial, errors.New("政采云主图未完全加载，结果可能不完整"))
	}
	if !state.VariantReady || !state.DescriptionReady || state.Incomplete || state.Truncated || invalid {
		partial = errors.Join(partial, errors.New("政采云部分规格、详情或图片来源未确认，结果可能不完整"))
	}
	return result, partial
}

func discoverZcygov(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !isZcygovProduct(page) || getter == nil {
		return result, errors.New("政采云商品地址或下载客户端无效")
	}
	if page.Scheme != "https" {
		return result, errors.New("政采云请使用 HTTPS 商品地址")
	}
	return result, errors.New("政采云商品需在下载浏览器中打开后采集")
}

func zcygovCapture(ctx context.Context, session *RenderedSession, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := zcygovID(page)
	if id == "" || page.Scheme != "https" || session == nil {
		return result, errors.New("政采云商品地址或下载浏览器无效")
	}
	var state zcygovState
	for attempt := 0; attempt < 20; attempt++ {
		state = zcygovState{}
		if err := session.run(ctx, chromedp.Evaluate(zcygovStateScript, &state)); err != nil {
			return result, fmt.Errorf("读取政采云商品页面: %w", err)
		}
		current, err := crawlURL(nil, state.Location)
		if err != nil || !zcygovSameProduct(page, current) {
			return result, errors.New("政采云商品页面已跳转")
		}
		if state.ProductID != "" && state.ProductID != id {
			return result, errors.New("政采云商品身份不匹配")
		}
		if state.ProductID == id && state.MainReady && state.VariantReady && state.DescriptionReady {
			break
		}
		if attempt < 19 {
			if err := Wait(ctx, 250*time.Millisecond); err != nil {
				return result, err
			}
		}
	}
	return zcygovCollectState(ctx, page, state)
}
