// 发送确认：用户气泡或新的对话 id。输入框被清空不算。
const assert = require('assert')
const fs = require('fs')
const path = require('path')
const vm = require('vm')

function el(tag, text) {
  return {
    tagName: tag.toUpperCase(),
    innerText: text || '',
    textContent: text || '',
    attrs: {},
    getAttribute(name) { return this.attrs[name] || null },
    setAttribute(name, value) { this.attrs[name] = String(value) },
  }
}

const users = []
const questions = []
const document = {
  querySelector() { return null },
  querySelectorAll(selector) {
    const sel = String(selector)
    if (sel.includes('chat-question-card-wrap')) return questions.slice()
    if (sel.includes('user')) return users.slice()
    return []
  },
  createTreeWalker(root) {
    const texts = []
    const walk = n => {
      for (const c of n.childNodes || []) {
        if (c.nodeType === 3) texts.push(c)
        else if (c.nodeType === 1) walk(c)
      }
    }
    walk(root)
    let i = 0
    return { nextNode() { return i < texts.length ? texts[i++] : null } }
  },
}
const location = { href: 'https://www.doubao.com/chat/', hostname: 'www.doubao.com' }
const context = vm.createContext({
  window: {},
  document,
  location,
  URL,
  NodeFilter: { SHOW_TEXT: 4 },
  getComputedStyle() { return { visibility: 'visible', display: 'block' } },
})
context.window = context
vm.runInContext(fs.readFileSync(path.join(__dirname, 'composer.js'), 'utf8'), context)

const accepted = (before, prompt) => context.tulaiSendAccepted(before, prompt)

assert.strictEqual(accepted('https://www.doubao.com/chat/', '雪山下的小木屋，油画风格'), false, '输入框空了但没有气泡、地址也没变，不能算发出去')
location.href = 'https://www.doubao.com/chat/'
assert.strictEqual(accepted('https://www.doubao.com/chat/old', '雪山下的小木屋，油画风格'), false, '从旧对话回到没有 id 的地址不算新对话已接受')

const bubble = el('div', '雪山下的小木屋，油画风格')
bubble.attrs['data-message-author-role'] = 'user'
users.push(bubble)
assert.strictEqual(accepted('https://www.doubao.com/chat/', '雪山下的小木屋，油画风格'), true)

users.length = 0
location.href = 'https://www.doubao.com/chat/1847291'
assert.strictEqual(accepted('https://www.doubao.com/chat/', '雪山下的小木屋，油画风格'), true, '地址出现新的对话 id 就是发送成功')
assert.strictEqual(accepted('https://www.doubao.com/chat/1847291', '雪山下的小木屋，油画风格'), false, '还停在同一个对话 id 上不算')

users.length = 0
questions.length = 0
location.hostname = 'www.qianwen.com'
context.__tulaiPageHost = 'www.qianwen.com'
location.href = 'https://www.qianwen.com/chat/78241e13b27e48a2868f7133437ca28f'
const previous = el('div', '画一只橘猫坐在窗台上，水彩风格')
previous.attrs.class = 'chat-question-card-wrap'
questions.push(previous)
assert.strictEqual(accepted('https://www.qianwen.com/', '雪山下的小木屋，油画风格'), false, '地址写回上一轮对话、气泡里没有这句提示词，不能算发出去')
questions.push(el('div', '雪山下的小木屋，油画风格'))
assert.strictEqual(accepted('https://www.qianwen.com/', '雪山下的小木屋，油画风格'), true, '新的用户气泡出现才算这句发出去了')
questions.length = 0
location.href = 'https://www.qianwen.com/chat/new-chat'
assert.strictEqual(accepted('https://www.qianwen.com/', '雪山下的小木屋，油画风格'), true, '空白页地址变成新对话才算发出去')

const src = fs.readFileSync(path.join(__dirname, 'composer.js'), 'utf8')
assert.ok(!src.includes("if (!String(text || '').trim()) return JSON.stringify({ sent: true })"), '不能再靠输入框变空确认发送')

