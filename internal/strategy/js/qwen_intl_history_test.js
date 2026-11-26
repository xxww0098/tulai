// 千问国际（chat.qwen.ai）历史对话删除脚本单测：API 分页、单条删除、并发波删、降级 v1 及风控/登录态拦截。
const assert = require('assert')
const fs = require('fs')
const path = require('path')
const vm = require('vm')

function makeEl(tag, opts = {}) {
  const attrs = Object.assign({}, opts.attrs || {})
  return {
    tagName: String(tag || 'div').toUpperCase(),
    id: opts.id || '',
    className: opts.className || '',
    innerText: opts.text || '',
    textContent: opts.text || '',
    href: attrs.href || '',
    isConnected: opts.connected !== false,
    attrs,
    getAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k) ? this.attrs[k] : null },
    getBoundingClientRect() { return opts.rect || { width: 100, height: 32, left: 0, top: 0 } },
  }
}

function loadHistory(opts = {}) {
  const calls = []
  const elementsBySelector = opts.elements || {}
  const doc = {
    querySelectorAll(sel) {
      const out = []
      for (const [key, list] of Object.entries(elementsBySelector)) {
        if (sel.includes(key)) out.push(...list)
      }
      return out
    },
  }
  const storage = opts.token ? { getItem: (k) => (k === 'token' ? opts.token : null) } : { getItem: () => null }
  const ctx = vm.createContext({
    console,
    URL,
    Set,
    Promise,
    setTimeout,
    clearTimeout,
    location: { href: opts.href || 'https://chat.qwen.ai/' },
    localStorage: storage,
    getComputedStyle() { return { display: 'block', visibility: 'visible', opacity: '1' } },
    fetch(url, init) {
      calls.push({ url, init })
      return opts.fetch(url, init)
    },
  })
  ctx.window = ctx
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'qwen_intl_history.js'), 'utf8'), ctx)
  return { api: ctx.__tulaiQwenIntlHistory, doc, calls }
}

function jsonResp(status, body) {
  const text = typeof body === 'string' ? body : JSON.stringify(body)
  return Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    text: () => Promise.resolve(text),
  })
}

async function run() {
  // 1) URL 会话 ID 解析（仅接受 /c/<uuid>，排除临时路由）
  {
    const { api } = loadHistory({ fetch: () => jsonResp(200, []) })
    assert.strictEqual(api.conversationIdFromHref('https://chat.qwen.ai/c/550e8400-e29b-41d4-a716-446655440000'), '550e8400-e29b-41d4-a716-446655440000')
    assert.strictEqual(api.conversationIdFromHref('https://chat.qwen.ai/c/new-chat'), '')
    assert.strictEqual(api.conversationIdFromHref('https://chat.qwen.ai/c/new-branch'), '')
    assert.strictEqual(api.conversationIdFromHref('https://chat.qwen.ai/c/guest'), '')
    assert.strictEqual(api.conversationIdFromHref('https://chat.qwen.ai/c/local'), '')
    assert.strictEqual(api.conversationIdFromHref('https://chat.qwen.ai/'), '')
  }

  // 2) 未登录墙检测 & 已登录菜单放行
  {
    const { api, doc } = loadHistory({
      elements: { '.header-right-auth-button': [makeEl('button', { text: 'Log in' })] },
      fetch: () => jsonResp(200, []),
    })
    assert.strictEqual(api.checkBlocked(doc).stopped, 'not_logged_in')

    const logged = loadHistory({
      elements: {
        '.user-menu-btn': [makeEl('div', { text: 'Alice' })],
        '.header-right-auth-button': [makeEl('button', { text: 'Log in' })],
      },
      fetch: () => jsonResp(200, []),
    })
    assert.strictEqual(logged.api.checkBlocked(logged.doc), null)
  }

  // 3) 分页收集、保留最近 N 条、单条删除、并发波删（携带 Bearer token）
  {
    const store = [
      { id: 'c1', title: '最新一条' },
      { id: 'c2', title: '第二条' },
      { id: 'c3', title: '第三条' },
      { id: 'c4', title: '第四条' },
    ]
    const { api, doc, calls } = loadHistory({
      token: 'tok_intl_123',
      elements: { '.user-menu-btn': [makeEl('div', { text: 'Alice' })] },
      fetch(url, init) {
        if (init.method === 'GET' && url.includes('page=1')) {
          return jsonResp(200, { success: true, data: store.slice() })
        }
        if (init.method === 'GET') {
          return jsonResp(200, { success: true, data: [] })
        }
        if (init.method === 'DELETE') {
          const id = decodeURIComponent(url.split('/').pop())
          const idx = store.findIndex((x) => x.id === id)
          if (idx >= 0) store.splice(idx, 1)
          return jsonResp(200, { success: true })
        }
        return jsonResp(404, {})
      },
    })

    const col = await api.collectHistoryIds(doc, ['c3'], 1, 10)
    assert.strictEqual(col.ok, true)
    assert.deepStrictEqual(Array.from(col.keepIds), ['c1'])
    assert.deepStrictEqual(Array.from(col.ids), ['c2', 'c4'])
    assert.strictEqual(calls[0].init.headers.Authorization, 'Bearer tok_intl_123')

    const next = await api.deleteNextHistoryItem(doc, [], 1, null)
    assert.strictEqual(next.ok, true)
    assert.strictEqual(next.deleted, true)
    assert.strictEqual(next.id, 'c2')
    assert.deepStrictEqual(store.map((x) => x.id), ['c1', 'c3', 'c4'])

    const wave = await api.deleteWave(doc, ['c3', 'c4'], 2)
    assert.strictEqual(wave.ok, true)
    assert.deepStrictEqual(Array.from(wave.deletedIds).sort(), ['c3', 'c4'])
    assert.deepStrictEqual(store.map((x) => x.id), ['c1'])
  }

  // 4) v2 返回 404 时自动回退 v1；429 触发 rate_limit；baxia 触发 captcha
  {
    let v1Called = false
    const { api, doc } = loadHistory({
      elements: { '.user-menu-btn': [makeEl('div', { text: 'Alice' })] },
      fetch(url, init) {
        if (url.includes('/api/v2/chats/')) return jsonResp(404, { detail: 'not found' })
        if (url.includes('/api/v1/chats/old-1') && init.method === 'DELETE') {
          v1Called = true
          return jsonResp(200, { success: true })
        }
        if (url.includes('/api/v1/chats/rate-1')) return jsonResp(429, { detail: 'Too many requests' })
        if (url.includes('/api/v1/chats/cap-1')) return jsonResp(200, { rgv587_flag: 'sm', url: 'baxia' })
        return jsonResp(401, { detail: 'Unauthorized' })
      },
    })
    const okRes = await api.deleteConversation(doc, 'old-1')
    assert.strictEqual(okRes.ok, true)
    assert.strictEqual(v1Called, true)

    const rateRes = await api.deleteConversation(doc, 'rate-1')
    assert.strictEqual(rateRes.ok, false)
    assert.strictEqual(rateRes.stopped, 'rate_limit')

    const capRes = await api.deleteConversation(doc, 'cap-1')
    assert.strictEqual(capRes.ok, false)
    assert.strictEqual(capRes.stopped, 'captcha')

    const authRes = await api.deleteConversation(doc, 'unauth-1')
    assert.strictEqual(authRes.ok, false)
    assert.strictEqual(authRes.stopped, 'not_logged_in')
  }

  console.log('qwen_intl_history_test ok')
}

run().catch((err) => {
  console.error(err)
  process.exit(1)
})
