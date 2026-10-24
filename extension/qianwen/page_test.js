// 用最小 DOM 夹具核对 page.js 的锚点。这些锚点来自 internal/strategy/js/ego.js，不是新发明的 class。
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

function surfaceWithInputs() {
  const doc = makeDoc('www.tongyi.com')
  const old = doc.body.append(new El('div'))
  const oldInput = old.append(new El('input', { type: 'file', id: 'old' }))
  oldInput.id = 'old'
  const surface = doc.body.append(new El('div', { class: 'guidance-input-surface' }))
  surface.className = 'guidance-input-surface'
  const box = surface.append(new El('textarea'))
  const entry = surface.append(new El('button', { 'aria-label': '添加附件' }))
  const hidden = surface.append(new El('input', { type: 'file', id: 'new', accept: 'image/*' }))
  hidden.id = 'new'
  hidden.rect = { width: 0, height: 0, left: 0, top: 0 }
  return { doc, box, entry, hidden }
}

{
  const { doc, hidden } = surfaceWithInputs()
  const comp = Page.composer(doc)
  assert.strictEqual(comp.tagName, 'TEXTAREA')
  const input = Page.fileInput(doc, comp)
  assert.strictEqual(input, hidden, '应使用输入区旁的 file input，而不是历史消息里的那个')
  assert.strictEqual(Page.unsentBlocksUpload(doc, comp), '')
}

{
  const doc = makeDoc('www.qianwen.com')
  const surface = doc.body.append(new El('div', { class: 'guidance-input-surface' }))
  surface.className = 'guidance-input-surface'
  const box = surface.append(new El('textarea'))
  box.value = '还没发送'
  surface.append(new El('button', { 'aria-label': '添加附件' }))
  assert.strictEqual(Page.unsentBlocksUpload(doc, Page.composer(doc)), '当前输入区有未发送内容，未操作上传菜单')
}

{
  const doc = makeDoc('www.tongyi.com')
  const menu = doc.body.append(new El('div'))
  const image = menu.append(new El('div', { role: 'menuitem' }))
  image.addText('上传图片')
  const docItem = menu.append(new El('div', { role: 'menuitem' }))
  docItem.addText('上传文档')
  assert.strictEqual(Page.uploadImageItem(doc), image)
}

{
  const { doc, box } = surfaceWithInputs()
  const comp = Page.composer(doc)
  assert.strictEqual(Page.setComposer(comp, '水彩风格'), true)
  assert.strictEqual(box.value, '水彩风格')
  const send = box.parentElement.append(new El('button', { 'aria-label': '发送' }))
  assert.strictEqual(Page.sendButton(doc, comp), send)
}

{
  const doc = makeDoc('www.tongyi.com')
  const surface = doc.body.append(new El('div', { class: 'guidance-input-surface' }))
  surface.className = 'guidance-input-surface'
  const box = surface.append(new El('textarea'))
  const preview = surface.append(new El('img'))
  preview.complete = true
  preview.naturalWidth = 40
  preview.src = 'https://cdn.example/source.png'
  preview.currentSrc = preview.src
  const user = doc.body.append(new El('div', { class: 'chat-question-card-wrap' }))
  user.className = 'chat-question-card-wrap'
  user.addText('水彩风格')
  const marked = Page.mark(doc, '水彩风格')
  box.value = ''
  assert.strictEqual(Page.sendAccepted(doc, marked), true)

  box.value = '水彩风格'
  const markedAgain = Page.mark(doc, '水彩风格')
  assert.strictEqual(Page.sendAccepted(doc, markedAgain), false)
  const user2 = doc.body.append(new El('div', { class: 'chat-question-card-wrap' }))
  user2.className = 'chat-question-card-wrap'
  user2.addText('水彩风格')
  assert.strictEqual(Page.sendAccepted(doc, markedAgain), true)

  const answer = doc.body.append(new El('div', { class: 'chat-answers-card-wrap' }))
  answer.className = 'chat-answers-card-wrap'
  const markdown = answer.append(new El('div', { id: 'qk-markdown-react' }))
  markdown.id = 'qk-markdown-react'
  const good = markdown.append(new El('img'))
  good.complete = true
  good.naturalWidth = 512
  good.src = 'https://cdn.example/out.png'
  good.currentSrc = good.src
  const card = markdown.append(new El('div', { id: 'card_card_ai_generate_image_1' }))
  card.id = 'card_card_ai_generate_image_1'
  const loading = card.append(new El('img', { alt: 'loading' }))
  loading.complete = true
  loading.naturalWidth = 32
  loading.src = 'https://cdn.example/loading.png'
  const echo = markdown.append(new El('img'))
  echo.complete = true
  echo.naturalWidth = 40
  echo.src = preview.src
  echo.currentSrc = preview.src
  const found = Page.resultImages(doc, markedAgain)
  assert.deepStrictEqual(found.map((item) => item.url), ['https://cdn.example/out.png'])
  assert.strictEqual(Page.resultsBusy(doc, markedAgain), true)
}

{
  const doc = makeDoc('chat.qwen.ai')
  const widget = doc.body.append(new El('div', { class: 'captcha-widget' }))
  widget.className = 'captcha-widget'
  widget.addText('请完成下方验证码')
  const canvas = widget.append(new El('canvas'))
  canvas.rect = { width: 120, height: 80, left: 0, top: 0 }
  canvas.toDataURL = () => 'data:image/png;base64,aGVsbG8='
  const found = Page.detectCaptcha(doc)
  assert.strictEqual(found.blocked, true)
  assert.strictEqual(found.root, widget)
  assert.strictEqual(Page.captchaCrop(widget), 'data:image/png;base64,aGVsbG8=')
  const plain = makeDoc('www.tongyi.com')
  plain.body.append(new El('textarea'))
  assert.strictEqual(Page.detectCaptcha(plain).blocked, false)
}

{
  const doc = makeDoc('chat.qwen.ai')
  const login = doc.body.append(new El('button'))
  login.addText('登录')
  assert.strictEqual(Page.loginWall(doc), true)
  doc.body.append(new El('textarea'))
  assert.strictEqual(Page.loginWall(doc), false)
}

console.log('page.js anchors ok')
