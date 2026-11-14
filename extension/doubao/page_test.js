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

  closest(sel) {
    for (let node = this; node; node = node.parentElement) {
      if (node.matches && node.matches(sel)) return node
    }
    return null
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


let rawMessage
globalThis.addEventListener = (type, fn) => { if (type === 'message') rawMessage = fn }
vm.runInThisContext(fs.readFileSync(path.join(__dirname, '..', 'protocol.js'), 'utf8'))
vm.runInThisContext(fs.readFileSync(path.join(__dirname, 'page.js'), 'utf8'))
const Page = globalThis.TULAIPage
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
  const { doc } = surface()
  const login = doc.body.append(new El('button', { 'data-testid': 'to_login_button' }))
  login.addText('登录')
  assert.strictEqual(Page.loginWall(doc), true, '未登录时即使有输入框，也不能当作已登录')
  login.display = 'none'
  assert.strictEqual(Page.loginWall(doc), false, '隐藏的登录按钮不应影响已登录页面')
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

for (const attrs of [{ class: 'guidance-input-surface' }, { 'data-chat-input-body': '1' }]) {
  const doc = makeDoc('www.doubao.com')
  doc.body.append(new El('div', { 'data-testid': 'attachment-image-card', 'aria-label': 'old.png' }))
  const wrap = doc.body.append(new El('div', attrs))
  const input = wrap.append(new El('div'))
  input.append(new El('textarea'))
  input.append(new El('button'))
  const baseline = new Set(descendants(doc))
  wrap.append(new El('div', { 'data-testid': 'attachment-image-card', 'aria-label': 'source.png' }))
  assert.strictEqual(Page.uploadEntry(doc, Page.composer(doc)), null)
  assert.deepStrictEqual(Page.attachmentNames(doc), ['source.png'], '图像模式没有旧上传按钮标识时，仍须读取同一输入壳内的附件')
  assert.strictEqual(Page.freshAttachment(doc, baseline), true)
}

for (const prefix of ['生成图片：', '生成图片: ']) {
  const { doc, box } = surface()
  const prompt = '橙色陶瓷杯静物图片'
  box.value = prompt
  message(doc, 'user', prefix + prompt)
  const old = message(doc, 'assistant', '')
  rasterImg(old.append(new El('div', { 'data-testid': 'mdbox_image' })), 'https://cdn.example/old-mode.png', 1024)
  const marked = Page.mark(doc, prompt)
  assert.strictEqual(Page.sendAccepted(doc, marked), false, '旧的带前缀消息不能算新发送')
  assert.strictEqual(Page.resultImages(doc, marked).length, 0)
  const user = message(doc, 'user', '')
  user.append(new El('p')).addText(prefix + prompt)
  user.append(new El('span')).addText('今天 19:32')
  const answer = message(doc, 'assistant', '')
  rasterImg(answer.append(new El('div', { 'data-testid': 'mdbox_image' })), 'https://cdn.example/new-mode.png', 1024)
  assert.strictEqual(Page.sendAccepted(doc, marked), true)
  assert.deepStrictEqual(Page.newAssistants(doc, marked), [answer])
  assert.deepStrictEqual(Page.resultImages(doc, marked).map(image => image.url), ['https://cdn.example/new-mode.png'])
}

async function testImageMode() {
  const { doc, box } = surface()
  const outside = doc.body.append(new El('button', { 'data-testid': 'skill_bar_button_3' })).addText('图像生成')
  const bar = box.parentElement.append(new El('div', { 'data-testid': 'guidance-skill-bar' }))
  const button = bar.append(new El('button', { 'data-testid': 'skill_bar_button_3' })).addText('图像生成')
  let clicks = 0
  let replacement
  button.click = function () {
    clicks++
    setTimeout(function () {
      box.isConnected = false
      replacement = box.parentElement.append(new El('textarea'))
      box.parentElement.append(new El('span')).addText('图像生成')
    }, 10)
  }
  assert.strictEqual(Page.imageModeActive(doc), false, '工具栏的同名入口不代表模式已选中')
  assert.strictEqual((await Page.ensureImageMode(doc)).ok, true)
  assert.strictEqual(Page.composer(doc), replacement, '切换模式后须使用重绘的输入框')
  assert.strictEqual(Page.imageModeActive(doc), true)
  assert.strictEqual((await Page.ensureImageMode(doc)).ok, true)
  assert.strictEqual(clicks, 1, '模式已经选中时不能再次点击')
  assert.strictEqual(outside.clicked, undefined, '不能点击输入区外的同名按钮')

  const selected = surface()
  const pressed = selected.box.parentElement.append(new El('button', { 'data-testid': 'skill_bar_button_3', 'aria-pressed': 'true' })).addText('图像生成')
  assert.strictEqual((await Page.ensureImageMode(selected.doc)).ok, true)
  assert.strictEqual(pressed.clicked, undefined)

  const unchanged = surface()
  const entry = unchanged.box.parentElement.append(new El('button', { 'data-testid': 'skill_bar_button_3' })).addText('图像生成')
  assert.strictEqual((await Page.ensureImageMode(unchanged.doc, null, 250)).code, 'E_SELECTOR_MISS', '只有点击、没有选中证据时必须停止')
  assert.strictEqual(entry.clicked, 1, '等待模式生效时不能反复点击')

  // 点击后 2 秒多才出现已选标签（2026-11 冷加载实测）：默认窗口要等到它。
  const slow = surface()
  const slowEntry = slow.box.parentElement.append(new El('button', { 'data-testid': 'skill_bar_button_3' })).addText('图像生成')
  slowEntry.click = function () {
    slowEntry.clicked = (slowEntry.clicked || 0) + 1
    setTimeout(function () {
      slow.box.parentElement.append(new El('span')).addText('图像生成')
    }, 2200)
  }
  assert.strictEqual((await Page.ensureImageMode(slow.doc)).ok, true, '慢页面在默认窗口内生效')
  assert.strictEqual(slowEntry.clicked, 1)

  // 页面没挂载完时第一次点击会被吞掉（2026-11 冷加载实测）：间隔 3.5 秒补点。
  const swallowed = surface()
  const swallowEntry = swallowed.box.parentElement.append(new El('button', { 'data-testid': 'skill_bar_button_3' })).addText('图像生成')
  swallowEntry.click = function () {
    swallowEntry.clicked = (swallowEntry.clicked || 0) + 1
    if (swallowEntry.clicked < 2) return
    setTimeout(function () {
      swallowed.box.parentElement.append(new El('span')).addText('图像生成')
    }, 10)
  }
  assert.strictEqual((await Page.ensureImageMode(swallowed.doc)).ok, true, '被吞掉的点击要在补点后生效')
  assert.strictEqual(swallowEntry.clicked, 2, '只允许一次补点')

  const missing = surface()
  missing.doc.body.append(new El('span')).addText('图像生成')
  assert.strictEqual((await Page.ensureImageMode(missing.doc, null, 250)).code, 'E_SELECTOR_MISS', '输入区外的文字不能代表模式已选中')
  assert.strictEqual((await Page.ensureImageMode(missing.doc, () => true)).code, 'E_CANCELLED')
  missing.doc.body.append(new El('button', { 'data-testid': 'to_login_button' }))
  assert.strictEqual((await Page.ensureImageMode(missing.doc)).code, 'E_NOT_LOGGED_IN')

  // 2026-11 起已选模式挂固定 testid 的退出按钮，是最直接的「已进入图像模式」证据。
  const exitChip = surface()
  exitChip.box.parentElement.append(new El('div', { 'data-testid': 'skill_input_exit_button', 'data-value': '3' })).addText('图像生成')
  assert.strictEqual(Page.imageModeActive(exitChip.doc), true, '退出按钮代表图像模式已选中')
  assert.strictEqual((await Page.ensureImageMode(exitChip.doc, null, 250)).ok, true, '已有退出按钮时不再点击入口')

  // 「更多」折叠菜单与技能栏文本按钮，以及游客态无入口的未登录归类。
  const overflow = surface()
  const moreBtn = overflow.box.parentElement.append(new El('button', { 'data-testid': 'skill_bar_button_more' })).addText('更多')
  moreBtn.click = function () {
    moreBtn.clicked = (moreBtn.clicked || 0) + 1
    const menu = overflow.doc.body.append(new El('div', { role: 'menu', 'data-state': 'open' }))
    const entry = menu.append(new El('div', { role: 'menuitem' })).addText('图像生成')
    entry.click = function () {
      entry.clicked = (entry.clicked || 0) + 1
      overflow.box.parentElement.append(new El('div', { 'data-testid': 'skill_input_exit_button' })).addText('图像生成')
    }
  }
  assert.strictEqual((await Page.ensureImageMode(overflow.doc, null, 800)).ok, true, '更多菜单里的图像生成也要能点开')
  assert.strictEqual(moreBtn.clicked, 1)

  const skillBar = surface()
  const guidanceBar = skillBar.box.parentElement.append(new El('div', { 'data-testid': 'guidance-skill-bar' }))
  const guidanceBtn = guidanceBar.append(new El('button')).addText('图像生成')
  guidanceBtn.click = function () {
    guidanceBtn.clicked = (guidanceBtn.clicked || 0) + 1
    skillBar.box.parentElement.append(new El('div', { 'data-testid': 'skill_input_exit_button' })).addText('图像生成')
  }
  assert.strictEqual((await Page.ensureImageMode(skillBar.doc, null, 800)).ok, true, '技能栏文本按钮也要能点击')
  assert.strictEqual(guidanceBtn.clicked, 1)

  const guest = surface()
  guest.doc.body.append(new El('button')).addText('登录')
  assert.strictEqual(Page.loginWall(guest.doc), false, '仅有底部输入框和登录按钮时不直接破坏原有判定')
  assert.strictEqual((await Page.ensureImageMode(guest.doc, null, 250)).code, 'E_NOT_LOGGED_IN', '游客态无图像入口应归类为未登录')

  const modalGuest = surface()
  modalGuest.doc.body.append(new El('div', { role: 'dialog' })).addText('扫码登录')
  assert.strictEqual(Page.loginWall(modalGuest.doc), true, '出现登录弹窗时即使有输入框也要判为未登录')
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
  const fixture = fs.readFileSync(path.join(__dirname, '..', '..', 'internal', 'strategy', 'testdata', 'doubao', '9336-1', 'page.html'), 'utf8')
  const rawMatch = fixture.match(/"image_ori_raw"\s*:\s*\{\s*"url"\s*:\s*("(?:\\.|[^"\\])*")/)
  assert.ok(rawMatch, '豆包页面夹具必须含无水印原图')
  const raw = JSON.parse(rawMatch[1])
  const imagePath = new URL(raw).pathname.split('~tplv-')[0]
  const tag = Array.from(fixture.matchAll(/<img\b[^>]*>/g), (match) => match[0]).find((value) => value.includes(imagePath))
  assert.ok(tag, '豆包页面夹具必须含对应的展示图')
  const display = tag.match(/\bsrc="([^"]+)"/)[1].replace(/&amp;/g, '&')
  const { doc, marked, card } = generatedCard('原图')
  rasterImg(card, display, 1024)
  const main = vm.createContext({ URL, location: { href: 'https://www.doubao.com/chat/', hostname: 'www.doubao.com', origin: 'https://www.doubao.com' } })
  main.window = main
  main.postMessage = (data) => rawMessage({ source: globalThis, data })
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'raw.js'), 'utf8'), main)
  main.payload = JSON.stringify({ creations: [{ image: { image_preview: { url: display }, image_ori_raw: { url: raw } } }] })
  vm.runInContext('JSON.parse(payload)', main)
  assert.strictEqual(Page.resultImages(doc, marked)[0].rawUrl, raw, '扩展应按真实页面图片路径匹配响应原图')
  rawMessage({ source: {}, data: { type: 'TULAI_DOUBAO_RAW_IMAGES', pairs: [{ source: display, raw: 'https://evil.example/wrong.png' }] } })
  assert.strictEqual(Page.resultImages(doc, marked)[0].rawUrl, raw, '不得接受其他窗口消息')
  Page.mark(doc, '下一轮')
  assert.strictEqual(Page.resultImages(doc, marked)[0].rawUrl, '', '新任务应清除旧原图')
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
  assert.strictEqual(globalThis.TULAI.looksLikeRaster(shellBytes), false)
  assert.strictEqual(globalThis.TULAI.looksLikeRaster(Uint8Array.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])), true)
  assert.strictEqual(globalThis.TULAI.looksLikeRaster(Uint8Array.from([0xff, 0xd8, 0xff, 0x00])), true)
  assert.strictEqual(globalThis.TULAI.looksLikeRaster(Uint8Array.from([0x47, 0x49, 0x46, 0x38, 0x39, 0x61])), true)
  const webp = new Uint8Array(12)
  webp.set(Uint8Array.from('RIFF', (ch) => ch.charCodeAt(0)), 0)
  webp.set(Uint8Array.from('WEBP', (ch) => ch.charCodeAt(0)), 8)
  assert.strictEqual(globalThis.TULAI.looksLikeRaster(webp), true)
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
  const card = wrap.append(new El('div', { 'data-testid': 'attachment-image-card', 'aria-label': '01-first.png' }))
  const pending = card.append(new El('img'))
  pending.complete = false
  pending.naturalWidth = 0
  pending.src = 'blob:https://www.doubao.com/pending'
  pending.currentSrc = pending.src
  assert.strictEqual(Page.freshAttachment(doc, baseline), true)
  baseline.add(card)
  baseline.add(pending)
  assert.strictEqual(Page.freshAttachment(doc, baseline), false, '已有附件不能当作新上传')
  const second = wrap.append(new El('div', { 'data-testid': 'attachment-image-card', 'aria-label': '02-second.png' }))
  assert.strictEqual(Page.freshAttachment(doc, baseline), true)
  assert.deepStrictEqual(Array.from(Page.attachmentNames(doc)), ['01-first.png', '02-second.png'])
  void second
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

