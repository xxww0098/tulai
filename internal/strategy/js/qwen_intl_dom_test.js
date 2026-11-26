// 千问国际（chat.qwen.ai）页面脚本集成单测：覆盖 hook.js（新对话、结果采集、排除推荐图与用户上传图、加载态/报错态）与 composer.js（+ 菜单上传、发送确认、附件清理、额度/限流/审核识别）。
const assert = require('assert')
const fs = require('fs')
const path = require('path')
const vm = require('vm')

function splitSelectorParts(sel) {
  return String(sel || '').split(',').map((s) => s.trim()).filter(Boolean)
}

function matchSimple(node, sel) {
  if (!node || node.nodeType !== 1) return false
  let rest = String(sel || '').trim()
  if (!rest) return false
  // Handle simple descendant combinator "A B"
  if (/\s+/.test(rest)) {
    const chain = rest.split(/\s+/)
    const last = chain.pop()
    if (!matchSimple(node, last)) return false
    let cur = node.parentElement
    while (cur && chain.length) {
      if (matchSimple(cur, chain[chain.length - 1])) {
        chain.pop()
      }
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
      if (body.includes('^=')) {
        const i = body.indexOf('^=')
        name = body.slice(0, i); op = '^='; val = body.slice(i + 2).replace(/^["']|["']$/g, '')
      } else if (body.includes('*=')) {
        const i = body.indexOf('*=')
        name = body.slice(0, i); op = '*='; val = body.slice(i + 2).replace(/^["']|["']$/g, '')
      } else if (body.includes('=')) {
        const i = body.indexOf('=')
        name = body.slice(0, i); op = '='; val = body.slice(i + 1).replace(/^["']|["']$/g, '')
      } else {
        name = body; op = 'has'
      }
      const got = name === 'id' ? node.id : (name === 'class' ? node.className : node.getAttribute(name))
      if (op === 'has' && (got == null || got === '')) return false
      if (op === '=' && String(got || '').toLowerCase() !== val.toLowerCase()) return false
      if (op === '^=' && !String(got || '').startsWith(val)) return false
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
    set innerText(v) { node._text = String(v || '') },
    get textContent() {
      const childText = node.childNodes.map((c) => c.textContent || '').filter(Boolean).join(' ')
      return [node._text, childText].filter(Boolean).join(' ')
    },
    set textContent(v) { node._text = String(v || '') },
    value: opts.value || '',
    src: opts.src || '',
    currentSrc: opts.src || '',
    complete: opts.complete !== false,
    naturalWidth: opts.naturalWidth != null ? opts.naturalWidth : 1024,
    naturalHeight: opts.naturalHeight != null ? opts.naturalHeight : 1024,
    loading: opts.loading || '',
    disabled: Boolean(opts.disabled),
    readOnly: Boolean(opts.readOnly),
    offsetParent: opts.hidden ? null : {},
    clientWidth: opts.hidden ? 0 : (opts.width || 120),
    clientHeight: opts.hidden ? 0 : (opts.height || 36),
    isConnected: true,
    attrs,
    classList: {
      contains(c) { return String(node.className || '').split(/\s+/).includes(c) },
    },
    getAttribute(k) {
      if (k === 'id') return node.id || null
      if (k === 'class') return node.className || null
      return Object.prototype.hasOwnProperty.call(node.attrs, k) ? node.attrs[k] : null
    },
    setAttribute(k, v) {
      if (k === 'id') node.id = String(v)
      else if (k === 'class') node.className = String(v)
      else node.attrs[k] = String(v)
    },
    hasAttribute(k) { return node.getAttribute(k) !== null },
    getAttributeNames() { return Object.keys(node.attrs) },
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
    contains(other) {
      let cur = other
      while (cur) {
        if (cur === node) return true
        cur = cur.parentElement
      }
      return false
    },
    matches(sel) { return matchAny(node, sel) },
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
    getBoundingClientRect() {
      if (opts.hidden) return { width: 0, height: 0, left: 0, top: 0 }
      return { width: node.clientWidth, height: node.clientHeight, left: 100, top: 100 }
    },
    scrollIntoView() {},
    focus() {},
    click() { if (opts.onClick) opts.onClick(node) },
    dispatchEvent() {},
  }
  return node
}

function makeEnv(initialHref) {
  const body = el('body')
  const doc = {
    body,
    documentElement: body,
    readyState: 'complete',
    childNodes: [body],
    get images() { return queryAll(body, 'img') },
    addEventListener() {},
    querySelectorAll(sel) { return queryAll(body, sel) },
    querySelector(sel) { return queryAll(body, sel)[0] || null },
    getElementById(id) { return queryAll(body, '[id="' + id + '"]')[0] || null },
  }
  body.parentElement = doc
  const assigned = []
  const location = {
    hostname: 'chat.qwen.ai',
    href: initialHref || 'https://chat.qwen.ai/',
    reloads: 0,
    assign(u) { assigned.push(u); this.href = u },
    reload() { this.reloads++ },
  }
  class XMLHttpRequest {
    open() {}
    send() {}
    addEventListener() {}
  }
  const clicked = []
  const ctx = vm.createContext({
    console,
    URL,
    Set,
    WeakSet,
    Map,
    Promise,
    setTimeout,
    clearTimeout,
    XMLHttpRequest,
    document: doc,
    location,
    NodeFilter: { SHOW_TEXT: 4 },
    getComputedStyle() { return { display: 'block', visibility: 'visible', opacity: '1' } },
    MutationObserver: class { observe() {} },
    fetch() { return Promise.resolve({ headers: { get() { return '' } }, clone() { return this }, text() { return Promise.resolve('') }, blob() { return Promise.resolve({ size: 0 }) } }) },
    tulaiClick(target) {
      clicked.push(target)
      if (target && typeof target.click === 'function') target.click()
      return Promise.resolve(true)
    },
  })
  ctx.window = ctx
  ctx.globalThis = ctx
  vm.runInContext(fs.readFileSync(path.join(__dirname, '..', '..', '..', 'extension', 'refuse.js'), 'utf8'), ctx)
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'hook.js'), 'utf8'), ctx)
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'composer.js'), 'utf8'), ctx)
  return { ctx, doc, body, location, assigned, clicked }
}

async function run() {
  // 1) hook.js: 新对话检查、URL 解析、不串跳国内站、排除推荐图与用户原图、生成态/完成态/报错态
  {
    const { ctx, body, location, assigned } = makeEnv('https://chat.qwen.ai/')
    const container = el('div', { className: 'message-input-container' })
    const ta = el('textarea', { className: 'message-input-textarea' })
    container.appendChild(ta)
    body.appendChild(container)

    // 落地页推荐灵感画廊不应干扰生成结果
    const rec = el('div', { className: 'chat-recommend-image-list' })
    rec.appendChild(el('img', { src: 'https://img.alicdn.com/imgextra/recommend-1.png' }))
    body.appendChild(rec)

    assert.strictEqual(ctx.__tulaiConversationKey('https://chat.qwen.ai/c/550e8400-e29b-41d4-a716-446655440000'), '550e8400-e29b-41d4-a716-446655440000')
    assert.strictEqual(ctx.__tulaiConversationKey('https://chat.qwen.ai/c/new-chat'), '')
    assert.strictEqual(ctx.__tulaiChatReady().ready, true)
    assert.strictEqual(ctx.__tulaiFreshChat(), 'ready')

    // 切到旧对话后 __tulaiFreshChat 应回到 https://chat.qwen.ai/（绝不跳国内 qianwen.com）
    location.href = 'https://chat.qwen.ai/c/old-uuid'
    assert.strictEqual(ctx.__tulaiChatReady().ready, false)
    assert.strictEqual(ctx.__tulaiFreshChat(), 'url')
    assert.strictEqual(assigned[0], 'https://chat.qwen.ai/')

    // 发送一轮新任务
    ctx.__tulaiMarkTask()
    location.href = 'https://chat.qwen.ai/c/new-uuid-1'
    const userMsg = el('div', { id: 'chat-user-message-u1', className: 'chat-user-message', text: 'draw a cute fox' })
    const userFiles = el('div', { className: 'chat-user-message-files' })
    userFiles.appendChild(el('img', { src: 'https://cdn.qwenlm.ai/upload/source-ref.png' }))
    userMsg.appendChild(userFiles)
    body.appendChild(userMsg)

    assert.strictEqual(ctx.tulaiSendAccepted('https://chat.qwen.ai/', 'draw a cute fox', 0), true)

    // 生成中：stop-button 与 status card 可见
    const stopBtn = el('button', { className: 'stop-button', attrs: { 'aria-label': 'Stop' } })
    container.appendChild(stopBtn)
    const resp = el('div', { id: 'chat-response-message-r1', className: 'chat-response-message' })
    const statusCard = el('div', { className: 'qwen-chat-status-card', text: 'Generating image' })
    resp.appendChild(statusCard)
    body.appendChild(resp)

    let shot = ctx.__tulaiLiveImages()
    assert.strictEqual(shot.busy, true)
    assert.strictEqual(shot.done, false)
    assert.deepStrictEqual(Array.from(shot.images), [], '推荐画廊和用户上传图不得进入结果列表')

    // 出图完成：移除 stop-button 与状态卡，挂载 common-image-grid-image 和 footer 图标
    container.removeChild(stopBtn)
    resp.removeChild(statusCard)
    const media = el('div', { className: 'chat-response-media-render' })
    const grid = el('div', { className: 'common-image-grid' })
    const genImg = el('img', {
      className: 'common-image-grid-image',
      src: 'https://cdn.qwenlm.ai/generated/fox-1.png',
      loading: 'lazy',
      naturalWidth: 1664,
      naturalHeight: 928,
    })
    grid.appendChild(genImg)
    media.appendChild(grid)
    resp.appendChild(media)
    const footer = el('div', { className: 'response-message-footer' })
    footer.appendChild(el('img', { className: 'qwen-chat-package-comp-new-action-control-icon-img', src: 'https://cdn.qwenlm.ai/ui/copy.png', naturalWidth: 200, naturalHeight: 200 }))
    resp.appendChild(footer)

    shot = ctx.__tulaiLiveImages()
    assert.strictEqual(shot.busy, false)
    assert.strictEqual(shot.done, true)
    assert.strictEqual(shot.reason, 'images-ready')
    assert.strictEqual(genImg.loading, 'eager')
    assert.deepStrictEqual(Array.from(shot.images, (i) => i.url), ['https://cdn.qwenlm.ai/generated/fox-1.png'])

    // 绑定对话后若跳离当前对话，报告 off-chat
    ctx.__tulaiQwenURL = 'new-uuid-1'
    location.href = 'https://chat.qwen.ai/'
    assert.strictEqual(ctx.__tulaiLiveImages().reason, 'off-chat')
  }

  // 2) composer.js: + 菜单上传（即使同级存在 #filesUpload 也必须点击菜单里的「上传附件」）、清理附件、英文额度/限流/审核识别
  {
    const { ctx, body, clicked } = makeEnv('https://chat.qwen.ai/')
    const container = el('div', { className: 'message-input-container' })
    const fileRow = el('div', { className: 'message-input-column-file' })
    const visionCard = el('div', { className: 'vision-item-container' })
    const closeIcon = el('span', {
      className: 'icon-close',
      onClick() { fileRow.removeChild(visionCard) },
    })
    visionCard.appendChild(closeIcon)
    fileRow.appendChild(visionCard)
    container.appendChild(fileRow)

    const ta = el('textarea', { className: 'message-input-textarea' })
    container.appendChild(ta)

    let uploadItemClicked = false
    const modeSelect = el('div', { className: 'mode-select' })
    modeSelect.appendChild(el('input', { id: 'filesUpload', attrs: { type: 'file', multiple: 'true' }, hidden: true }))
    const plusBtn = el('div', {
      className: 'mode-select-open',
      attrs: { role: 'button', 'aria-label': 'Select Mode' },
      onClick() {
        const menu = el('div', { className: 'qwen-chat-v2-dropdown-menu-popup', width: 240, height: 220 })
        menu.appendChild(el('div', {
          className: 'qwen-chat-v2-dropdown-menu-item',
          text: 'Create Image',
        }))
        menu.appendChild(el('div', {
          className: 'qwen-chat-v2-dropdown-menu-item',
          text: 'Upload attachment Files, images, videos, audio',
          onClick() {
            uploadItemClicked = true
            ctx.__tulaiChooserOpened = true
          },
        }))
        body.appendChild(menu)
      },
    })
    modeSelect.appendChild(plusBtn)
    container.appendChild(modeSelect)
    container.appendChild(el('button', { className: 'send-button', attrs: { 'aria-label': 'Send' } }))
    body.appendChild(container)

    // 先验证附件清理
    assert.strictEqual(await ctx.tulaiCleanComposer(), true)
    assert.strictEqual(fileRow.childNodes.length, 0, '千问国际附件卡片应被 .icon-close 清理')

    // 验证上传入口走 + -> Upload attachment 菜单
    const openRes = JSON.parse(await ctx.tulaiOpenUpload())
    assert.strictEqual(openRes.opened, true)
    assert.strictEqual(uploadItemClicked, true, '必须点击弹出菜单里的 Upload attachment，不可把 .mode-select-open 误判为 directFileButton')
    assert.strictEqual(clicked[1], plusBtn)

    // 验证当 .chat-prompt-recommend-detail-container 遮挡 .mode-select-open（tulaiClick 返回 false）时仍能打开上传菜单并通过可信点击触发 Upload attachment
    uploadItemClicked = false
    const origClick = ctx.tulaiClick
    ctx.tulaiClick = (target) => {
      if (target === plusBtn) return Promise.resolve(false)
      return origClick(target)
    }
    const openCoveredRes = JSON.parse(await ctx.tulaiOpenUpload())
    assert.strictEqual(openCoveredRes.opened, true, '推荐浮层遮挡 .mode-select-open 时应回退派发打开下拉菜单')
    assert.strictEqual(uploadItemClicked, true, '回退打开菜单后仍须点击 Upload attachment 触发文件选择器')
    ctx.tulaiClick = origClick

    // 验证英文额度/限流/审核提示识别
    const toast = el('div', { attrs: { role: 'alert' }, text: 'Not Enough Credits' })
    body.appendChild(toast)
    assert.strictEqual(ctx.tulaiAccountFault().quota, true)
    body.removeChild(toast)

    const rateToast = el('div', { attrs: { role: 'alert' }, text: 'Your operation is too frequent, please try again later.' })
    body.appendChild(rateToast)
    assert.strictEqual(ctx.tulaiAccountFault().rateLimit, true)
    body.removeChild(rateToast)

    ctx.__tulaiMarkTask()
    const modResp = el('div', { id: 'chat-response-message-mod', className: 'chat-response-message', text: 'Sorry, this request violates our safety guidelines.' })
    body.appendChild(modResp)
    assert.ok(ctx.tulaiAccountFault().moderation.length > 0, '英文审核拒绝应被 TULAIRefuse 识别')
  }

  console.log('qwen_intl_dom_test ok')
}

run().catch((err) => {
  console.error(err)
  process.exit(1)
})
