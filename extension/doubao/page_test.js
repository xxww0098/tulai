// 用最小 DOM 夹具核对 page.js 的锚点。豆包锚点来自 internal/strategy/js/ego.js：upload_file_button、上传文件或图片、mdbox_image。
const assert = require('assert')
const fs = require('fs')
const path = require('path')
const vm = require('vm')

globalThis.getComputedStyle = (el) => ({
  display: (el && el.display) || 'block',
  visibility: (el && el.visibility) || 'visible',
})

class El {
  constructor(tag, attrs) {
    this.nodeType = 1
    this.tagName = String(tag || 'div').toUpperCase()
    this.attrs = Object.assign({}, attrs || {})
    this.children = []
    this.childNodes = []
    this.parentElement = null
    this.innerText = ''
    this.textContent = ''
    this.value = ''
    this.disabled = false
    this.isConnected = true
    this.complete = false
    this.naturalWidth = 0
    this.src = ''
    this.currentSrc = ''
    this.id = this.attrs.id || ''
    this.className = this.attrs.class || ''
    this.rect = { width: 80, height: 24, left: 0, top: 0 }
    this.files = null
  }

  getAttribute(name) {
    if (name === 'id') return this.id || null
    if (name === 'class') return this.className || null
    if (this.attrs[name] == null || this.attrs[name] === false) return null
    return String(this.attrs[name])
  }

  getBoundingClientRect() {
    return this.rect
  }

  contains(el) {
    for (let node = el; node; node = node.parentElement) if (node === this) return true
    return false
  }

  matches(sel) {
    return matchSelector(this, sel)
  }

  querySelectorAll(sel) {
    return descendants(this).filter((el) => matchSelector(el, sel))
  }

  querySelector(sel) {
    return this.querySelectorAll(sel)[0] || null
  }

  append(child) {
    child.parentElement = this
    this.children.push(child)
    this.childNodes.push(child)
    return child
  }

  addText(text) {
    this.childNodes.push({ nodeType: 3, nodeValue: text })
    this.innerText += text
    this.textContent += text
    return this
  }

  focus() {}
  dispatchEvent() {}
  click() { this.clicked = (this.clicked || 0) + 1 }
}

function descendants(root) {
  const out = []
  function walk(node) {
    for (const child of node.children) {
      out.push(child)
      walk(child)
    }
  }
  walk(root)
  return out
}

function splitComma(selector) {
  const parts = []
  let buf = ''
  let quote = ''
  for (const ch of selector) {
    if (quote) {
      buf += ch
      if (ch === quote) quote = ''
      continue
    }
    if (ch === '"' || ch === "'") {
      quote = ch
      buf += ch
      continue
    }
    if (ch === ',') {
      parts.push(buf)
      buf = ''
      continue
    }
    buf += ch
  }
  if (buf) parts.push(buf)
  return parts
}

function readIdent(source, index) {
  let end = index
  while (end < source.length && source[end] !== '#' && source[end] !== '.' && source[end] !== '[' && source[end] !== ' ') end++
  return { token: source.slice(index, end), next: end }
}

function matchCompound(el, raw) {
  const compound = raw.trim()
  if (!compound) return false
  let index = 0
  while (index < compound.length) {
    const ch = compound[index]
    if (ch === '#') {
      const ident = readIdent(compound, index + 1)
      if ((el.id || '') !== ident.token) return false
      index = ident.next
      continue
    }
    if (ch === '.') {
      const ident = readIdent(compound, index + 1)
      const classes = String(el.className || '').split(/\s+/).filter(Boolean)
      if (!classes.includes(ident.token)) return false
      index = ident.next
      continue
    }
    if (ch === '[') {
      const end = compound.indexOf(']', index)
      if (end < 0) return false
      const body = compound.slice(index + 1, end)
      const matched = body.match(/^([^^$*~|=]+)(\^=|\*=|=)?(?:"([^"]*)"|'([^']*)'|([^]*))?$/)
      if (!matched) return false
      const got = el.getAttribute(matched[1])
      if (got == null) return false
      const op = matched[2] || ''
      const value = matched[3] ?? matched[4] ?? matched[5] ?? ''
      if (op === '=' && got !== value) return false
      if (op === '^=' && !String(got).startsWith(value)) return false
      if (op === '*=' && !String(got).includes(value)) return false
      index = end + 1
      continue
    }
    const ident = readIdent(compound, index)
    if (!ident.token || el.tagName !== ident.token.toUpperCase()) return false
    index = ident.next
  }
  return true
}