{
  const doc = makeDoc('www.doubao.com')
  const marked = Page.mark(doc, '水彩风格')
  message(doc, 'user', '旧问题')
  const old = message(doc, 'assistant', '')
  const oldCard = old.append(new El('div', { 'data-testid': 'mdbox_image' }))
  const oldImg = oldCard.append(new El('img'))
  oldImg.complete = true
  oldImg.naturalWidth = 800
  oldImg.src = 'https://cdn.example/old.png'
  oldImg.currentSrc = oldImg.src
  old.append(new El('button')).addText('重新生成')
  message(doc, 'user', '水彩风格')
  const answer = message(doc, 'assistant', '')
  function card(url) {
    const box = answer.append(new El('div', { 'data-testid': 'mdbox_image' }))
    const img = box.append(new El('img'))
    img.complete = true
    img.naturalWidth = 1024
    img.src = url
    img.currentSrc = url
  }
  card('https://cdn.example/n1.png')
  assert.strictEqual(Page.replyDone(doc, marked).done, true, '实图在、没有生成指示，就可以交给 3 秒稳定判断')
  card('https://cdn.example/n2.png')
  card('https://cdn.example/n3.png')
  card('https://cdn.example/n4.png')
  assert.strictEqual(Page.replyDone(doc, marked).done, true, '没有重新生成按钮也要结束')
  assert.strictEqual(Page.resultImages(doc, marked).length, 4)
  answer.innerText = '生成中'
  assert.strictEqual(Page.replyDone(doc, marked).done, false, '还在生成时不能结束')
  answer.innerText = ''
  const counted = makeDoc('www.doubao.com')
  const mark2 = Page.mark(counted, '格子')
  message(counted, 'user', '格子')
  const grid = message(counted, 'assistant', '')
  const box = grid.append(new El('div', { 'data-testid': 'mdbox_image' }))
  const img = box.append(new El('img'))
  img.complete = true
  img.naturalWidth = 1024
  img.src = 'https://cdn.example/g0.png'
  img.currentSrc = img.src
  assert.strictEqual(Page.replyDone(counted, mark2).done, true, '没有声明张数时，一张实图也算候选')

  answer.attrs['data-streaming'] = 'true'
  assert.strictEqual(Page.replyDone(doc, marked).done, false, '流式属性为 true 时不能结束')
  answer.attrs['data-streaming'] = 'false'
  assert.strictEqual(Page.replyDone(doc, marked).done, true)

  marked.anchorConfirmed = true
  const brk = doc.body.append(new El('button', { 'data-testid': 'chat_input_local_break_button' }))
  assert.strictEqual(Page.resultsBusy(doc, marked), true, '停止生成按钮可见时代表仍在出图')
  assert.strictEqual(Page.replyDone(doc, marked).done, false)
  brk.className = '!hidden'
  assert.strictEqual(Page.resultsBusy(doc, marked), false)
  assert.strictEqual(Page.replyDone(doc, marked).done, true)

  const lazyBox = answer.append(new El('div', { 'data-testid': 'mdbox_image' }))
  const lazyImg = lazyBox.append(new El('img'))
  lazyImg.src = 'data:image/svg+xml;base64,PHN2Zz48L3N2Zz4='
  lazyImg.currentSrc = lazyImg.src
  const pendingSignal = Page.replyDone(doc, marked)
  assert.strictEqual(pendingSignal.done, false, '仍有 SVG 占位卡片待揭开时应等待')
  assert.strictEqual(pendingSignal.pendingCards, 1)
  marked.pendingSince = Date.now() - 16000
  assert.strictEqual(Page.replyDone(doc, marked).done, true, '占位卡片宽限到期后放行，避免永久卡死')
}

