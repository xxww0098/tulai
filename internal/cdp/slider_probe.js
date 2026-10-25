function xbsProbeSlider() {
  function shown(el) {
    if (!el || !el.getBoundingClientRect) return false
    const r = el.getBoundingClientRect()
    if (r.width < 8 || r.height < 8) return false
    if (r.bottom < 0 || r.right < 0) return false
    let view = null
    try { view = el.ownerDocument.defaultView } catch (e) { view = null }
    if (!view || !view.getComputedStyle) return true
    const s = view.getComputedStyle(el)
    if (!s) return true
    if (s.display === 'none' || s.visibility === 'hidden' || Number(s.opacity) === 0) return false
    return true
  }

  function pick(doc, selectors, maxW, maxH) {
    for (let i = 0; i < selectors.length; i++) {
      let nodes = []
      try { nodes = doc.querySelectorAll(selectors[i]) } catch (e) { nodes = [] }
      for (let j = 0; j < nodes.length; j++) {
        const el = nodes[j]
        if (!shown(el)) continue
        const r = el.getBoundingClientRect()
        if (maxW && r.width > maxW) continue
        if (maxH && r.height > maxH) continue
        return el
      }
    }
    return null
  }

  function box(el) {
    const r = el.getBoundingClientRect()
    return { x: r.left, y: r.top, w: r.width, h: r.height }
  }

  function shiftBox(b, ox, oy) {
    if (!b) return null
    return { x: b.x + ox, y: b.y + oy, w: b.w, h: b.h }
  }

  function loaded(el) {
    const tag = String(el.tagName || '').toUpperCase()
    if (tag !== 'IMG') return true
    return !!(el.complete && el.naturalWidth > 1 && el.naturalHeight > 1)
  }

  function grab(el) {
    if (!el) return null
    const r = el.getBoundingClientRect()
    const naturalW = el.naturalWidth || el.width || r.width
    const naturalH = el.naturalHeight || el.height || r.height
    let b64 = ''
    // 没解码完的 img 画到 canvas 上是空白，会把缺口置信度打成 0。等加载完再截，否则只留 src。
    try {
      if (String(el.tagName).toUpperCase() === 'CANVAS' && el.toDataURL) {
        const url = el.toDataURL('image/png')
        const cut = url.indexOf(',')
        if (cut > 0 && url.length < 1500000) b64 = url.slice(cut + 1)
      } else if (loaded(el) && naturalW > 1 && naturalH > 1) {
        const c = el.ownerDocument.createElement('canvas')
        c.width = naturalW
        c.height = naturalH
        c.getContext('2d').drawImage(el, 0, 0)
        const url = c.toDataURL('image/png')
        const cut = url.indexOf(',')
        if (cut > 0 && url.length < 1500000) b64 = url.slice(cut + 1)
      }
    } catch (e) {}
    let src = ''
    try { src = String(el.currentSrc || el.src || '') } catch (e) { src = '' }
    if (src.length > 500) src = src.slice(0, 500)
    return {
      x: r.left, y: r.top, w: r.width, h: r.height,
      naturalW: naturalW, naturalH: naturalH, b64: b64, src: src,
    }
  }

  function labelOf(el) {
    let node = el
    for (let i = 0; i < 7 && node; i++) {
      const text = String(node.innerText || '').replace(/\s+/g, ' ').trim()
      if (text && text.length < 500) return text
      node = node.parentElement
    }
    return ''
  }

  function solved(text, handle) {
    if (/验证通过|验证成功|verification (?:passed|successful)|verified successfully/i.test(text || '')) return true
    const cls = String((handle && handle.className) || '')
    return /(^|\s)(ok|verified)(\s|$)/i.test(cls)
  }

  function trackFor(doc, handle) {
    const known = pick(doc, [
      '#aliyunCaptcha-sliding-body',
      '#aliyunCaptcha-sliding-wrapper',
      '.scratch-captcha-slider',
      '.nc_scale',
      '[id$="_n1t"]',
      '#nc_1__scale_text',
    ], 0, 160)
    if (known) {
      const b = box(known)
      const h = box(handle)
      if (b.w >= h.w + 20) return known
    }
    let node = handle.parentElement
    const h = box(handle)
    for (let i = 0; i < 6 && node; i++) {
      const b = box(node)
      if (b.w >= h.w + 30 && b.h <= 160 && b.h >= 8) return node
      node = node.parentElement
    }
    return handle.parentElement || handle
  }

  function listFrames(doc) {
    const out = []
    let nodes = []
    try { nodes = doc.querySelectorAll('iframe,frame') } catch (e) { nodes = [] }
    for (let i = 0; i < nodes.length; i++) {
      const fr = nodes[i]
      const r = fr.getBoundingClientRect()
      if (r.width < 20 || r.height < 20) continue
      let src = ''
      try { src = String(fr.src || (fr.getAttribute && fr.getAttribute('src')) || '') } catch (e) { src = '' }
      if (src.length > 500) src = src.slice(0, 500)
      out.push({ src: src, x: r.left, y: r.top, w: r.width, h: r.height })
    }
    return out
  }

  function probeDoc(doc) {
    const handle = pick(doc, [
      '#aliyunCaptcha-sliding-slider',
      '#scratch-captcha-btn',
      '#nc_1_n1z',
      '[id^="nc_"][id$="_n1z"]',
      '.btn_slide',
      '.nc_iconfont.btn_slide',
      '#nocaptcha .button',
      '.scratch-captcha-slider .button',
    ], 160, 160)
    if (!handle) return null
    const text = labelOf(handle)
    if (solved(text, handle)) return { found: false, passed: true, text: text }
    const track = trackFor(doc, handle)
    const bg = pick(doc, [
      '#aliyunCaptcha-img',
      '.scratch-captcha-question-bg',
      'img.scratch-captcha-question-bg',
    ], 0, 0)
    const piece = pick(doc, [
      '#aliyunCaptcha-puzzle',
      '.scratch-captcha-question-img',
      'img.scratch-captcha-question-img',
    ], 0, 0)
    const hb = box(handle)
    const tb = box(track)
    const bgGrab = grab(bg)
    const pieceGrab = grab(piece)
    let pieceLeft = 0
    if (bgGrab && pieceGrab) pieceLeft = pieceGrab.x - bgGrab.x
    return {
      found: true,
      passed: false,
      text: text,
      handle: hb,
      track: tb,
      pieceLeft: pieceLeft,
      bg: bgGrab,
      piece: pieceGrab,
    }
  }

  function shiftHit(hit, ox, oy) {
    if (!hit) return null
    if (hit.passed) return hit
    hit.handle = shiftBox(hit.handle, ox, oy)
    hit.track = shiftBox(hit.track, ox, oy)
    if (hit.bg) {
      hit.bg.x += ox
      hit.bg.y += oy
    }
    if (hit.piece) {
      hit.piece.x += ox
      hit.piece.y += oy
    }
    return hit
  }

  function walk(doc, ox, oy, depth) {
    if (!doc || depth > 5) return null
    const hit = probeDoc(doc)
    if (hit && (hit.found || hit.passed)) return shiftHit(hit, ox, oy)
    let nodes = []
    try { nodes = doc.querySelectorAll('iframe,frame') } catch (e) { nodes = [] }
    for (let i = 0; i < nodes.length; i++) {
      const fr = nodes[i]
      let child = null
      try { child = fr.contentDocument } catch (e) { child = null }
      if (!child) continue
      const r = fr.getBoundingClientRect()
      const found = walk(child, ox + r.left, oy + r.top, depth + 1)
      if (found) return found
    }
    return null
  }

  const doc = document
  const hit = walk(doc, 0, 0, 0) || { found: false, passed: false }
  hit.iframes = listFrames(doc)
  return hit
}