function matchSelector(el, selector) {
  return splitComma(selector).some((part) => matchCompound(el, part))
}

function makeDoc(hostname) {
  const doc = new El('document')
  doc.hostname = hostname
  doc.body = new El('body')
  doc.append(doc.body)
  doc.querySelectorAll = (sel) => descendants(doc).filter((el) => matchSelector(el, sel))
  doc.querySelector = (sel) => doc.querySelectorAll(sel)[0] || null
  return doc
}


vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', 'protocol.js'), 'utf8'))
vm.runInThisContext(fs.readFileSync(path.join(__dirname, 'page.js'), 'utf8'))
const Page = globalThis.XBSPage
const EMPTY_SVG = "<svg xmlns='http://www.w3.org/2000/svg' version='1.1' width='1536' height='1536'/>"
const EMPTY_SVG_URL = 'data:image/svg+xml,' + EMPTY_SVG

function surface() {
  const doc = makeDoc('www.doubao.com')
  const old = doc.body.append(new El('div'))
  const oldInput = old.append(new El('input', { type: 'file', id: 'old' }))
  oldInput.id = 'old'
  const twin = doc.body.append(new El('button', { 'data-testid': 'upload_file_button', id: 'twin' }))
  twin.id = 'twin'
  const wrap = doc.body.append(new El('div', { class: 'guidance-input-surface' }))
  wrap.className = 'guidance-input-surface'
  const box = wrap.append(new El('textarea'))
  const entry = wrap.append(new El('button', { 'data-testid': 'upload_file_button', id: 'entry' }))
  entry.id = 'entry'
  const hidden = wrap.append(new El('input', { type: 'file', id: 'new', accept: 'image/*' }))
  hidden.id = 'new'
  hidden.rect = { width: 0, height: 0, left: 0, top: 0 }
  return { doc, box, entry, hidden, twin }
}

function message(doc, role, text) {
  const el = doc.body.append(new El('div', { 'data-role': role }))
  if (text) el.addText(text)
  return el
}

{
  const { doc, hidden, entry, twin } = surface()
  const comp = Page.composer(doc)
  assert.strictEqual(Page.fileInput(doc, comp), hidden)
  assert.strictEqual(Page.uploadEntry(doc, comp), entry)
  assert.notStrictEqual(Page.uploadEntry(doc, comp), twin)
  boxSet(doc, '还没发送')
  assert.strictEqual(Page.unsentBlocksUpload(doc, comp), '')
}

function boxSet(doc, value) {
  Page.composer(doc).value = value
}

{
  const doc = makeDoc('www.doubao.com')
  const hiddenMenu = doc.body.append(new El('div', { role: 'listbox' }))
  hiddenMenu.rect = { width: 0, height: 0, left: 0, top: 0 }
  const stale = hiddenMenu.append(new El('div', { role: 'option' }))
  stale.addText('上传文件或图片')
  assert.strictEqual(Page.uploadImageItem(doc), null)
  const menu = doc.body.append(new El('div', { role: 'listbox' }))
  const other = menu.append(new El('div', { role: 'option' }))
  other.addText('上传文档')
  const image = menu.append(new El('div', { role: 'option' }))
  image.addText('上传文件或图片')
  assert.strictEqual(Page.uploadImageItem(doc), image)
}

{
  const { doc, box } = surface()
  const comp = Page.composer(doc)
  assert.strictEqual(Page.setComposer(comp, '水彩风格'), true)
  assert.strictEqual(box.value, '水彩风格')
  const send = box.parentElement.append(new El('button', { 'aria-label': '发送' }))
  assert.strictEqual(Page.sendButton(doc, comp), send)
}

