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
	soukuanIDPath   = regexp.MustCompile(`^/item/([a-fA-F0-9]{24}|[1-9][0-9]{0,18})$`)
	soukuanIntlPath = regexp.MustCompile(`^/en/item/([a-fA-F0-9]{24}|[1-9][0-9]{0,18})$`)
)

func soukuanRoute(u *url.URL) (string, string) {
	if u == nil || len(u.String()) > 8192 || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		u.RawPath != "" || (!strings.EqualFold(u.Hostname(), "www.vvic.com") && !strings.EqualFold(u.Hostname(), "vvic.com")) {
		return "", ""
	}
	if m := soukuanIDPath.FindStringSubmatch(u.Path); m != nil {
		return "item", strings.ToLower(m[1])
	}
	if m := soukuanIntlPath.FindStringSubmatch(u.Path); m != nil {
		return "unsupported", strings.ToLower(m[1])
	}
	return "", ""
}

func isSoukuanProduct(u *url.URL) bool {
	kind, _ := soukuanRoute(u)
	return kind != ""
}

func soukuanSameProduct(a, b *url.URL) bool {
	ak, ai := soukuanRoute(a)
	bk, bi := soukuanRoute(b)
	return ak != "" && ak == bk && ai == bi && sameOrigin(a, b)
}

