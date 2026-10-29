// 豆包完成信号和基准图排除。1 张、2 张、4 张陆续出现时不能结束。
const assert = require('assert')
const fs = require('fs')
const path = require('path')
const vm = require('vm')

function match(node, sel) {
  sel = String(sel || '').trim()
  if (!sel) return false
  let tag = ''
  const tagMatch = sel.match(/^[a-zA-Z][\w-]*/)
  if (tagMatch) {
    tag = tagMatch[0].toUpperCase()
    sel = sel.slice(tagMatch[0].length)
  }
  if (tag && node.tagName !== tag) return false
  while (sel.startsWith('[')) {
    const end = sel.indexOf(']')
    if (end < 0) return false
    const body = sel.slice(1, end)
    sel = sel.slice(end + 1)
    let name, op, value
    if (body.includes('*=')) {
      const at = body.indexOf('*=')
      name = body.slice(0, at)
      op = '*='
      value = body.slice(at + 2).replace(/^["']|["']$/g, '')
    } else if (body.includes('=')) {
      const at = body.indexOf('=')
      name = body.slice(0, at)
      op = '='
      value = body.slice(at + 1).replace(/^["']|["']$/g, '')
    } else {
      name = body
      op = 'has'
    }
    const got = node.attrs[name]
    if (op === 'has' && (got == null || got === '')) return false
    if (op === '=' && String(got) !== value) return false
    if (op === '*=' && String(got || '').indexOf(value) < 0) return false
  }
  if (sel.startsWith('.')) {
    return String(node.attrs.class || '').split(/\s+/).includes(sel.slice(1))
  }
  return tag !== '' || sel === ''
}

function queryAll(root, selector) {
  const parts = String(selector || '').split(',')
  const out = []
  const seen = new Set()
  function walk(node) {
    for (const child of node.childNodes || []) {
      if (parts.some((part) => match(child, part))) {
        if (!seen.has(child)) {
          seen.add(child)
          out.push(child)
        }
      }
      walk(child)
    }
  }
  walk(root)
  return out
}

function el(tag) {
  const node = {
    tagName: String(tag || 'div').toUpperCase(),
    nodeType: 1,
    childNodes: [],
    attrs: {},
    innerText: '',
    src: '',
    currentSrc: '',
    naturalWidth: 512,
    naturalHeight: 512,
    parentElement: null,
    clicked: 0,
    setAttribute(name, value) { this.attrs[name] = String(value) },
    getAttribute(name) { return Object.prototype.hasOwnProperty.call(this.attrs, name) ? this.attrs[name] : null },
    appendChild(child) {
      child.parentElement = this
      this.childNodes.push(child)
      return child
    },
    querySelectorAll(selector) { return queryAll(this, selector) },
    querySelector(selector) { return queryAll(this, selector)[0] || null },
    closest(selector) {
      let cur = this
      while (cur) {
        if (match(cur, selector)) return cur
        cur = cur.parentElement
      }
      return null
    },
    getBoundingClientRect() { return { width: 200, height: 200, left: 0, top: 0 } },
    click() { this.clicked++ },
  }
  return node
}

function imageCard(src, href) {
  const card = el('div')
  card.attrs['data-testid'] = 'mdbox_image'
  const link = el('a')
  if (href) link.attrs.href = href
  const img = el('img')
  img.src = src
  img.currentSrc = src
  link.appendChild(img)
  card.appendChild(img.closest ? img : img)
  card.appendChild(link)
  link.appendChild(img)
  // link.appendChild already set parent; card.appendChild(link) below if not
  if (link.parentElement !== card) card.appendChild(link)
  return { card, img }
}

function reply(images, opts) {
  const box = el('div')
  box.attrs['data-message-author-role'] = 'assistant'
  if (opts && opts.count) box.attrs['data-count'] = String(opts.count)
  if (opts && opts.text) box.innerText = opts.text
  for (const item of images) {
    const made = imageCard(item.src, item.href)
    box.appendChild(made.card)
  }
  if (opts && opts.action) {
    const button = el('button')
    button.innerText = opts.action
    box.appendChild(button)
  }
  return box
}

const body = el('body')
const doc = {
  body,
  readyState: 'complete',
  childNodes: [body],
  addEventListener() {},
  querySelectorAll(selector) { return queryAll(this, selector) },
  querySelector(selector) { return queryAll(this, selector)[0] || null },
}
body.parentElement = doc

const assigned = []
const location = {
  hostname: 'www.doubao.com',
  href: 'https://www.doubao.com/chat/old-thread',
  assign(url) { assigned.push(url); this.href = url },
}

class XMLHttpRequest {
  open() {}
  send() {}
  addEventListener() {}
}
const context = vm.createContext({
  console,
  URL,
  WeakSet,
  Set,
  document: doc,
  location,
  XMLHttpRequest,
  fetch() { return Promise.resolve({ headers: { get() { return '' } }, clone() { return this }, text() { return Promise.resolve('') }, blob() { return Promise.resolve({ size: 0 }) } }) },
  MutationObserver: class { observe() {} },
})
context.window = context
context.globalThis = context
vm.runInContext(fs.readFileSync(path.join(__dirname, 'hook.js'), 'utf8'), context)

const live = () => context.__tulaiLiveImages()
const urls = (state) => Array.from(state.images, (item) => item.url)

const old = []
for (let i = 0; i < 5; i++) old.push({ src: 'https://cdn.example/old-' + i + '.png' })
body.appendChild(reply(old, { action: '重新生成' }))
context.__tulaiMarkTask()
let state = live()
assert.strictEqual(state.done, false, '上一轮对话不能在提交前就算完成')
assert.deepStrictEqual(urls(state), [], '基准里的 5 张不能进入本轮')

const current = reply([])
body.appendChild(current)
function addImage(src, href) {
  const made = imageCard(src, href)
  current.appendChild(made.card)
  return made
}
addImage('https://p3-flow-imagex-sign.byteimg.com/tos/a.jpeg~tplv-a9rns2rl98-cthumb.png', 'https://p3-flow-imagex-sign.byteimg.com/tos/a.jpeg~tplv-a9rns2rl98-cgen_lwm.png')
state = live()
assert.strictEqual(state.done, false, '只有 1 张且没有完成信号，不能结束')
assert.strictEqual(state.images.length, 1)
assert.ok(state.images[0].url.includes('cgen_lwm'), '应选用原图地址而不是缩略图: ' + state.images[0].url)

addImage('https://cdn.example/new-2.png')
state = live()
assert.strictEqual(state.done, false, '2 张仍是半成品')
assert.strictEqual(state.images.length, 2)

addImage('https://cdn.example/new-3.png')
addImage('https://cdn.example/new-4.png')
state = live()
assert.strictEqual(state.done, false, '4 张齐了但豆包还没给出完成信号，不能结束')
assert.strictEqual(state.images.length, 4)
for (const url of urls(state)) assert.ok(!url.includes('/old-'), url)

const again = el('button')
again.innerText = '重新生成'
current.appendChild(again)
state = live()
assert.strictEqual(state.done, true, '重新生成出现后才算这一轮完成')
assert.strictEqual(state.images.length, 4)
assert.deepStrictEqual(urls(state).filter((url) => url.includes('/old-')), [])

const stop = el('button')
stop.innerText = '停止'
body.appendChild(stop)
state = live()
assert.strictEqual(state.done, false, '停止按钮还在时不能把半成品收走')
stop.parentElement = null
body.childNodes.pop()

context.__tulaiMarkTask()
const counted = reply([
  { src: 'https://cdn.example/grid-1.png' },
  { src: 'https://cdn.example/grid-2.png' },
], { count: 4 })
body.appendChild(counted)
state = live()
assert.strictEqual(state.done, false, '声明 4 张时只有 2 张不能结束')
counted.appendChild(imageCard('https://cdn.example/grid-3.png').card)
counted.appendChild(imageCard('https://cdn.example/grid-4.png').card)
state = live()
assert.strictEqual(state.done, true, '格子达到声明的最终张数才结束')
assert.strictEqual(state.images.length, 4)
assert.deepStrictEqual(urls(state).filter((url) => url.includes('/old-') || url.includes('/new-')), [])

context.__tulaiMarkTask()
const reused = reply([{ src: 'https://cdn.example/kept.png' }], { action: '重新生成' })
body.appendChild(reused)
context.__tulaiMarkTask()
reused.appendChild(imageCard('https://cdn.example/appended.png').card)
state = live()
assert.strictEqual(state.done, false, '追加进旧回复的第一张不能靠旧的重新生成按钮结束')
assert.deepStrictEqual(urls(state), ['https://cdn.example/appended.png'])
const freshAction = el('button')
freshAction.innerText = '复制'
reused.appendChild(freshAction)
state = live()
assert.strictEqual(state.done, true, '这一轮新出现的复制按钮才算完成')
assert.deepStrictEqual(urls(state), ['https://cdn.example/appended.png'])

const button = el('button')
button.innerText = '新对话'
body.appendChild(button)
assert.strictEqual(context.__tulaiFreshChat(), 'button')
assert.strictEqual(button.clicked, 1, '每单要点新对话，不能继续用上一轮页面')
assert.deepStrictEqual(assigned, [])

console.log('hook live images ok')