{
  const doc = makeDoc('www.doubao.com')
  const wrap = doc.body.append(new El('div', { class: 'guidance-input-surface' }))
  wrap.className = 'guidance-input-surface'
  const box = wrap.append(new El('textarea'))
  const preview = wrap.append(new El('img'))
  preview.complete = true
  preview.naturalWidth = 40
  preview.src = 'https://cdn.example/source.png'
  preview.currentSrc = preview.src
  message(doc, 'user', '旧问题')
  const oldAnswer = message(doc, 'assistant', '')
  const oldCard = oldAnswer.append(new El('div', { 'data-testid': 'mdbox_image' }))
  const oldImg = oldCard.append(new El('img'))
  oldImg.complete = true
  oldImg.naturalWidth = 32
  oldImg.src = 'https://cdn.example/old.png'
  oldImg.currentSrc = oldImg.src
  box.value = '水彩风格'
  const marked = Page.mark(doc, '水彩风格')
  assert.strictEqual(Page.sendAccepted(doc, marked), false)
  message(doc, 'user', '水彩风格')
  assert.strictEqual(Page.sendAccepted(doc, marked), true)
  const answer = message(doc, 'assistant', '正在生成')
  const userMedia = answer.append(new El('div', { 'data-testid': 'message_image_content' }))
  const userImg = userMedia.append(new El('img'))
  userImg.complete = true
  userImg.naturalWidth = 200
  userImg.src = 'https://cdn.example/user.png'
  userImg.currentSrc = userImg.src
  const loose = answer.append(new El('img'))
  loose.complete = true
  loose.naturalWidth = 400
  loose.src = 'https://cdn.example/loose.png'
  loose.currentSrc = loose.src
  const card = answer.append(new El('div', { 'data-testid': 'mdbox_image' }))
  const thumb = card.append(new El('img'))
  thumb.complete = true
  thumb.naturalWidth = 64
  thumb.src = 'https://cdn.example/thumb.png'
  thumb.currentSrc = thumb.src
  const full = card.append(new El('img'))
  full.complete = true
  full.naturalWidth = 1024
  full.src = 'https://cdn.example/full.png'
  full.currentSrc = full.src
  const echo = card.append(new El('img'))
  echo.complete = true
  echo.naturalWidth = 2000
  echo.src = preview.src
  echo.currentSrc = preview.src
  const outside = doc.body.append(new El('div', { 'data-testid': 'mdbox_image' }))
  const outsideImg = outside.append(new El('img'))
  outsideImg.complete = true
  outsideImg.naturalWidth = 900
  outsideImg.src = 'https://cdn.example/outside.png'
  outsideImg.currentSrc = outsideImg.src
  const found = Page.resultImages(doc, marked)
  assert.deepStrictEqual(found.map((item) => item.url), ['https://cdn.example/full.png'])
  assert.strictEqual(Page.resultsBusy(doc, marked), true)
}

{
  const doc = makeDoc('www.doubao.com')
  const promo = doc.body.append(new El('div'))
  const cta = promo.append(new El('button'))
  cta.addText('立即下载电脑版')
  const close = promo.append(new El('button', { 'aria-label': '关闭' }))
  assert.strictEqual(Page.dismissClientPromo(doc, null), close)
  const comp = doc.body.append(new El('textarea'))
  promo.append(comp)
  assert.strictEqual(Page.dismissClientPromo(doc, comp), null)
}

{
  const doc = makeDoc('www.doubao.com')
  const widget = doc.body.append(new El('div', { class: 'captcha-widget' }))
  widget.className = 'captcha-widget'
  widget.addText('请完成下方验证码')
  const canvas = widget.append(new El('canvas'))
  canvas.rect = { width: 120, height: 80, left: 0, top: 0 }
  canvas.toDataURL = () => 'data:image/png;base64,aGVsbG8='
  const found = Page.detectCaptcha(doc)
  assert.strictEqual(found.blocked, true)
  assert.strictEqual(Page.captchaCrop(widget), 'data:image/png;base64,aGVsbG8=')
}

{
  const doc = makeDoc('www.doubao.com')
  const login = doc.body.append(new El('button'))
  login.addText('登录')
  assert.strictEqual(Page.loginWall(doc), true)
  doc.body.append(new El('textarea'))
  assert.strictEqual(Page.loginWall(doc), false)
}

function rasterImg(parent, src, width) {
  const img = parent.append(new El('img'))
  img.complete = true
  img.naturalWidth = width
  img.src = src
  img.currentSrc = src
  return img
}

function generatedCard(prompt) {
  const doc = makeDoc('www.doubao.com')
  const wrap = doc.body.append(new El('div', { class: 'guidance-input-surface' }))
  wrap.className = 'guidance-input-surface'
  const box = wrap.append(new El('textarea'))
  box.value = prompt
  const marked = Page.mark(doc, prompt)
  message(doc, 'user', prompt)
  const answer = message(doc, 'assistant', '已转成水彩风格')
  const card = answer.append(new El('div', { 'data-testid': 'mdbox_image' }))
  return { doc, marked, card }
}