// Tiptap/ProseMirror 与 Slate 块级段落读取不应把单换行翻倍为 \n\n，并忽略尾随占位 <br>
function makeBlock(tag, text, opts = {}) {
  const classes = String(opts.className || '').split(/\s+/).filter(Boolean)
  const childNodes = opts.childNodes || []
  const node = {
    nodeType: 1,
    tagName: tag.toUpperCase(),
    textContent: text,
    innerText: opts.innerText !== undefined ? opts.innerText : text,
    childNodes,
    children: childNodes.filter(c => c.nodeType === 1),
    firstChild: childNodes[0] || null,
    nextSibling: null,
    parentElement: null,
    attrs: opts.attrs || {},
    className: opts.className || '',
    classList: { contains(c) { return classes.includes(c) } },
    getAttribute(k) { return this.attrs[k] !== undefined ? this.attrs[k] : null },
    hasAttribute(k) { return this.attrs[k] !== undefined },
    querySelector(sel) {
      return this.querySelectorAll(sel)[0] || null
    },
    matches(sel) {
      return sel.split(',').some(part => {
        const s = part.trim()
        if (s === 'p' || s === 'div') return this.tagName === s.toUpperCase()
        if (s === 'br') return this.tagName === 'BR'
        if (s === '.ProseMirror-trailingBreak') return classes.includes('ProseMirror-trailingBreak')
        if (s === '[data-slate-zero-width]') return this.attrs['data-slate-zero-width'] != null
        if (s === '[data-slate-placeholder]') return this.attrs['data-slate-placeholder'] != null
        if (s === '[data-slate-node="element"]') return this.attrs['data-slate-node'] === 'element'
        return false
      })
    },
    querySelectorAll(sel) {
      const out = []
      const walk = n => {
        for (const c of n.childNodes || []) {
          if (c.nodeType === 1) {
            if (c.matches && c.matches(sel)) out.push(c)
            walk(c)
          }
        }
      }
      walk(this)
      return out
    },
  }
  for (let i = 0; i < childNodes.length; i++) {
    childNodes[i].parentElement = node
    childNodes[i].nextSibling = childNodes[i + 1] || null
  }
  return node
}
const pmEditor = makeBlock('div', '第一行第二行', {
  className: 'tiptap ProseMirror',
  innerText: '第一行\n\n第二行',
  attrs: { contenteditable: 'true' },
  childNodes: [
    makeBlock('p', '第一行', { childNodes: [{ nodeType: 3, nodeValue: '第一行' }] }),
    makeBlock('p', '', { childNodes: [makeBlock('br', '', { className: 'ProseMirror-trailingBreak' })] }),
    makeBlock('p', '第二行', { childNodes: [{ nodeType: 3, nodeValue: '第二行' }] }),
  ],
})
const pmRead = context.tulaiReadComposer(pmEditor)
assert.strictEqual(pmRead, '第一行\n\n第二行', 'ProseMirror 空行段落应保留真实空行而非翻倍: ' + JSON.stringify(pmRead))

const pmTwoLines = makeBlock('div', '甲乙', {
  className: 'tiptap ProseMirror',
  innerText: '甲\n\n乙',
  attrs: { contenteditable: 'true' },
  childNodes: [
    makeBlock('p', '甲', { childNodes: [{ nodeType: 3, nodeValue: '甲' }] }),
    makeBlock('p', '乙', { childNodes: [{ nodeType: 3, nodeValue: '乙' }] }),
  ],
})
assert.strictEqual(context.tulaiReadComposer(pmTwoLines), '甲\n乙', '相邻 <p> 不应被 innerText 误翻倍成 \\n\\n')

const slateEditor = makeBlock('div', '行1行2', {
  innerText: '行1\n\n行2',
  attrs: { contenteditable: 'true', 'data-slate-editor': 'true' },
  childNodes: [
    makeBlock('div', '行1', { attrs: { 'data-slate-node': 'element' }, childNodes: [{ nodeType: 3, nodeValue: '行1' }] }),
    makeBlock('div', '\uFEFF', { attrs: { 'data-slate-node': 'element' }, childNodes: [makeBlock('span', '\uFEFF', { attrs: { 'data-slate-zero-width': 'n' }, childNodes: [makeBlock('br', '')] })] }),
    makeBlock('div', '行2', { attrs: { 'data-slate-node': 'element' }, childNodes: [{ nodeType: 3, nodeValue: '行2' }] }),
  ],
})
assert.strictEqual(context.tulaiReadComposer(slateEditor), '行1\n\n行2', 'Slate 零宽空段与正文行应准确还原')

// Slate 即使残留隐藏 placeholder 节点，有正文块时也应按块级节点还原换行
const slateWithPlaceholder = makeBlock('div', '甲乙', {
  attrs: { contenteditable: 'true', 'data-slate-editor': 'true' },
  childNodes: [
    makeBlock('span', '向千问提问', { attrs: { 'data-slate-placeholder': 'true', contenteditable: 'false' }, childNodes: [{ nodeType: 3, nodeValue: '向千问提问' }] }),
    makeBlock('div', '甲', { attrs: { 'data-slate-node': 'element' }, childNodes: [{ nodeType: 3, nodeValue: '甲' }] }),
    makeBlock('div', '乙', { attrs: { 'data-slate-node': 'element' }, childNodes: [{ nodeType: 3, nodeValue: '乙' }] }),
  ],
})
assert.strictEqual(context.tulaiReadComposer(slateWithPlaceholder), '甲\n乙', 'Slate 含 placeholder 时也应保留块级换行并忽略占位文本')

