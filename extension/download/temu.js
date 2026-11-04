function tulaiTemuSnapshot() {
  const out = {url: location.href, title: document.title.slice(0, 1024), rows: [], missing: 0, truncated: false, blocked: false};
  const all = (root, selector) => {
    const nodes = root.querySelectorAll(selector), result = [];
    for (let i = 0; i < nodes.length; i++) result.push(nodes[i]);
    return result;
  };
  const visible = (node, image) => {
    const rect = node.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) return false;
    let n = node, depth = 0;
    for (; n && depth < 100; n = n.parentElement, depth++) {
      if (n.hidden || (n.getAttribute('aria-hidden') === 'true' && !(image && n === node))) return false;
      const style = getComputedStyle(n);
      if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse' || Number(style.opacity) === 0) return false;
    }
    return !n;
  };
  for (const node of all(document, 'form, [role="dialog"], iframe')) {
    const text = (node.innerText || '') + ' ' + (node.getAttribute('title') || '');
    const verification = /security verification|captcha|verify you are human|安全验证/i.test(text);
    const login = node.tagName !== 'IFRAME' && /\bsign\s*in\b|\blog\s*in\b|请先登录/i.test(text);
    if (visible(node, false) && (verification || login)) {
      out.blocked = true; return out;
    }
  }
  const cards = document.querySelectorAll('.js-search-goodsList .EKDT7a3v');
  if (cards.length > 10000) out.truncated = true;
  for (let i = 0; i < cards.length && i < 10000; i++) {
    const card = cards[i];
    if (!visible(card, false)) continue;
    const images = [...new Set(all(card, 'img.goods-img-external, .goods-img-external img')
      .filter(n => n.closest('.EKDT7a3v') === card && visible(n, true))
      .map(n => {
        const src = n.currentSrc || n.src;
        if (n.complete && n.naturalWidth > 0 && /^https?:/i.test(src)) return src;
        const lazy = n.getAttribute('data-src');
        if (!lazy) return '';
        try { const u = new URL(lazy, document.baseURI); return /^https?:$/.test(u.protocol) ? u.href : ''; }
        catch (_) { return ''; }
      }).filter(Boolean))];
    if (images.length === 0) continue;
    if (images.length !== 1) { out.missing++; continue; }
    const names = all(card, '[id^="goodName-"], [data-tooltip^="goodName-"]').filter(n => n.closest('.EKDT7a3v') === card && visible(n, false));
    if (names.length !== 1) { out.missing++; continue; }
    const name = names[0], id = (name.getAttribute('id') || name.getAttribute('data-tooltip') || '').slice('goodName-'.length);
    if (!/^[1-9][0-9]{14,19}$/.test(id)) { out.missing++; continue; }
    const links = all(card, 'a[href]').filter(n => n.closest('.EKDT7a3v') === card)
      .map(n => n.href).filter(raw => {
        try { const u = new URL(raw); return /(^|\.)temu\.com$/i.test(u.hostname) && u.pathname.endsWith('-g-' + id + '.html'); }
        catch (_) { return false; }
      });
    if (links.length === 0) { out.missing++; continue; }
    if (out.rows.length >= 10000) { out.truncated = true; break; }
    out.rows.push({id, title: (name.getAttribute('title') || name.getAttribute('data-tooltip-title') || name.textContent || '').trim().slice(0, 1024), image: images[0], link: links[0]});
  }
  return out;
}