// Only return site-provided image addresses from independently observed hosts.
// Do not remove resize parameters, manufacture originals, or follow video URLs.
func soukuanImage(page *url.URL, raw string) string {
	if len(raw) > 8192 {
		return ""
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") ||
		strings.Contains(u.Path, "..") || strings.Contains(u.Path, "\\") {
		return ""
	}
	switch strings.ToLower(u.Hostname()) {
	case "img1.vvic.com":
		if !strings.HasPrefix(u.Path, "/upload/") {
			return ""
		}
	case "img.vvic.com":
	case "img.alicdn.com":
		if !strings.HasPrefix(u.Path, "/imgextra/") && !strings.HasPrefix(u.Path, "/bao/uploaded/") {
			return ""
		}
	default:
		return ""
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".bmp":
	default:
		return ""
	}
	return u.String()
}

type soukuanVariant struct {
	URL   string `json:"url"`
	Value string `json:"value"`
}

type soukuanState struct {
	Location        string           `json:"location"`
	ProductID       string           `json:"productId"`
	RouteID         string           `json:"routeId"`
	Title           string           `json:"title"`
	ProtectionKnown bool             `json:"protectionKnown"`
	Protected       bool             `json:"protected"`
	MainReady       bool             `json:"mainReady"`
	VariantReady    bool             `json:"variantReady"`
	DescReady       bool             `json:"descReady"`
	DescIncomplete  bool             `json:"descIncomplete"`
	MainIncomplete  bool             `json:"mainIncomplete"`
	SKUIncomplete   bool             `json:"skuIncomplete"`
	Invalid         bool             `json:"invalid"`
	Truncated       bool             `json:"truncated"`
	Main            []string         `json:"main"`
	Variant         []soukuanVariant `json:"variant"`
	Description     []string         `json:"description"`
}

// Inspect only displayed product image nodes and their owning public product
// fields. Do not read browser storage, cookies, credentials, or private APIs.
const soukuanStateScript = `(() => {
  const loc=location.href.length<=8192?location.href:'';
  const out={location:loc,productId:'',routeId:'',title:'',protectionKnown:false,protected:false,
    mainReady:false,variantReady:false,descReady:false,descIncomplete:false,mainIncomplete:false,skuIncomplete:false,
    invalid:false,truncated:false,main:[],variant:[],description:[]};
  const expected=location.pathname.split('/').pop().toLowerCase();
  const title=document.querySelector('.detail-title');
  if (title) out.title=(title.textContent||'').trim().slice(0,4096);
  const findVM=el=>{
    for (let p=el,i=0;p&&i++<32;p=p.parentElement) {
      const v=p.__vue__, item=v?.item, vid=String(item?.vid||'').toLowerCase();
      const route=String(v?.$route?.params?.vid||'').toLowerCase();
      if (item && vid===expected && route===expected) return v;
    }
    return null;
  };
  const roots=[...document.querySelectorAll('#thumblist')];
  if (roots.length!==1) return out;
  const root=roots[0], vm=findVM(root);
  if (!vm) return out;
  const item=vm.item;
  out.productId=String(item.vid||'').toLowerCase().slice(0,64);
  out.routeId=String(vm.$route.params.vid||'').toLowerCase().slice(0,64);
  out.protectionKnown=Object.prototype.hasOwnProperty.call(item,'isTheft') &&
    (item.isTheft===0 || item.isTheft==='0' || item.isTheft===false || item.isTheft===1 || item.isTheft==='1' || item.isTheft===true);
  function shown(el) {
    for (let p=el,i=0;p&&i++<64;p=p.parentElement) {
      const style=getComputedStyle(p);
      if (p.hidden || p.getAttribute('aria-hidden')==='true' || style.display==='none' || style.visibility==='hidden' || style.visibility==='collapse' || style.opacity==='0') return false;
    }
    return true;
  }
  out.protected=item.isTheft===1 || item.isTheft==='1' || item.isTheft===true ||
    [...document.querySelectorAll('.theft,.tb-pic-theft,.sp-hide-mask,.sp-hide-pic')].some(shown);
  if (!out.protectionKnown || out.protected) return out;
  let steps=0,bytes=0;
  function bounded(a,fn) {
    if (a == null) return;
    if (!Array.isArray(a)) {out.invalid=true;return;}
    if (a.length>10000) out.truncated=true;
    for (let i=0;i<Math.min(a.length,10000)&&!out.truncated;i++) {
      if (++steps>20000) {out.truncated=true;break;} fn(a[i]);
    }
  }
  function add(arr,value,label) {
    if (typeof value!=='string' || !value || value.length>8192) {out.invalid=true;return;}
    if (++steps>20000 || (bytes+=value.length)>2097152 || arr.length>=10000) {out.truncated=true;return;}
    if (arr===out.variant) arr.push({url:value,value:typeof label==='string'?label.slice(0,512):''});
    else arr.push(value);
  }
  const mainList=[], skuList=[];
  out.mainReady=Array.isArray(item.imgs);
  bounded(item.imgs,x=>{if(typeof x==='string'&&x)mainList.push(x);else if(x!=null&&x!=='')out.invalid=true;});
  out.variantReady=Array.isArray(item.color_pics) || item.color_pics==null;
  bounded(item.color_pics,x=>{if(typeof x==='string'&&x)skuList.push(x);else if(x!=null&&x!=='')out.invalid=true;});
  const mainSet=new Set(mainList), skuSet=new Set(skuList);
  const nodes=root.querySelectorAll('.owl-item.active img[big]');
  if(nodes.length>10000)out.truncated=true;
  for (let i=0;i<Math.min(nodes.length,10000)&&!out.truncated;i++) {
    const n=nodes[i];
    if (!shown(n)) continue;
    const src=n.getAttribute('big');
    if (!mainSet.has(src)) {out.invalid=true;continue;}
    add(out.main,src);
  }
  const sku=document.querySelectorAll('#skuInfo');
  if(sku.length>1)out.invalid=true;
  const skuVM=sku.length===1?findVM(sku[0]):null;
  if(skuVM && (skuVM.item.isTheft===1 || skuVM.item.isTheft==='1' || skuVM.item.isTheft===true)) {out.protected=true;return out;}
  if(skuVM && skuVM.item.isTheft!==0 && skuVM.item.isTheft!=='0' && skuVM.item.isTheft!==false) {out.variantReady=false;}
  else if(skuVM) {
    const opts=sku[0].querySelectorAll('.value.color-choice img[big]');
    if(opts.length>10000)out.truncated=true;
    for(let i=0;i<Math.min(opts.length,10000)&&!out.truncated;i++){
      const n=opts[i]; if(!shown(n))continue;
      const src=n.getAttribute('big');
      if(!skuSet.has(src)){out.invalid=true;continue;}
      add(out.variant,src,n.getAttribute('alt'));
    }
  } else if(skuList.length) out.variantReady=false;
  out.mainIncomplete=new Set(out.main).size<mainSet.size;
  out.skuIncomplete=new Set(out.variant.map(x=>x.url)).size<skuSet.size;
  const desc=document.querySelectorAll('.detail-desc');
  if(desc.length>1)out.invalid=true;
  const descVM=desc.length===1?findVM(desc[0]):null;
  if(descVM && (descVM.item.isTheft===1 || descVM.item.isTheft==='1' || descVM.item.isTheft===true)) {out.protected=true;return out;}
  if(descVM && (descVM.item.isTheft===0 || descVM.item.isTheft==='0' || descVM.item.isTheft===false) && item.item_desc && typeof item.item_desc.desc==='string') {
    out.descReady=true;
    const images=desc[0].querySelectorAll('img');
    if(images.length>10000)out.truncated=true;
    for(let i=0;i<Math.min(images.length,10000)&&!out.truncated;i++){
      const n=images[i], src=n.getAttribute('data-src');
      if(!shown(n) || !src){out.descIncomplete=true;continue;}
      add(out.description,src);
    }
  }
  return out;
})()`

func soukuanCollectState(ctx context.Context, page *url.URL, state soukuanState) (Discovery, error) {
	result := Discovery{IsPage: true, Title: state.Title}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	kind, id := soukuanRoute(page)
	current, err := crawlURL(nil, state.Location)
	if kind != "item" || page.Scheme != "https" || err != nil || !soukuanSameProduct(page, current) ||
		!strings.EqualFold(state.ProductID, id) || !strings.EqualFold(state.RouteID, id) {
		return result, errors.New("搜款网商品页面尚未加载、需要验证或商品身份不匹配")
	}
	if !state.ProtectionKnown || state.Protected {
		return result, errors.New("搜款网商品的图片访问权限无法确认或处于保护状态")
	}
	seen := map[string]bool{}
	invalid := state.Invalid
	add := func(raw, category, label string) {
		image := soukuanImage(page, raw)
		if image == "" {
			invalid = true
			return
		}
		if seen[image] {
			return
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"soukuan_product_id": id, "image_kind": category}}
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
		for _, image := range state.Variant {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			add(image.URL, "sku", image.Value)
		}
	}
	if state.DescReady {
		for _, raw := range state.Description {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			add(raw, "description", "")
		}
	}
	var partial error
	if !state.MainReady || len(state.Main) == 0 || state.MainIncomplete {
		partial = errors.Join(partial, errors.New("搜款网主图未完全显示，结果可能不完整"))
	}
	if !state.VariantReady || state.SKUIncomplete {
		partial = errors.Join(partial, errors.New("搜款网规格图未完全显示，结果可能不完整"))
	}
	if !state.DescReady || state.DescIncomplete {
		partial = errors.Join(partial, errors.New("搜款网详情图尚未加载，结果可能不完整"))
	}
	if invalid || state.Truncated {
		partial = errors.Join(partial, errors.New("搜款网部分图片地址、结构或数量无法确认，结果可能不完整"))
	}
	if len(result.Items) == 0 && partial == nil {
		partial = errors.New("搜款网未返回可确认的商品图片")
	}
	return result, partial
}