{
  const { doc, hidden } = surface()
  const named = hidden.parentElement.append(new El('input', { type: 'file', 'data-testid': 'upload-file-input', accept: 'image/*' }))
  named.rect = { width: 0, height: 0, left: 0, top: 0 }
  const extra = hidden.parentElement.append(new El('input', { type: 'file', accept: 'image/*' }))
  extra.rect = { width: 0, height: 0, left: 0, top: 0 }
  assert.strictEqual(Page.fileInput(doc, Page.composer(doc)), named)
}

{
  const doc = makeDoc('www.doubao.com')
  const decoy = doc.body.append(new El('textarea'))
  decoy.value = '别写这里'
  const slot = doc.body.append(new El('div', { 'data-testid': 'chat_input_input' }))
  const box = slot.append(new El('div', { contenteditable: 'true' }))
  assert.strictEqual(Page.composer(doc), box)
  assert.notStrictEqual(Page.composer(doc), decoy)
}

{
  assert.strictEqual(EMPTY_SVG.length, 82)
  assert.strictEqual(Page.isPlaceholderURL(EMPTY_SVG_URL), true)
  assert.strictEqual(Page.isPlaceholderURL('data:image/svg+xml;charset=utf-8,' + encodeURIComponent(EMPTY_SVG)), true)
  assert.strictEqual(Page.isPlaceholderURL('blob:https://www.doubao.com/11111111-2222-3333-4444-555555555555'), true)
  assert.strictEqual(Page.isPlaceholderURL('https://cdn.example/shell.svg'), true)
  assert.strictEqual(Page.isPlaceholderURL('https://p3-flow-imagex-sign.byteimg.com/ocean-cloud-tos/a.jpeg'), false)
  const shellBytes = Uint8Array.from(EMPTY_SVG, (ch) => ch.charCodeAt(0))
  assert.strictEqual(globalThis.XBS.looksLikeRaster(shellBytes), false)
  assert.strictEqual(globalThis.XBS.looksLikeRaster(Uint8Array.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])), true)
  assert.strictEqual(globalThis.XBS.looksLikeRaster(Uint8Array.from([0xff, 0xd8, 0xff, 0x00])), true)
  assert.strictEqual(globalThis.XBS.looksLikeRaster(Uint8Array.from([0x47, 0x49, 0x46, 0x38, 0x39, 0x61])), true)
  const webp = new Uint8Array(12)
  webp.set(Uint8Array.from('RIFF', (ch) => ch.charCodeAt(0)), 0)
  webp.set(Uint8Array.from('WEBP', (ch) => ch.charCodeAt(0)), 8)
  assert.strictEqual(globalThis.XBS.looksLikeRaster(webp), true)
}

{
  const { doc, marked, card } = generatedCard('水彩风格')
  const shell = rasterImg(card, EMPTY_SVG_URL, 1536)
  assert.deepStrictEqual(Page.resultImages(doc, marked).map((item) => item.url), [])
  shell.src = 'https://cdn.example/watercolor.png'
  shell.currentSrc = shell.src
  assert.deepStrictEqual(Page.resultImages(doc, marked).map((item) => item.url), ['https://cdn.example/watercolor.png'])
}

{
  const { doc, marked, card } = generatedCard('水彩风格')
  rasterImg(card, EMPTY_SVG_URL, 1536)
  rasterImg(card, 'https://cdn.example/thumb.png', 64)
  rasterImg(card, 'https://cdn.example/full.png', 1024)
  assert.deepStrictEqual(Page.resultImages(doc, marked).map((item) => item.url), ['https://cdn.example/full.png'])
}

{
  const { doc, marked, card } = generatedCard('水彩风格')
  const shell = rasterImg(card, EMPTY_SVG_URL, 1536)
  shell.attrs['data-src'] = 'https://p3-flow-imagex-sign.byteimg.com/ocean-cloud-tos/watercolor.jpeg'
  assert.strictEqual(Page.imageURL(shell), shell.attrs['data-src'])
  assert.deepStrictEqual(Page.resultImages(doc, marked).map((item) => item.url), [shell.attrs['data-src']])
}

{
  const { doc, marked, card } = generatedCard('水彩风格')
  const shell = rasterImg(card, 'blob:https://www.doubao.com/placeholder', 1536)
  shell.attrs.srcset = 'https://cdn.example/small.png 480w, https://cdn.example/full.png 1536w'
  assert.deepStrictEqual(Page.resultImages(doc, marked).map((item) => item.url), ['https://cdn.example/full.png'])
}

