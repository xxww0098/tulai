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
    const got = node.attrs && node.attrs[name]
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
assert.strictEqual(state.done, false, '图已经出来但格子还没 data-finished，不能结束')
assert.strictEqual(state.reason, 'unfinished')
assert.strictEqual(state.images.length, 1)
assert.ok(state.images[0].url.includes('cgen_lwm'), '应选用原图地址而不是缩略图: ' + state.images[0].url)
current.attrs['data-finished'] = 'true'
state = live()
assert.strictEqual(state.done, true, 'data-finished=true 后这一轮才算完成')
assert.strictEqual(state.reason, 'images-ready')

addImage('https://cdn.example/new-2.png')
addImage('https://cdn.example/new-3.png')
addImage('https://cdn.example/new-4.png')
state = live()
assert.strictEqual(state.done, true, '没有重新生成按钮也要能结束')
assert.strictEqual(state.bar, false)
assert.strictEqual(state.images.length, 4)
for (const url of urls(state)) assert.ok(!url.includes('/old-'), url)

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
assert.strictEqual(state.done, false, '声明张数不是完成信号，格子没写 data-finished 不能结束')
counted.attrs['data-finished'] = 'true'
state = live()
assert.strictEqual(state.done, true, 'data-finished 之后不再等声明张数')
assert.deepStrictEqual(urls(state).filter((url) => url.includes('/old-') || url.includes('/new-')), [])

context.__tulaiMarkTask()
const shell = reply([])
const shellCard = imageCard('data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg"/>')
shell.appendChild(shellCard.card)
body.appendChild(shell)
context.__tulaiMarkTask()
shellCard.img.src = 'https://p3-flow-imagex-sign.byteimg.com/tos/cat.png'
shellCard.img.currentSrc = shellCard.img.src
shell.attrs['data-streaming'] = 'false'
state = live()
assert.deepStrictEqual(urls(state), ['https://p3-flow-imagex-sign.byteimg.com/tos/cat.png'], '占位壳换成实图后不能被基准元素 id 丢掉')
assert.strictEqual(state.done, true)

const bare = el('div')
bare.attrs['data-testid'] = 'receive_message'
bare.attrs['data-finished'] = 'true'
const grid = el('div')
grid.attrs.class = 'image-box-grid-item-FTeESI'
grid.attrs['data-testid'] = 'message_image_content'
const bareCard = imageCard('https://p3-flow-imagex-sign.byteimg.com/tos/rc_gen_image/bare.jpeg~tplv-a9rns2rl98-cthumb.png', 'https://p3-flow-imagex-sign.byteimg.com/tos/rc_gen_image/bare.jpeg~tplv-a9rns2rl98-cgen_lwm.png')
grid.appendChild(bareCard.card)
bare.appendChild(grid)
body.appendChild(bare)
state = live()
assert.ok(urls(state).some((url) => url.includes('cgen_lwm') && url.includes('rc_gen_image')), 'receive_message 里 message_image_content 的原图要留下: ' + urls(state).join(','))

const user = el('div')
user.attrs['data-testid'] = 'send_message'
user.attrs['data-message-role'] = 'user'
const userMedia = el('div')
userMedia.attrs['data-testid'] = 'message_image_content'
const userCard = imageCard('https://cdn.example/user-upload.png')
userMedia.appendChild(userCard.card)
user.appendChild(userMedia)
body.appendChild(user)
state = live()
assert.ok(!urls(state).includes('https://cdn.example/user-upload.png'), '用户消息上的 message_image_content 不是生成图')

const icon = el('button')
icon.attrs['aria-label'] = ''
bare.appendChild(icon)
state = live()
assert.strictEqual(state.done, true, '图标按钮没有重新生成文案也要结束')
assert.strictEqual(state.bar, false)
const copy = el('button')
copy.attrs['data-testid'] = 'message_action_copy'
copy.innerText = ''
bare.appendChild(copy)
bare.attrs['data-finished'] = 'false'
state = live()
assert.strictEqual(state.bar, true, 'message_action_copy 即使没有文案也是完成条')
assert.strictEqual(state.done, true)

const button = el('button')
button.innerText = '新对话'
body.appendChild(button)
assert.strictEqual(context.__tulaiFreshChat(), 'button')
assert.strictEqual(button.clicked, 1, '停在旧对话时才点新对话')
assert.deepStrictEqual(assigned, [])

location.href = 'https://www.doubao.com/chat/'
assert.strictEqual(context.__tulaiFreshChat(), 'button', '地址是 /chat/ 但旧回复还在，仍要点新对话')
assert.strictEqual(button.clicked, 2)

body.childNodes = []
const box = el('textarea')
box.value = '雪山下的小木屋，油画风格'
body.appendChild(box)
assert.strictEqual(context.__tulaiChatReady().ready, false, '输入框里还有草稿不能打字')
assert.strictEqual(context.__tulaiFreshChat(), 'reload', '草稿还在且没有新对话按钮时要重新打开空白页')

box.value = ''
let ready = context.__tulaiChatReady()
assert.strictEqual(ready.ready, true)
assert.strictEqual(ready.id, '')
assert.strictEqual(ready.chars, 0)
const node = ready.node
ready = context.__tulaiChatReady()
assert.strictEqual(ready.node, node, '输入框没被换掉时节点要稳定')
assert.strictEqual(context.__tulaiFreshChat(), 'ready', '空白新对话不要再点')

location.href = 'https://www.doubao.com/chat/still-old'
ready = context.__tulaiChatReady()
assert.strictEqual(ready.ready, false)
assert.strictEqual(ready.id, 'still-old', '旧对话 id 还在就不能当成新对话已经准备好')

console.log('hook live images ok')