func discoverSoukuan(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	kind, _ := soukuanRoute(page)
	if kind == "" || getter == nil {
		return result, errors.New("搜款网商品地址或下载客户端无效")
	}
	if kind == "unsupported" {
		return result, errors.New("搜款网此语言的商品页面尚未验证")
	}
	if page.Scheme != "https" {
		return result, errors.New("搜款网请使用 HTTPS 商品地址")
	}
	return result, errors.New("搜款网商品需在下载浏览器中打开后采集")
}

func soukuanCapture(ctx context.Context, session *RenderedSession, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	kind, id := soukuanRoute(page)
	if kind == "unsupported" {
		return result, errors.New("搜款网此语言的商品页面尚未验证")
	}
	if kind != "item" || page.Scheme != "https" || session == nil {
		return result, errors.New("搜款网商品地址或下载浏览器无效")
	}
	var state soukuanState
	for attempt := 0; attempt < 16; attempt++ {
		state = soukuanState{}
		if err := session.run(ctx, chromedp.Evaluate(soukuanStateScript, &state)); err != nil {
			return result, fmt.Errorf("读取搜款网商品页面: %w", err)
		}
		current, err := crawlURL(nil, state.Location)
		if err != nil || !soukuanSameProduct(page, current) {
			return result, errors.New("搜款网商品页面已跳转")
		}
		if state.ProductID != "" && !strings.EqualFold(state.ProductID, id) || state.RouteID != "" && !strings.EqualFold(state.RouteID, id) {
			return result, errors.New("搜款网商品身份不匹配")
		}
		if state.Protected || (state.ProductID != "" && !state.ProtectionKnown) {
			return result, errors.New("搜款网商品的图片访问权限无法确认或处于保护状态")
		}
		if strings.EqualFold(state.ProductID, id) && strings.EqualFold(state.RouteID, id) && state.MainReady && state.VariantReady && state.DescReady {
			break
		}
		if attempt < 15 {
			if err := Wait(ctx, 250*time.Millisecond); err != nil {
				return result, err
			}
		}
	}
	return soukuanCollectState(ctx, page, state)
}