// tulaiCleanComposer：覆盖豆包 chat_input 作用域下的 display:none SVG 删除按钮（逐张重查），以及千问 imageBoxWrapper 关闭图标
async function testCleanComposerLiveDOMPatterns() {
  const mkClickCtx = (comp, scopeMap) => {
    const dispatched = []
    const c = vm.createContext({
      window: {},
      setTimeout,
      clearTimeout,
      MouseEvent: class MouseEvent { constructor(type, init) { this.type = type; Object.assign(this, init) } },
      URL,
      NodeFilter: { SHOW_TEXT: 4 },
      getComputedStyle() { return { visibility: 'visible', display: 'block' } },
      location: { href: 'https://www.doubao.com/chat/', hostname: 'www.doubao.com' },
      tulaiClick(el) { return Promise.resolve(Boolean(el && el._clickable)) },
      document: {
        body: {},
        documentElement: {},
        querySelectorAll(sel) {
          if (sel.includes('chat_input_input') || sel.includes('contenteditable')) return [comp]
          return []
        },
      },
    })
    c.window = c
    vm.runInContext(fs.readFileSync(path.join(__dirname, 'composer.js'), 'utf8'), c)
    return { c, dispatched }
  }

  // 1) 豆包：2 张卡片，每张的 <svg data-testid="attachment-delete-btn"> 不可 tulaiClick，需派发 MouseEvent 且逐张重查删光
  const doubaoBtns = [
    { id: 'svg-1', _clickable: false, isConnected: true },
    { id: 'svg-2', _clickable: false, isConnected: true },
  ]
  const removedDoubao = []
  for (const b of doubaoBtns) {
    b.dispatchEvent = () => {
      removedDoubao.push(b.id)
      const idx = doubaoBtns.indexOf(b)
      if (idx >= 0) doubaoBtns.splice(idx, 1)
    }
  }
  const chatInput = {
    querySelectorAll(sel) { return sel.includes('attachment-delete-btn') ? doubaoBtns.slice() : [] },
  }
  const doubaoComp = {
    tagName: 'DIV', offsetParent: {}, clientWidth: 200, clientHeight: 40, disabled: false,
    getAttribute(k) { return k === 'contenteditable' ? 'true' : '' },
    closest(sel) { return sel === '[data-testid="chat_input"]' ? chatInput : null },
  }
  const { c: dCtx } = mkClickCtx(doubaoComp)
  assert.strictEqual(await dCtx.tulaiCleanComposer(), true)
  assert.deepStrictEqual(removedDoubao, ['svg-1', 'svg-2'], '豆包多张隐藏 svg 删除按钮应逐张重查并全部清理')

  // 2) 千问：附件位于内层发送区外一层，且必须匹配 imageBoxWrapper 内的 qwpcicon-close2，不误点其他关闭按钮
  let qwenRemoved = 0
  let unrelatedClicked = 0
  const qwenClose = { id: 'qw-close', _clickable: true, isConnected: true }
  const qwenOuter = {
    querySelector(sel) { return sel.includes('imageBoxWrapper') && qwenRemoved === 0 ? {} : null },
    querySelectorAll(sel) {
      if (sel.includes('qwpcicon-close2') && qwenRemoved === 0) return [qwenClose]
      return []
    },
    parentElement: null,
  }
  const qwenInner = {
    hasAttribute() { return false },
    querySelector(sel) { return null },
    querySelectorAll(sel) {
      if (sel.includes('button')) return [{ textContent: '发送', getAttribute() { return '' } }]
      return []
    },
    parentElement: qwenOuter,
  }
  const qwenComp = {
    tagName: 'DIV', offsetParent: {}, clientWidth: 200, clientHeight: 40, disabled: false,
    parentElement: qwenInner,
    getAttribute(k) { return k === 'data-slate-editor' ? 'true' : '' },
    closest() { return null },
  }
  const { c: qCtx } = mkClickCtx(qwenComp)
  qCtx.tulaiClick = (el) => {
    if (el === qwenClose) { qwenRemoved++; return Promise.resolve(true) }
    unrelatedClicked++
    return Promise.resolve(true)
  }
  assert.strictEqual(await qCtx.tulaiCleanComposer(), true)
  assert.strictEqual(qwenRemoved, 1, '千问 imageBoxWrapper 内关闭图标应被点击清理')
  assert.strictEqual(unrelatedClicked, 0)
}

testCleanComposerLiveDOMPatterns().then(() => {
  console.log('composer send ok')
}).catch((err) => {
  console.error(err)
  process.exit(1)
})