{
  const { doc, marked, card } = generatedCard('水彩风格')
  const picture = card.append(new El('picture'))
  picture.append(new El('source', { srcset: 'https://cdn.example/full.webp 1536w, https://cdn.example/small.webp 480w' }))
  rasterImg(picture, EMPTY_SVG_URL, 1536)
  assert.deepStrictEqual(Page.resultImages(doc, marked).map((item) => item.url), ['https://cdn.example/full.webp'])
}

{
  const { doc, marked, card } = generatedCard('水彩风格')
  const frame = card.append(new El('div'))
  frame.style = { backgroundImage: 'url("https://cdn.example/bg.png")' }
  rasterImg(frame, EMPTY_SVG_URL, 1536)
  assert.deepStrictEqual(Page.resultImages(doc, marked).map((item) => item.url), ['https://cdn.example/bg.png'])
}

{
  const { doc, marked, card } = generatedCard('水彩风格')
  const link = card.append(new El('a', { href: 'https://p9-flow-imagex-sign.byteimg.com/ocean-cloud-tos/full.image', download: '' }))
  link.addText('下载')
  rasterImg(link, EMPTY_SVG_URL, 1536)
  assert.deepStrictEqual(Page.resultImages(doc, marked).map((item) => item.url), ['https://p9-flow-imagex-sign.byteimg.com/ocean-cloud-tos/full.image'])
}

assert.strictEqual(Page.attachLocalImage, undefined)
assert.strictEqual(Page.createChooserBridge, undefined)

console.log('doubao page.js anchors ok')

{
  const doc = makeDoc('www.doubao.com')
  const button = doc.body.append(new El('button', { 'aria-label': '发送' }))
  const order = []
  button.dispatchEvent = function (ev) {
    order.push(ev.type)
    if (ev.type === 'pointerdown' && ev.preventDefault) ev.preventDefault()
  }
  button.click = function () {
    order.push('click')
    this.clicked = (this.clicked || 0) + 1
  }
  assert.strictEqual(Page.activate(button), true)
  assert.deepStrictEqual(order, ['pointerdown', 'mousedown', 'pointerup', 'mouseup', 'click'])
  assert.strictEqual(button.clicked, 1)
}

{
  const { doc, box } = surface()
  const wrap = box.parentElement
  const entry = wrap.append(new El('button', { 'data-testid': 'upload_file_button' }))
  const baseline = new Set()
  assert.strictEqual(Page.freshAttachment(doc, baseline), false)
  const card = wrap.append(new El('div', { 'data-testid': 'attachment-image-card' }))
  const pending = card.append(new El('img'))
  pending.complete = false
  pending.naturalWidth = 0
  pending.src = 'blob:https://www.doubao.com/pending'
  pending.currentSrc = pending.src
  assert.strictEqual(Page.freshAttachment(doc, baseline), true)
  void entry
}

{
  const doc = makeDoc('www.doubao.com')
  const marked = Page.mark(doc, '水彩风格')
  message(doc, 'user', '水彩风格')
  const answer = message(doc, 'assistant', '')
  const card = answer.append(new El('div', { 'data-testid': 'mdbox_image' }))
  const shell = card.append(new El('img'))
  shell.complete = true
  shell.naturalWidth = 1536
  shell.src = 'data:image/svg+xml,%3csvg xmlns=%27http://www.w3.org/2000/svg%27/%3e'
  shell.currentSrc = shell.src
  let scrolled = 0
  card.scrollIntoView = function () { scrolled += 1 }
  Page.revealResults(doc, marked)
  assert.ok(scrolled >= 1)
  const real = card.append(new El('img'))
  real.complete = true
  real.naturalWidth = 384
  real.loading = 'lazy'
  real.src = 'https://p6-flow-imagex-sign.byteimg.com/tos-cn-i-a9rns2rl98/rc_gen_image/x.jpeg~tplv-a9rns2rl98-cthumb_lwm3.png'
  real.currentSrc = real.src
  const chosen = Page.resultImages(doc, marked)
  assert.strictEqual(chosen.length, 1)
  assert.ok(/byteimg/.test(chosen[0].url))
  const full = card.append(new El('img'))
  full.complete = true
  full.naturalWidth = 1024
  full.src = 'https://p26-flow-imagex-sign.byteimg.com/tos-cn-i-a9rns2rl98/rc_gen_image/x.jpeg~tplv-a9rns2rl98-cgen_lwm3.png'
  full.currentSrc = full.src
  const prefer = Page.resultImages(doc, marked)
  assert.strictEqual(prefer.length, 1)
  assert.ok(/cgen_lwm/.test(prefer[0].url))
}