// 同一对话里重复洗图指令，旧消息重绘后也不能归到下一张源图。
{
  const doc = makeDoc('www.doubao.com')
  doc.readyState = 'complete'
  doc.addEventListener = () => {}
  const oldUser = message(doc, 'user', '洗图指令')
  const oldReply = message(doc, 'assistant', '')
  oldReply.attrs['data-finished'] = 'true'
  const oldCard = oldReply.append(new El('div', { 'data-testid': 'mdbox_image' }))
  rasterImg(oldCard, 'https://cdn.example/old.png', 1024).naturalHeight = 1024
  const context = vm.createContext({
    document: doc,
    location: { hostname: 'www.doubao.com', href: 'https://www.doubao.com/chat/test' },
    URL,
    XMLHttpRequest: class { open() {} send() {} },
    MutationObserver: class { observe() {} },
  })
  context.window = context
  context.__tulaiWashPage = Page
  vm.runInContext(fs.readFileSync(path.join(__dirname, '..', '..', 'internal', 'strategy', 'js', 'hook.js'), 'utf8'), context)
  context.__tulaiMarkTask()
  assert.strictEqual(context.__tulaiMarkDoubaoTurn('洗图指令'), true)
  assert.strictEqual(context.__tulaiLiveImages().images.length, 0)

  oldUser.isConnected = false
  oldReply.isConnected = false
  doc.body.children = []
  doc.body.childNodes = []
  message(doc, 'user', '洗图指令')
  const redrawn = message(doc, 'assistant', '')
  redrawn.attrs['data-finished'] = 'true'
  const redrawnCard = redrawn.append(new El('div', { 'data-testid': 'mdbox_image' }))
  rasterImg(redrawnCard, 'https://cdn.example/old.png', 1024).naturalHeight = 1024
  assert.strictEqual(context.__tulaiLiveImages().images.length, 0, '旧消息重绘不能算作本次结果')

  message(doc, 'user', '洗图指令')
  const answer = message(doc, 'assistant', '')
  answer.attrs['data-finished'] = 'true'
  const card = answer.append(new El('div', { 'data-testid': 'mdbox_image' }))
  rasterImg(card, 'https://cdn.example/new.png', 1024).naturalHeight = 1024
  const live = context.__tulaiLiveImages()
  assert.strictEqual(live.done, true)
  assert.deepStrictEqual(Array.from(live.images, (image) => image.url), ['https://cdn.example/new.png'])
}

