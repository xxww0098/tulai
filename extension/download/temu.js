function tulaiTemuSnapshot() {
  const out = {url: location.href, title: document.title.slice(0, 1024), rows: [], missing: 0, truncated: false, blocked: false};
  const all = (root, selector) => {
    const nodes = root.querySelectorAll(selector), result = [];
    for (let i = 0; i < nodes.length; i++) result.push(nodes[i]);
    return result;
  };
  const visible = (node, image, stopAt) => {
    const rect = node.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) return false;
    let n = node, depth = 0;
    for (; n && depth < 100; n = n.parentElement, depth++) {
      if (n.hidden) return false;
      if (!image && n.getAttribute('aria-hidden') === 'true') return false;
      const style = getComputedStyle(n);
      if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse' || Number(style.opacity) === 0) return false;
      if (stopAt && n === stopAt) return true;
    }
    return !n;
  };
  const goodsIdRe = /^[1-9][0-9]{14,19}$/;
  const productSlugRe = /-g-([1-9][0-9]{14,19})\.html$/i;
  const goodsPathRe = /^\/(?:[a-z]{2,3}(?:-[a-z]{2,4})?\/)?goods\.html$/i;
  const kwcdnHostRe = /^[a-z0-9-]*img[a-z0-9-]*\.kwcdn\.com$/i;
  const parseTemuLink = raw => {
    if (!raw) return null;
    try {
      const u = new URL(raw, document.baseURI);
      if (!/^https?:$/.test(u.protocol) || !/(^|\.)temu\.com$/i.test(u.hostname)) return null;
      const slugMatch = productSlugRe.exec(u.pathname);
      if (slugMatch) return {id: slugMatch[1], href: u.href};
      if (goodsPathRe.test(u.pathname)) {
        const gid = (u.searchParams.get('goods_id') || u.searchParams.get('goodsId') || '').trim();
        if (goodsIdRe.test(gid)) return {id: gid, href: u.href};
      }
      return null;
    } catch (_) { return null; }
  };
  const parseGoodNameId = node => {
    const raw = node.getAttribute('id') || node.getAttribute('data-tooltip') || '';
    if (!raw.startsWith('goodName-')) return '';
    const id = raw.slice('goodName-'.length);
    return goodsIdRe.test(id) ? id : '';
  };
  const readImageUrl = img => {
    const src = img.currentSrc || img.src;
    if (img.complete && img.naturalWidth > 0 && /^https?:/i.test(src)) return src;
    for (const attr of ['data-src', 'data-original', 'data-lazy-src']) {
      const lazy = img.getAttribute(attr);
      if (!lazy) continue;
      try {
        const u = new URL(lazy, document.baseURI);
        if (/^https?:$/.test(u.protocol)) return u.href;
      } catch (_) {}
    }
    const srcset = img.getAttribute('srcset') || img.getAttribute('data-srcset') || '';
    if (srcset) {
      const first = srcset.split(',')[0].trim().split(/\s+/)[0];
      if (first) {
        try {
          const u = new URL(first, document.baseURI);
          if (/^https?:$/.test(u.protocol)) return u.href;
        } catch (_) {}
      }
    }
    return '';
  };
  const isKwcdnUrl = raw => {
    try { return kwcdnHostRe.test(new URL(raw).hostname); }
    catch (_) { return false; }
  };
  const pickPrimaryImage = (card, scopeFilter) => {
    const score = (img, url, preferred) => {
      const rect = img.getBoundingClientRect();
      const area = Math.max(1, rect.width * rect.height);
      const pathBonus = /\/(?:product|goods)\b/i.test(url) ? 1e9 : 0;
      const kwBonus = isKwcdnUrl(url) ? 5e8 : 0;
      const prefBonus = preferred ? 2e9 : 0;
      return prefBonus + pathBonus + kwBonus + area;
    };
    const collect = (selector, preferred, requireKwcdn) => {
      const found = [];
      const seenUrl = new Set();
      for (const img of all(card, selector)) {
        if (!scopeFilter(img) || !visible(img, true, card)) continue;
        const url = readImageUrl(img);
        if (!url || seenUrl.has(url)) continue;
        if (requireKwcdn && !isKwcdnUrl(url)) continue;
        seenUrl.add(url);
        found.push({url, rank: score(img, url, preferred)});
      }
      return found;
    };
    const extImgs = collect('img.goods-img-external, .goods-img-external img', true, false);
    if (extImgs.length === 1) return extImgs[0].url;
    if (extImgs.length > 1) {
      extImgs.sort((a, b) => b.rank - a.rank);
      return extImgs[0].url;
    }
    // If card explicitly has .goods-img-external image nodes that are still unloaded, do not grab unrelated icons.
    if (all(card, 'img.goods-img-external, .goods-img-external img').some(scopeFilter)) return '';
    const fallback = collect('img', false, true);
    if (fallback.length === 0) return '';
    fallback.sort((a, b) => b.rank - a.rank);
    return fallback[0].url;
  };
  for (const node of all(document, 'form, [role="dialog"], iframe')) {
    const text = (node.innerText || '') + ' ' + (node.getAttribute('title') || '');
    const verification = /security verification|captcha|verify you are human|安全验证/i.test(text);
    const login = node.tagName !== 'IFRAME' && /\bsign\s*in\b|\blog\s*in\b|请先登录/i.test(text);
    if (visible(node, false) && (verification || login)) {
      out.blocked = true; return out;
    }
  }
  const classicCards = all(document, '.js-search-goodsList .EKDT7a3v');
  if (classicCards.length > 0) {
    if (classicCards.length > 10000) out.truncated = true;
    for (let i = 0; i < classicCards.length && i < 10000; i++) {
      const card = classicCards[i];
      if (!visible(card, false)) continue;
      const inCard = n => n.closest('.EKDT7a3v') === card;
      const image = pickPrimaryImage(card, inCard);
      if (!image) continue;
      const names = all(card, '[id^="goodName-"], [data-tooltip^="goodName-"]').filter(n => inCard(n) && visible(n, false));
      let id = '', title = '';
      if (names.length === 1) {
        const name = names[0];
        id = parseGoodNameId(name);
        if (!id) { out.missing++; continue; }
        title = (name.getAttribute('title') || name.getAttribute('data-tooltip-title') || name.textContent || '').trim();
      } else if (names.length > 1) {
        out.missing++; continue;
      }
      const parsedLinks = all(card, 'a[href]').filter(inCard).map(n => ({node: n, parsed: parseTemuLink(n.href)})).filter(x => x.parsed);
      if (!id) {
        const linkIds = [...new Set(parsedLinks.map(x => x.parsed.id))];
        if (linkIds.length !== 1) { out.missing++; continue; }
        id = linkIds[0];
      }
      const matchingLinks = parsedLinks.filter(x => x.parsed.id === id);
      if (matchingLinks.length === 0) { out.missing++; continue; }
      if (!title) {
        const heading = all(card, 'h2, h3, [title]').find(n => inCard(n) && visible(n, false) && (n.getAttribute('title') || n.textContent || '').trim());
        title = heading ? (heading.getAttribute('title') || heading.textContent || '').trim() : (matchingLinks[0].node.getAttribute('title') || matchingLinks[0].node.getAttribute('aria-label') || matchingLinks[0].node.textContent || '').trim();
      }
      if (out.rows.length >= 10000) { out.truncated = true; break; }
      out.rows.push({id, title: title.slice(0, 1024), image, link: matchingLinks[0].parsed.href});
    }
    return out;
  }

  // Structural fallback when Temu rotates obfuscated CSS module classes (e.g. .EKDT7a3v).
  const root = document.querySelector('.js-search-goodsList') ||
    document.querySelector('[class*="search-goodsList"], [class*="goodsList"], main') ||
    document.body;
  if (!root) return out;
  const isIgnored = n => Boolean(n.closest('header, nav, footer, [role="navigation"], [role="dialog"]'));
  const anchors = [];
  const ownerIds = new Map();
  const markAncestors = (node, id) => {
    let cur = node, depth = 0;
    for (; cur && cur !== root && cur !== document.body && cur !== document.documentElement && depth < 15; cur = cur.parentElement, depth++) {
      let set = ownerIds.get(cur);
      if (!set) { set = new Set(); ownerIds.set(cur, set); }
      if (set.size < 2) set.add(id);
    }
  };
  for (const nameNode of all(root, '[id^="goodName-"], [data-tooltip^="goodName-"]')) {
    if (isIgnored(nameNode)) continue;
    const id = parseGoodNameId(nameNode);
    if (!id) continue;
    anchors.push({id, node: nameNode, kind: 'name'});
    markAncestors(nameNode, id);
  }
  for (const a of all(root, 'a[href*="-g-"], a[href*="goods_id="], a[href*="goodsId="]')) {
    if (isIgnored(a)) continue;
    const parsed = parseTemuLink(a.href);
    if (!parsed) continue;
    anchors.push({id: parsed.id, node: a, kind: 'link', href: parsed.href});
    markAncestors(a, parsed.id);
  }
  const seenCard = new Set();
  const seenId = new Set();
  for (const anchor of anchors) {
    if (seenId.has(anchor.id)) continue;
    let card = null, cur = anchor.node, depth = 0;
    for (; cur && cur !== root && cur !== document.body && cur !== document.documentElement && depth < 15; cur = cur.parentElement, depth++) {
      const ids = ownerIds.get(cur);
      if (!ids || ids.size !== 1) break;
      card = cur;
    }
    if (!card || seenCard.has(card)) continue;
    seenCard.add(card);
    if (!visible(card, false)) continue;
    const image = pickPrimaryImage(card, () => true);
    if (!image) continue;
    const nameCandidates = (card.matches && card.matches('[id^="goodName-"], [data-tooltip^="goodName-"]') ? [card] : []).concat(all(card, '[id^="goodName-"], [data-tooltip^="goodName-"]'));
    const nameNodes = nameCandidates.filter(n => visible(n, false));
    let id = anchor.id, title = '';
    if (nameNodes.length === 1) {
      const nid = parseGoodNameId(nameNodes[0]);
      if (!nid) { out.missing++; continue; }
      id = nid;
      title = (nameNodes[0].getAttribute('title') || nameNodes[0].getAttribute('data-tooltip-title') || nameNodes[0].textContent || '').trim();
    } else if (nameNodes.length > 1) {
      out.missing++; continue;
    }
    const linkCandidates = (card.matches && card.matches('a[href]') ? [card] : []).concat(all(card, 'a[href]'));
    const matchingLinks = linkCandidates
      .map(n => ({node: n, parsed: parseTemuLink(n.href)}))
      .filter(x => x.parsed && x.parsed.id === id);
    if (matchingLinks.length === 0) { out.missing++; continue; }
    if (!title) {
      const heading = all(card, 'h2, h3, [title]').find(n => visible(n, false) && (n.getAttribute('title') || n.textContent || '').trim());
      title = heading
        ? (heading.getAttribute('title') || heading.textContent || '').trim()
        : (matchingLinks[0].node.getAttribute('title') || matchingLinks[0].node.getAttribute('aria-label') || matchingLinks[0].node.textContent || '').trim();
    }
    if (!title) {
      const altImg = all(card, 'img[alt]').find(n => (n.getAttribute('alt') || '').trim());
      if (altImg) title = altImg.getAttribute('alt').trim();
    }
    if (out.rows.length >= 10000) { out.truncated = true; break; }
    seenId.add(id);
    out.rows.push({id, title: title.slice(0, 1024), image, link: matchingLinks[0].parsed.href});
  }
  return out;
}
