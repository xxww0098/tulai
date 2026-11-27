// Gemini（gemini.google.com）历史对话删除脚本单测：URL 解析、登录态校验、侧边栏收集与 DOM 删除流程。
const assert = require('assert')
const fs = require('fs')
const path = require('path')
const vm = require('vm')

function splitSelectorParts(sel) {
  return String(sel || '').split(',').map((s) => s.trim()).filter(Boolean)
}

function matchSimple(node, sel) {
  if (!node || node.nodeType !== 1) return false
  let rest = String(sel || '').trim().replace(/\s+i\]/g, ']')
  if (!rest) return false
  if (/\s+/.test(rest)) {
    const chain = rest.split(/\s+/)
    const last = chain.pop()
    if (!matchSimple(node, last)) return false
    let cur = node.parentElement
    while (cur && chain.length) {
      if (matchSimple(cur, chain[chain.length - 1])) chain.pop()
      cur = cur.parentElement
    }
    return chain.length === 0
  }
  const tagM = rest.match(/^[a-zA-Z][\w-]*/)
  if (tagM) {
    if (node.tagName !== tagM[0].toUpperCase()) return false
    rest = rest.slice(tagM[0].length)
  }
  while (rest.length) {
    if (rest.startsWith('#')) {
      const m = rest.match(/^#([^\s.#[:]+)/)
      if (!m || node.id !== m[1]) return false
      rest = rest.slice(m[0].length)
    } else if (rest.startsWith('.')) {
      const m = rest.match(/^\.([^\s.#[:]+)/)
      const classes = String(node.className || '').split(/\s+/).filter(Boolean)
      if (!m || !classes.includes(m[1])) return false
      rest = rest.slice(m[0].length)
    } else if (rest.startsWith('[')) {
      const end = rest.indexOf(']')
      if (end < 0) return false
      const body = rest.slice(1, end).replace(/\s+i$/, '')
      rest = rest.slice(end + 1)
      let name, op, val
      if (body.includes('*=')) {
        const i = body.indexOf('*=')
        name = body.slice(0, i); op = '*='; val = body.slice(i + 2).replace(/^["']|["']$/g, '')
      } else if (body.includes('=')) {
        const i = body.indexOf('=')
        name = body.slice(0, i); op = '='; val = body.slice(i + 1).replace(/^["']|["']$/g, '')
      } else {
        name = body; op = 'has'
      }
      const got = name === 'id' ? node.id : (name === 'class' ? node.className : node.getAttribute(name))
      if (op === 'has' && got == null) return false
      if (op === '=' && String(got || '').toLowerCase() !== val.toLowerCase()) return false
      if (op === '*=' && !String(got || '').toLowerCase().includes(val.toLowerCase())) return false
    } else {
      return false
    }
  }
  return true
}

function matchAny(node, selector) {
  return splitSelectorParts(selector).some((part) => matchSimple(node, part))
}

function queryAll(root, selector) {
  const out = []
  const seen = new Set()
  const walk = (n) => {
    for (const c of n.childNodes || []) {
      if (c.nodeType === 1) {
        if (matchAny(c, selector) && !seen.has(c)) {
          seen.add(c)
          out.push(c)
        }
        walk(c)
      }
    }
  }
  walk(root)
  return out
}

function el(tag, opts = {}) {
  const attrs = Object.assign({}, opts.attrs || {})
  const node = {
    tagName: String(tag || 'div').toUpperCase(),
    nodeType: 1,
    id: opts.id || attrs.id || '',
    className: opts.className || attrs.class || '',
    childNodes: [],
    parentElement: null,
    _text: opts.text || '',
    get innerText() {
      const childText = node.childNodes.map((c) => c.innerText || '').filter(Boolean).join('\n')
      return [node._text, childText].filter(Boolean).join('\n')
    },
    get textContent() {
      const childText = node.childNodes.map((c) => c.textContent || '').filter(Boolean).join(' ')
      return [node._text, childText].filter(Boolean).join(' ')
    },
    disabled: Boolean(opts.disabled),
    isConnected: true,
    attrs,
    getAttribute(k) {
      if (k === 'id') return node.id || null
      if (k === 'class') return node.className || null
      return Object.prototype.hasOwnProperty.call(node.attrs, k) ? node.attrs[k] : null
    },
    appendChild(child) {
      child.parentElement = node
      child.isConnected = true
      node.childNodes.push(child)
      return child
    },
    removeChild(child) {
      const i = node.childNodes.indexOf(child)
      if (i >= 0) node.childNodes.splice(i, 1)
      child.parentElement = null
      child.isConnected = false
    },
    closest(sel) {
      let cur = node
      while (cur && cur.nodeType === 1) {
        if (matchAny(cur, sel)) return cur
        cur = cur.parentElement
      }
      return null
    },
    querySelectorAll(sel) { return queryAll(node, sel) },
    querySelector(sel) { return queryAll(node, sel)[0] || null },
    getBoundingClientRect() { return opts.hidden ? { width: 0, height: 0, left: 0, top: 0 } : { width: 140, height: 32, left: 10, top: 10 } },
    focus() {},
    click() { if (opts.onClick) opts.onClick(node) },
    dispatchEvent() {},
  }
  return node
}

function makeHistoryEnv(initialHref) {
  const body = el('body')
  const doc = {
    body,
    childNodes: [body],
    querySelectorAll(sel) { return queryAll(body, sel) },
    querySelector(sel) { return queryAll(body, sel)[0] || null },
  }
  body.parentElement = doc
  const ctx = vm.createContext({
    console,
    URL,
    Set,
    Promise,
    setTimeout,
    clearTimeout,
    location: { href: initialHref || 'https://gemini.google.com/app' },
    getComputedStyle() { return { display: 'block', visibility: 'visible', opacity: '1' } },
    MouseEvent: class {},
    tulaiHover() { return Promise.resolve(true) },
    tulaiClick(target) {
      if (target && typeof target.click === 'function') target.click()
      return Promise.resolve(true)
    },
  })
  ctx.window = ctx
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'gemini_history.js'), 'utf8'), ctx)
  return { api: ctx.__tulaiGeminiHistory, doc, body }
}

async function run() {
  // 1) URL ID 解析
  {
    const { api } = makeHistoryEnv()
    assert.strictEqual(api.conversationIdFromHref('https://gemini.google.com/app/abcdef123456'), 'abcdef123456')
    assert.strictEqual(api.conversationIdFromHref('https://gemini.google.com/u/2/app/abcdef123456'), 'abcdef123456')
    assert.strictEqual(api.conversationIdFromHref('https://gemini.google.com/app'), '')
    assert.strictEqual(api.conversationIdFromHref('https://gemini.google.com/images'), '')
    assert.strictEqual(api.conversationIdFromHref('https://gemini.google.com/app/gems'), '')
  }

  // 2) 未登录墙检测与已登录放行（包含 sidenav-mavatar-migration-enabled 样式类名不得误报）
  {
    const { api, doc, body } = makeHistoryEnv()
    body.appendChild(el('div', { className: 'sidenav-mavatar-migration-enabled' }))
    body.appendChild(el('a', { attrs: { 'data-test-id': 'sidenav-error-action-link' }, text: '登录' }))
    assert.strictEqual(api.checkBlocked(doc).stopped, 'not_logged_in')

    body.appendChild(el('button', { attrs: { 'gem-open-account-menu': '' }, text: 'User' }))
    assert.strictEqual(api.checkBlocked(doc), null)
  }

  // 3) 侧边栏收集 + DOM 逐条删除（含确认弹层）
  {
    const { api, doc, body } = makeHistoryEnv('https://gemini.google.com/app/conv-2')
    body.appendChild(el('button', { attrs: { 'gem-open-account-menu': '' }, text: 'User' }))
    const sidenav = el('side-navigation-v2')
    body.appendChild(sidenav)

    function addConv(id, title) {
      const container = el('div', { className: 'conversation-items-container' })
      const row = el('div', { attrs: { 'data-test-id': 'conversation' } })
      row.appendChild(el('a', { attrs: { href: '/app/' + id }, text: title }))
      const menuBtn = el('button', {
        attrs: { 'data-test-id': 'actions-menu-button', 'aria-label': '更多选项' },
        onClick() {
          const overlay = el('div', { className: 'cdk-overlay-container' })
          const menu = el('div', { attrs: { role: 'menu' } })
          menu.appendChild(el('button', {
            attrs: { 'data-test-id': 'delete-button' },
            text: '删除',
            onClick() {
              body.removeChild(overlay)
              const dlg = el('div', { attrs: { role: 'dialog' } })
              dlg.appendChild(el('button', {
                attrs: { 'data-test-id': 'confirm-button' },
                text: '删除',
                onClick() {
                  body.removeChild(dlg)
                  sidenav.removeChild(container)
                },
              }))
              body.appendChild(dlg)
            },
          }))
          overlay.appendChild(menu)
          body.appendChild(overlay)
        },
      })
      container.appendChild(row)
      container.appendChild(menuBtn)
      sidenav.appendChild(container)
    }

    addConv('conv-1', '最新一条')
    addConv('conv-2', '第二条')
    addConv('conv-3', '第三条')

    const col = await api.collectHistoryIds(doc, ['conv-3'], 1, 10)
    assert.strictEqual(col.ok, true)
    assert.deepStrictEqual(Array.from(col.keepIds), ['conv-1'])
    assert.deepStrictEqual(Array.from(col.ids), ['conv-2'])

    const step = await api.deleteNextHistoryItem(doc, [], 1, null, 5000)
    assert.strictEqual(step.ok, true)
    assert.strictEqual(step.deleted, true)
    assert.strictEqual(step.id, 'conv-2')
    assert.strictEqual(step.remaining, 2)

    const delDirect = await api.deleteConversation(doc, 'conv-3', null, 5000)
    assert.strictEqual(delDirect.ok, true)
    assert.strictEqual(delDirect.deleted, true)
    assert.strictEqual(delDirect.remaining, 1)

    const doneStep = await api.deleteNextHistoryItem(doc, [], 1, null, 5000)
    assert.strictEqual(doneStep.ok, true)
    assert.strictEqual(doneStep.done, true)
    assert.strictEqual(doneStep.remaining, 1)
  }

  console.log('gemini_history_test ok')
}

run().catch((err) => {
  console.error(err)
  process.exit(1)
})
