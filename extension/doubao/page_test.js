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


vm.runInThisContext(fs.readFileSync(path.join(__dirname, 'page.js'), 'utf8'))
const Page = globalThis.XBSPage

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

console.log('doubao page.js anchors ok')
