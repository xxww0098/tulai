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

function installChooserMocks() {
  global.DataTransfer = class DataTransfer {
    constructor() {
      const files = []
      this.items = { add(file) { files.push(file) } }
      Object.defineProperty(this, 'files', {
        get() {
          return {
            length: files.length,
            item: (i) => files[i],
            [Symbol.iterator]: function* () { yield* files },
          }
        },
      })
    }
  }
  const elements = {}
  const listeners = {}
  const attrs = {}
  global.document = {
    documentElement: {
      setAttribute(name, value) { attrs[name] = String(value) },
      getAttribute(name) { return Object.prototype.hasOwnProperty.call(attrs, name) ? attrs[name] : null },
      appendChild(el) {
        if (el && el.id) elements[el.id] = el
        return el
      },
    },
    getElementById(id) { return elements[id] || null },
    createElement() {
      return {
        id: '',
        textContent: '',
        setAttribute() {},
        remove() {
          if (this.id && elements[this.id] === this) delete elements[this.id]
        },
      }
    },
    addEventListener(type, fn) {
      ;(listeners[type] || (listeners[type] = [])).push(fn)
    },
    dispatchEvent(ev) {
      ;(listeners[ev.type] || []).forEach((fn) => fn(ev))
      return true
    },
  }
  global.HTMLElement = class HTMLElement {}
  global.HTMLElement.nativeClicks = 0
  global.HTMLElement.prototype.click = function () { global.HTMLElement.nativeClicks++ }
  global.HTMLInputElement = class HTMLInputElement extends global.HTMLElement {
    constructor() {
      super()
      this.type = ''
      this.accept = ''
      this.files = null
      this.value = ''
      this._listeners = {}
    }
    getAttribute(name) {
      if (name === 'accept') return this.accept || null
      return null
    }
    addEventListener(type, fn) {
      ;(this._listeners[type] || (this._listeners[type] = [])).push(fn)
    }
    dispatchEvent(ev) {
      const list = this._listeners[ev.type] || []
      for (const fn of list) fn(ev)
      return true
    }
    remove() {}
  }
  global.HTMLInputElement.prototype.tagName = 'INPUT'
  global.HTMLInputElement.nativePickers = 0
  global.HTMLInputElement.prototype.showPicker = function () { global.HTMLInputElement.nativePickers++ }
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

async function chooserFlow() {
  installChooserMocks()
  vm.runInThisContext(fs.readFileSync(path.join(__dirname, 'chooser.js'), 'utf8'))
  assert.strictEqual(document.documentElement.getAttribute('data-xbs-chooser'), 'qianwen')

  document.documentElement.setAttribute('data-xbs-chooser', '')
  assert.strictEqual(Page.createChooserBridge(document), null)
  document.documentElement.setAttribute('data-xbs-chooser', 'qianwen')

  const bytes = Uint8Array.from([137, 80, 78, 71, 0, 255])
  const file = new File([bytes], 'src.png', { type: 'image/png' })

  {
    const { doc, entry, hidden } = surfaceWithInputs()
    let armed = false
    const located = await Page.attachLocalImage(doc, file, {
      bridge: {
        arm() { armed = true; return true },
        result() { return '' },
        disarm() {},
      },
    })
    assert.strictEqual(armed, false, '已有 file input 时不应武装菜单桥')
    assert.strictEqual(entry.clicked || 0, 0)
    assert.strictEqual(located.input, hidden)
    assert.strictEqual(located.assigned, true)
    assert.strictEqual(hidden.files.length, 1)
  }

  {
    const doc = makeDoc('www.qianwen.com')
    const surface = doc.body.append(new El('div', { class: 'guidance-input-surface' }))
    surface.className = 'guidance-input-surface'
    surface.append(new El('textarea'))
    const entry = surface.append(new El('button', { 'aria-label': '添加附件' }))
    let item = null
    entry.click = function () {
      this.clicked = (this.clicked || 0) + 1
      item = doc.body.append(new El('div', { role: 'menuitem' }))
      item.addText('上传图片\n最多上传10张')
      item.click = function () {
        this.clicked = (this.clicked || 0) + 1
        const input = new HTMLInputElement()
        input.type = 'file'
        input.accept = 'image/jpeg,image/png,.jpg,.png,.webp,.bmp'
        input.addEventListener('change', () => {
          item.picked = Array.from(input.files || [])
          input.value = ''
          input.files = { length: 0 }
        })
        try { input.showPicker() } catch (e) { input.click() }
      }
    }
    const bridge = Page.createChooserBridge(document)
    const located = await Page.attachLocalImage(doc, file, { bridge, timeout: 500 })
    assert.strictEqual(located.assigned, true)
    assert.strictEqual(located.via, 'chooser')
    assert.strictEqual(entry.clicked, 1)
    assert.strictEqual(item.clicked, 1)
    assert.strictEqual(HTMLInputElement.nativePickers, 0, '应拦住 showPicker，不打开系统文件框')
    assert.strictEqual(item.picked.length, 1)
    assert.strictEqual(item.picked[0].name, 'src.png')
    assert.strictEqual(item.picked[0].type, 'image/png')
    const got = new Uint8Array(await item.picked[0].arrayBuffer())
    assert.deepStrictEqual(Array.from(got), Array.from(bytes))
    assert.strictEqual(document.documentElement.getAttribute('data-xbs-chooser-armed'), '0')
    assert.strictEqual(document.getElementById('xbs-qianwen-file-slot'), null)
  }

  {
    const bridge = Page.createChooserBridge(document)
    assert.strictEqual(await bridge.arm(file), true)
    const before = HTMLInputElement.nativePickers
    const pdf = new HTMLInputElement()
    pdf.type = 'file'
    pdf.accept = '.pdf,.doc,application/pdf'
    pdf.showPicker()
    assert.strictEqual(HTMLInputElement.nativePickers, before + 1, '文档选择框仍走系统文件框')
    assert.strictEqual(document.documentElement.getAttribute('data-xbs-chooser-result') || '', '')
    const img = new HTMLInputElement()
    img.type = 'file'
    img.accept = 'image/png'
    let got = 0
    img.addEventListener('change', () => { got = img.files.length })
    img.showPicker()
    assert.strictEqual(got, 1)
    assert.strictEqual(HTMLInputElement.nativePickers, before + 1)
    assert.strictEqual(document.documentElement.getAttribute('data-xbs-chooser-result'), 'ok')
    bridge.disarm()
  }

  {
    const doc = makeDoc('www.qianwen.com')
    const surface = doc.body.append(new El('div', { class: 'guidance-input-surface' }))
    surface.className = 'guidance-input-surface'
    surface.append(new El('textarea'))
    const entry = surface.append(new El('button', { 'aria-label': '添加附件' }))
    const item = doc.body.append(new El('div', { role: 'menuitem' }))
    item.addText('上传图片')
    const located = await Page.attachLocalImage(doc, file, {
      timeout: 40,
      sleep: () => sleep(5),
    })
    assert.strictEqual(located.code, 'E_SELECTOR_MISS')
    assert.strictEqual(located.error, '未找到输入区的文件选择框（上传入口旁没有 input[type=file]）')
    assert.strictEqual(entry.clicked || 0, 0, '菜单已经开着时不应再点入口')
    assert.strictEqual(item.clicked || 0, 0, '没有主世界桥时不应点上传图片')
  }

  {
    const doc = makeDoc('www.qianwen.com')
    const surface = doc.body.append(new El('div', { class: 'guidance-input-surface' }))
    surface.className = 'guidance-input-surface'
    surface.append(new El('textarea'))
    surface.append(new El('button', { 'aria-label': '添加附件' }))
    let disarmed = false
    const located = await Page.attachLocalImage(doc, file, {
      bridge: { arm() { return false }, result() { return '' }, disarm() { disarmed = true } },
    })
    assert.strictEqual(located.code, 'E_UPLOAD_FAIL')
    assert.strictEqual(disarmed, false)
  }
}

chooserFlow().then(() => {
  console.log('page.js anchors ok')
}).catch((err) => {
  console.error(err)
  process.exit(1)
})