async function testSetOutputRatio() {
  // 图像模式激活后的「比例」触发按钮与下拉菜单（2026-11 页面结构）。
  const { doc, box } = surface()
  box.parentElement.append(new El('div', { 'data-testid': 'skill_input_exit_button', 'data-value': '3' })).addText('图像生成')
  const trigger = box.parentElement.append(new El('button', { 'aria-haspopup': 'menu' }))
  trigger.addText('比例 自动')
  let open = false
  const menu = doc.body.append(new El('div', { role: 'menu', 'data-state': 'closed' }))
  const setOpen = function (value) {
    open = value
    menu.attrs['data-state'] = open ? 'open' : 'closed'
  }
  trigger.click = function () {
    trigger.clicked = (trigger.clicked || 0) + 1
    setOpen(!open)
  }
  const items = ['自动', '9:16', '2:3', '3:4', '1:1', '4:3', '3:2', '16:9'].map(function (text) {
    const item = menu.append(new El('button'))
    item.addText(text)
    item.rect = { width: 64, height: 56, left: 0, top: 0 }
    item.click = function () {
      item.clicked = (item.clicked || 0) + 1
      setOpen(false)
      trigger.innerText = '比例 ' + text
    }
    return item
  })
  menu.rect = { width: 368, height: 240, left: 0, top: 0 }

  assert.strictEqual(Page.ratioControl(doc), trigger, '比例入口按文本与 aria-haspopup 识别')
  assert.strictEqual(Page.ratioValue(trigger), '', '「自动」不返回具体比例')
  let out = await Page.setOutputRatio(doc, '4:5', null, 3000)
  assert.strictEqual(out.error, '「比例」菜单没有 4:5', '菜单没有的比例要明确报错')
  assert.strictEqual(menu.attrs['data-state'], 'closed', '报错后菜单必须关上')

  out = await Page.setOutputRatio(doc, '4:3')
  assert.strictEqual(out.ok, true, '选中 4:3 后以触发按钮标签确认')
  assert.strictEqual(Page.ratioValue(Page.ratioControl(doc)), '4:3')

  out = await Page.setOutputRatio(doc, '4:3')
  assert.strictEqual(out.ok && out.already, true, '已是目标比例时不再点开菜单')

  const none = surface()
  assert.strictEqual((await Page.setOutputRatio(none.doc, '1:1', null, 250)).error, '未找到「比例」入口', '图像模式未激活时没有比例入口')

  const malformed = surface()
  assert.strictEqual((await Page.setOutputRatio(malformed.doc, '宽高比 4:3', null, 250)).error, '比例格式应为 4:3')
}

testImageMode().then(testSetOutputRatio).then(function () { console.log('doubao image mode ok') }).catch(function (err) { console.error(err); process.exitCode = 1 })
