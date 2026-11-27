// 即梦（jimeng.jianying.com）页面脚本集成单测：覆盖 composer.js、hook.js 以及 jimeng.go 中的参数选择脚本（图片生成、Seedream 4.7、3:4 · 2K、1..6张）。
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
    if (rest.startsWith(':not(')) {
      const end = rest.indexOf(')')
      if (end < 0) return false
      const inner = rest.slice(5, end)
      rest = rest.slice(end + 1)
      if (matchSimple(node, inner)) return false
    } else if (rest.startsWith('#')) {
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
      if (op === 'has' && got == null) return false
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
      const childText = node.childNodes.map((c) => c.innerText || '').filter(Boolean).join(' ')
      return [node._text, childText].filter(Boolean).join(' ')
    },
    set innerText(v) { node._text = String(v || '') },
    get textContent() {
      const childText = node.childNodes.map((c) => c.textContent || '').filter(Boolean).join(' ')
      return [node._text, childText].filter(Boolean).join(' ')
    },
    set textContent(v) { node._text = String(v || '') },
    value: opts.value || '',
    checked: Boolean(opts.checked),
    src: opts.src || '',
    currentSrc: opts.src || '',
    complete: opts.complete !== false,
    naturalWidth: opts.naturalWidth != null ? opts.naturalWidth : 1024,
    naturalHeight: opts.naturalHeight != null ? opts.naturalHeight : 1365,
    disabled: Boolean(opts.disabled),
    readOnly: Boolean(opts.readOnly),
    hidden: Boolean(opts.hidden),
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
      if (k === 'value' && node.value !== '') return String(node.value)
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
      if (node.hidden) return { width: 0, height: 0, left: 0, top: 0 }
      return { width: node.clientWidth, height: node.clientHeight, left: 100, top: 100 }
    },
    scrollIntoView() {},
    focus() {},
    click() { if (opts.onClick) opts.onClick(node) },
    dispatchEvent() {},
  }
  return node
}

function makeEnv(initialHref, fetchHandler) {
  const body = el('body')
  const doc = {
    body,
    documentElement: body,
    readyState: 'complete',
    childNodes: [body],
    get images() { return queryAll(body, 'img') },
    querySelectorAll(sel) { return queryAll(body, sel) },
    querySelector(sel) { return queryAll(body, sel)[0] || null },
    contains(n) { return body.contains(n) },
    createElement(tag) { return el(tag) },
    createTreeWalker(root) {
      const texts = []
      const walk = (n) => {
        if (n && n._text) texts.push({ nodeType: 3, nodeValue: n._text, parentElement: n })
        for (const c of (n && n.childNodes) || []) {
          if (c.nodeType === 3) texts.push(c)
          else if (c.nodeType === 1) walk(c)
        }
      }
      walk(root)
      let i = 0
      return { nextNode() { return i < texts.length ? texts[i++] : null } }
    },
    elementFromPoint() { return null },
    _listeners: {},
    dispatchEvent(ev) {
      const list = (doc._listeners && ev && doc._listeners[ev.type]) || []
      for (const fn of list) fn(ev)
    },
    addEventListener(type, fn) {
      if (!doc._listeners[type]) doc._listeners[type] = []
      doc._listeners[type].push(fn)
    },
  }
  const loc = new URL(initialHref)
  const location = {
    get href() { return loc.href },
    set href(v) { const u = new URL(v, loc.href); loc.href = u.href },
    get hostname() { return loc.hostname },
    get pathname() { return loc.pathname },
    get search() { return loc.search },
    get origin() { return loc.origin },
  }
  function MockXHR() {}
  MockXHR.prototype.open = function () {}
  MockXHR.prototype.send = function () {}
  MockXHR.prototype.addEventListener = function () {}

  const sandbox = {
    document: doc,
    location,
    URL,
    URLSearchParams,
    Date,
    Math,
    Promise,
    Set,
    WeakSet,
    Map,
    Array,
    Object,
    String,
    Number,
    Boolean,
    RegExp,
    JSON,
    Error,
    setTimeout,
    clearTimeout,
    console,
    NodeFilter: { SHOW_TEXT: 4 },
    MutationObserver: class { observe() {} disconnect() {} },
    MouseEvent: function (t, o) { return { type: t, ...o } },
    KeyboardEvent: function (t, o) { return { type: t, ...o } },
    Event: function (t, o) { return { type: t, ...o } },
    InputEvent: function (t, o) { return { type: t, ...o } },
    XMLHttpRequest: MockXHR,
    fetch: fetchHandler || (async () => ({
      ok: true,
      headers: { get: () => 'application/json' },
      clone() { return this },
      text: async () => '{}',
      json: async () => ({}),
    })),
    getComputedStyle(n) {
      if (n.hidden) return { display: 'none', visibility: 'hidden', opacity: '0', pointerEvents: 'none' }
      return { display: 'block', visibility: 'visible', opacity: '1', pointerEvents: 'auto' }
    },
    tulaiClick(target) {
      if (target && typeof target.click === 'function') target.click()
      return Promise.resolve(true)
    },
    requestAnimationFrame(fn) { return setTimeout(fn, 0) },
  }
  sandbox.window = sandbox
  sandbox.self = sandbox
  sandbox.globalThis = sandbox
  vm.createContext(sandbox)

  const refuseJS = fs.readFileSync(path.join(__dirname, '..', '..', '..', 'extension', 'refuse.js'), 'utf8')
  const hookJS = fs.readFileSync(path.join(__dirname, 'hook.js'), 'utf8')
  const composerJS = fs.readFileSync(path.join(__dirname, 'composer.js'), 'utf8')
  vm.runInContext(refuseJS, sandbox)
  vm.runInContext(hookJS, sandbox)
  vm.runInContext(composerJS, sandbox)
  return { sandbox, doc, body, location }
}

function extractGoBacktickConst(filePath, constName) {
  const src = fs.readFileSync(filePath, 'utf8')
  const marker = 'const ' + constName + ' = `'
  const start = src.indexOf(marker)
  assert.ok(start >= 0, 'missing const ' + constName)
  const from = start + marker.length
  const end = src.indexOf('`', from)
  assert.ok(end > from, 'unterminated const ' + constName)
  return src.slice(from, end)
}

async function testComposerIgnoresSizerAndHandlesSixRefs() {
  const { sandbox, body, location } = makeEnv('https://jimeng.jianying.com/ai-tool/generate/?type=image')
  const gen = el('div', { attrs: { 'data-testid': 'dreamina.generator' }, width: 900, height: 140 })
  body.appendChild(gen)

  // Hidden sizer clone that must be ignored
  const sizerWrap = el('div', { className: 'prompt-editor-sizer-abc', attrs: { 'aria-hidden': 'true', 'data-generator-preview-ignore': 'true' } })
  const sizerBox = el('div', { attrs: { role: 'textbox', 'aria-label': '提示词', contenteditable: 'true' }, width: 600, height: 40 })
  sizerWrap.appendChild(sizerBox)
  gen.appendChild(sizerWrap)

  // Real ProseMirror editor
  const realBox = el('div', { attrs: { role: 'textbox', 'aria-label': '提示词', contenteditable: 'true' }, width: 600, height: 48 })
  gen.appendChild(realBox)

  // Direct reference upload button
  let uploadClicks = 0
  const refsBar = el('div', { attrs: { 'data-content-generator-references': 'true' }, width: 200, height: 64 })
  const uploadBtn = el('div', {
    attrs: { role: 'button', 'aria-label': '上传参考图' },
    width: 56,
    height: 56,
    onClick: () => {
      uploadClicks++
      sandbox.__tulaiChooserOpened = true
    },
  })
  refsBar.appendChild(uploadBtn)
  gen.appendChild(refsBar)

  // Collapsed hidden submit + visible submit
  const hiddenSubmit = el('button', { attrs: { 'data-testid': 'dreamina.generator.submit', 'data-generator-submit-available': 'true' }, hidden: true })
  let submitClicks = 0
  const visibleSubmit = el('button', {
    attrs: { 'data-testid': 'dreamina.generator.submit', 'data-generator-submit-available': 'true' },
    width: 36,
    height: 36,
    onClick: () => {
      submitClicks++
      location.href = 'https://jimeng.jianying.com/ai-tool/generate/?type=image&workspace=99887766'
    },
  })
  gen.appendChild(hiddenSubmit)
  gen.appendChild(visibleSubmit)

  // 1. Composer finds realBox (not hidden sizer)
  assert.strictEqual(sandbox.tulaiMarkComposer(), true, 'tulaiMarkComposer must locate realBox')
  assert.strictEqual(sandbox.__tulaiComposer, realBox, 'tulaiMarkComposer must skip hidden prompt-editor-sizer')

  // 2. Upload button click
  const openRes = JSON.parse(await sandbox.tulaiOpenUpload())
  assert.strictEqual(openRes.opened, true, 'tulaiOpenUpload must find Jimeng reference upload button')
  assert.strictEqual(uploadClicks, 1)

  // 3. Wait for 6 stacked reference items
  for (let i = 0; i < 6; i++) {
    const refItem = el('div', { className: 'reference-item-card-' + i, width: 56, height: 56 })
    refItem.appendChild(el('img', { src: 'blob:https://jimeng.jianying.com/' + i, naturalWidth: 512, naturalHeight: 512 }))
    refsBar.appendChild(refItem)
  }
  const uploaded = JSON.parse(await sandbox.tulaiWaitForUpload(6))
  assert.strictEqual(uploaded.ready, true, 'tulaiWaitForUpload must confirm 6 stacked reference items')

  // 4. Send picks visible submit button & accepts via workspace change
  realBox.innerText = '保持构图重绘'
  const sendRes = JSON.parse(await sandbox.tulaiSendPrompt())
  assert.strictEqual(sendRes.sent, true, 'tulaiSendPrompt must succeed: ' + JSON.stringify(sendRes))
  assert.strictEqual(submitClicks, 1)
}

async function testHookInterceptsJimengAPIForSixImagesAndFaults() {
  const urls = Array.from({ length: 6 }, (_, i) => `https://p3-dreamina-sign.byteimg.com/tos-cn-i-tb4s082cfz/out_${i + 1}.jpeg?x-signature=ok`)
  const mkResp = (body) => ({
    ok: true,
    headers: { get: () => 'application/json' },
    clone() { return this },
    text: async () => JSON.stringify(body),
    json: async () => body,
  })
  let pollStep = 0
  const fakeFetch = async (input) => {
    const u = typeof input === 'string' ? input : input.url
    if (u.includes('/mweb/v1/aigc_draft/generate')) {
      return mkResp({
        ret: '0',
        data: { aigc_data: { history_record_id: 'hist-666' } },
      })
    }
    if (u.includes('/mweb/v1/get_history_by_ids')) {
      pollStep++
      const body = pollStep === 1
        ? { ret: '0', data: { 'hist-666': { status: 20, item_list: [] } } }
        : {
            ret: '0',
            data: {
              'hist-666': {
                status: 50,
                item_list: urls.map((url) => ({ image: { large_images: [{ image_url: url }] } })),
              },
            },
          }
      return mkResp(body)
    }
    return mkResp({ ret: '0' })
  }

  const { sandbox, body } = makeEnv('https://jimeng.jianying.com/ai-tool/generate/?type=image', fakeFetch)
  const gen = el('div', { attrs: { 'data-testid': 'dreamina.generator' }, width: 900, height: 140 })
  gen.appendChild(el('div', { attrs: { role: 'textbox', 'aria-label': '提示词', contenteditable: 'true' }, width: 600, height: 48 }))
  body.appendChild(gen)

  assert.strictEqual(sandbox.__tulaiChatReady().ready, true)
  sandbox.__tulaiMarkTask()

  await sandbox.fetch('https://jimeng.jianying.com/mweb/v1/aigc_draft/generate', { method: 'POST' })
  await new Promise((r) => setTimeout(r, 10))
  assert.strictEqual(sandbox.__tulaiJimengTurn.submitted, true)
  assert.deepStrictEqual(Array.from(sandbox.__tulaiJimengTurn.historyIds), ['hist-666'])

  // First poll: generating (status 20) -> busy=true, done=false, images=[]
  await sandbox.fetch('https://jimeng.jianying.com/mweb/v1/get_history_by_ids', { method: 'POST' })
  await new Promise((r) => setTimeout(r, 10))
  const live1 = sandbox.__tulaiLiveImages()
  assert.strictEqual(live1.busy, true, 'status 20 should report busy=true')
  assert.strictEqual(live1.done, false, 'status 20 should report done=false')
  assert.deepStrictEqual(Array.from(live1.images), [])

  // Second poll: finished (status 50) -> done=true, returns all 6 ordered URLs with empty rawUrl (not blindly copied from preview url)
  await sandbox.fetch('https://jimeng.jianying.com/mweb/v1/get_history_by_ids', { method: 'POST' })
  await new Promise((r) => setTimeout(r, 10))
  const live2 = sandbox.__tulaiLiveImages()
  assert.strictEqual(live2.busy, false, 'status 50 should report busy=false')
  assert.strictEqual(live2.done, true, 'status 50 should report done=true')
  assert.deepStrictEqual(Array.from(live2.images, (x) => x.url), urls)
  assert.deepStrictEqual(Array.from(live2.images, (x) => x.rawUrl), ['', '', '', '', '', ''])

  // Clean URL resolution via need_aigc_top_left_watermark=false & /lv/v1/editor/image/batch_get_url
  let sawCleanHistoryReq = false
  let sawBatchGetUrlReq = false
  const cleanEnv = makeEnv('https://jimeng.jianying.com/ai-tool/generate/?type=image', async (input, init) => {
    const u = typeof input === 'string' ? input : input.url
    const reqBody = init && init.body ? JSON.parse(init.body) : {}
    if (u.includes('/mweb/v1/aigc_draft/generate')) {
      return mkResp({ ret: '0', data: { aigc_data: { history_record_id: 'hist-wm' } } })
    }
    if (u.includes('/mweb/v1/get_history_by_ids')) {
      if (reqBody && reqBody.image_info && reqBody.image_info.need_aigc_top_left_watermark === false) {
        sawCleanHistoryReq = true
        return mkResp({
          ret: '0',
          data: {
            'hist-wm': {
              status: 50,
              item_list: [
                {
                  common_attr: { id: 'item-1', cover_url_map: { origin: 'https://p26-dreamina-sign.byteimg.com/tos-cn-i-tb4s082cfz/clean_1.png?x-signature=clean1' } },
                  image: { large_images: [{ image_uri: 'tos-cn-i-tb4s082cfz/uri_1', image_url: 'https://p26-dreamina-sign.byteimg.com/tos-cn-i-tb4s082cfz/clean_1.png?x-signature=clean1' }] },
                },
                {
                  common_attr: { id: 'item-2' },
                  image: { large_images: [{ image_uri: 'tos-cn-i-tb4s082cfz/uri_2', image_url: 'https://p26-dreamina-sign.byteimg.com/tos-cn-i-tb4s082cfz/out_2~tplv-tb4s082cfz-aigc_watermark.webp?x-signature=wm2' }] },
                },
              ],
            },
          },
        })
      }
      return mkResp({
        ret: '0',
        data: {
          'hist-wm': {
            status: 50,
            item_list: [
              {
                common_attr: { id: 'item-1' },
                image: { large_images: [{ image_uri: 'tos-cn-i-tb4s082cfz/uri_1', image_url: 'https://p26-dreamina-sign.byteimg.com/tos-cn-i-tb4s082cfz/preview_1~tplv-tb4s082cfz-aigc_resize:1080:1080.webp?x-signature=p1' }] },
              },
              {
                common_attr: { id: 'item-2' },
                image: { large_images: [{ image_uri: 'tos-cn-i-tb4s082cfz/uri_2', image_url: 'https://p26-dreamina-sign.byteimg.com/tos-cn-i-tb4s082cfz/preview_2~tplv-tb4s082cfz-aigc_watermark.webp?x-signature=p2' }] },
              },
            ],
          },
        },
      })
    }
    if (u.includes('/lv/v1/editor/image/batch_get_url')) {
      sawBatchGetUrlReq = true
      assert.strictEqual(reqBody.uri_info[0].biz, 'editor')
      return mkResp({
        BaseResp: { StatusCode: 0 },
        data: [
          { uri: 'tos-cn-i-tb4s082cfz/uri_2', full_url: 'https://p26-dreamina-sign.byteimg.com/tos-cn-i-tb4s082cfz/editor_full_2.png?x-signature=ed2' },
        ],
      })
    }
    return mkResp({ ret: '0' })
  })
  cleanEnv.sandbox.__tulaiMarkTask()
  await cleanEnv.sandbox.fetch('https://jimeng.jianying.com/mweb/v1/aigc_draft/generate', { method: 'POST' })
  await new Promise((r) => setTimeout(r, 10))
  await cleanEnv.sandbox.fetch('https://jimeng.jianying.com/mweb/v1/get_history_by_ids?aid=513695&web_version=7.5.0', { method: 'POST' })
  await new Promise((r) => setTimeout(r, 10))
  const resolved = JSON.parse(await cleanEnv.sandbox.__tulaiJimengResolveClean())
  assert.strictEqual(sawCleanHistoryReq, true, 'must query get_history_by_ids with need_aigc_top_left_watermark=false')
  assert.strictEqual(sawBatchGetUrlReq, true, 'must query /lv/v1/editor/image/batch_get_url with image_uri')
  assert.strictEqual(resolved.byIndex[0], 'https://p26-dreamina-sign.byteimg.com/tos-cn-i-tb4s082cfz/clean_1.png?x-signature=clean1')
  assert.strictEqual(resolved.byIndex[1], 'https://p26-dreamina-sign.byteimg.com/tos-cn-i-tb4s082cfz/editor_full_2.png?x-signature=ed2')

  // Quota fault interception
  const quotaEnv = makeEnv('https://jimeng.jianying.com/ai-tool/generate/?type=image', async () =>
    mkResp({ ret: '1006', errmsg: '积分不足，请充值' }),
  )
  quotaEnv.sandbox.__tulaiMarkTask()
  await quotaEnv.sandbox.fetch('https://jimeng.jianying.com/mweb/v1/aigc_draft/generate', { method: 'POST' })
  await new Promise((r) => setTimeout(r, 10))
  const fault = quotaEnv.sandbox.tulaiAccountFault()
  assert.strictEqual(fault.quota, true, 'expected quota=true fault from Jimeng API response')

  // Moderation failure interception (status 30)
  let modStep = 0
  const modEnv = makeEnv('https://jimeng.jianying.com/ai-tool/generate/?type=image', async (input) => {
    const u = typeof input === 'string' ? input : input.url
    if (u.includes('/mweb/v1/aigc_draft/generate')) {
      return mkResp({ ret: '0', data: { aigc_data: { history_record_id: 'hist-mod' } } })
    }
    modStep++
    return mkResp({
      ret: '0',
      data: { 'hist-mod': { status: 30, fail_starling_message: '内容审核未通过', item_list: [] } },
    })
  })
  modEnv.sandbox.__tulaiMarkTask()
  await modEnv.sandbox.fetch('https://jimeng.jianying.com/mweb/v1/aigc_draft/generate', { method: 'POST' })
  await new Promise((r) => setTimeout(r, 10))
  await modEnv.sandbox.fetch('https://jimeng.jianying.com/mweb/v1/get_history_by_ids', { method: 'POST' })
  await new Promise((r) => setTimeout(r, 10))
  assert.strictEqual(modStep, 1)
  const modFault = modEnv.sandbox.tulaiAccountFault()
  assert.ok(modFault.moderation.includes('审核'), 'expected moderation fault: ' + JSON.stringify(modFault))
}

async function testEnsureConfigSelectsToolbarParamsAndSkipsWhenMatched() {
  const goPath = path.join(__dirname, '..', 'jimeng.go')
  const ensureSrc = extractGoBacktickConst(goPath, 'jimengEnsureConfigJS')
  const activeSrc = extractGoBacktickConst(goPath, 'jimengConfigActiveJS')

  const { sandbox, body } = makeEnv('https://jimeng.jianying.com/ai-tool/generate/?type=image')
  const gen = el('div', { attrs: { 'data-testid': 'dreamina.generator' }, width: 900, height: 140 })
  body.appendChild(gen)

  const state = {
    mode: '图片生成',
    model: 'Seedream 5.5',
    ratio: '智能比例',
    res: '1.5K',
    count: 4,
    clicks: 0,
  }

  let activePopup = null
  const closePopup = () => {
    if (activePopup && activePopup.parentElement) activePopup.parentElement.removeChild(activePopup)
    activePopup = null
  }
  sandbox.document.addEventListener('keydown', (ev) => {
    if (ev && ev.key === 'Escape') closePopup()
  })

  const modeBox = el('div', {
    attrs: { role: 'combobox', 'aria-label': '创作类型' },
    text: state.mode,
    onClick: () => {
      state.clicks++
      closePopup()
      const pop = el('div', { className: 'lv-select-popup', attrs: { role: 'listbox' } })
      for (const m of ['图片生成', '视频生成']) {
        const opt = el('li', {
          attrs: { role: 'option' },
          onClick: () => { state.mode = m; modeBox.innerText = m; closePopup() },
        })
        opt.appendChild(el('span', { className: 'option-label-x', text: m }))
        pop.appendChild(opt)
      }
      activePopup = pop
      body.appendChild(pop)
    },
  })
  const modelBox = el('div', {
    attrs: { role: 'combobox', 'aria-label': '模型' },
    text: state.model,
    onClick: () => {
      state.clicks++
      closePopup()
      const pop = el('div', { className: 'lv-select-popup', attrs: { role: 'listbox' } })
      for (const m of ['Seedream 5.5', 'Seedream 4.7', 'Seedream 4.0']) {
        const opt = el('li', {
          attrs: { role: 'option' },
          onClick: () => { state.model = m; modelBox.innerText = m; closePopup() },
        })
        opt.appendChild(el('span', { className: 'option-label-x', text: m }))
        pop.appendChild(opt)
      }
      activePopup = pop
      body.appendChild(pop)
    },
  })
  const ratioBtn = el('button', {
    text: `${state.ratio} ${state.res}`,
    onClick: () => {
      state.clicks++
      closePopup()
      const pop = el('div', { className: 'lv-popover-inner-content', text: '画面比例 分辨率' })
      const smartLbl = el('label', { text: '智能', onClick: () => { state.ratio = '智能比例'; ratioBtn.innerText = `${state.ratio} · ${state.res}` } })
      smartLbl.appendChild(el('input', { attrs: { type: 'radio' }, value: '智能', checked: state.ratio === '智能比例' }))
      pop.appendChild(smartLbl)
      for (const r of ['21:9', '1:1', '3:4', '4:3', '16:9', '9:16']) {
        const lbl = el('label', { text: r, onClick: () => { state.ratio = r; ratioBtn.innerText = `${state.ratio} · ${state.res}` } })
        lbl.appendChild(el('input', { attrs: { type: 'radio' }, value: r, checked: state.ratio === r }))
        pop.appendChild(lbl)
      }
      for (const res of ['1.5k', '2k', '4k']) {
        const lbl = el('label', { text: res.toUpperCase(), onClick: () => { state.res = res.toUpperCase(); ratioBtn.innerText = `${state.ratio} · ${state.res}` } })
        lbl.appendChild(el('input', { attrs: { type: 'radio' }, value: res, checked: state.res.toLowerCase() === res }))
        pop.appendChild(lbl)
      }
      activePopup = pop
      body.appendChild(pop)
    },
  })
  const countBtn = el('button', {
    attrs: { 'aria-label': `选择生成数量：${state.count}张` },
    text: `${state.count}张`,
    onClick: () => {
      state.clicks++
      closePopup()
      const pop = el('div', { className: 'lv-popover-inner-content', text: '选择生成数量' })
      for (const n of [1, 2, 3, 4, 5, 6]) {
        const lbl = el('label', {
          text: String(n),
          onClick: () => {
            state.count = n
            countBtn.setAttribute('aria-label', `选择生成数量：${n}张`)
            countBtn.innerText = `${n}张`
            closePopup()
          },
        })
        lbl.appendChild(el('input', { attrs: { type: 'radio' }, value: String(n), checked: state.count === n }))
        pop.appendChild(lbl)
      }
      activePopup = pop
      body.appendChild(pop)
    },
  })
  gen.appendChild(modeBox)
  gen.appendChild(modelBox)
  gen.appendChild(ratioBtn)
  gen.appendChild(countBtn)

  const runEnsure = vm.runInContext(ensureSrc, sandbox)
  const runActive = vm.runInContext(activeSrc, sandbox)

  const targetCfg = { mode: '图片生成', model: 'Seedream 4.7', ratio: '3:4', resolution: '2k', count: 6, timeoutMs: 3000 }
  assert.strictEqual(runActive(targetCfg), false)

  const res1 = JSON.parse(await runEnsure(targetCfg))
  assert.strictEqual(res1.ok, true, 'ensureConfig failed: ' + JSON.stringify(res1))
  assert.strictEqual(state.mode, '图片生成')
  assert.strictEqual(state.model, 'Seedream 4.7')
  assert.strictEqual(state.ratio, '3:4')
  assert.strictEqual(state.res, '2K')
  assert.strictEqual(state.count, 6)
  assert.strictEqual(runActive(targetCfg), true)

  // Already matched -> zero clicks!
  const clicksBefore = state.clicks
  const res2 = JSON.parse(await runEnsure(targetCfg))
  assert.strictEqual(res2.ok, true)
  assert.strictEqual(state.clicks, clicksBefore, 'already matched config must skip toolbar clicks')

  // Switching count to 3张 for a 3-image batch
  const batch3Cfg = { mode: '图片生成', model: 'Seedream 4.7', ratio: '3:4', resolution: '2k', count: 3, timeoutMs: 3000 }
  const res3 = JSON.parse(await runEnsure(batch3Cfg))
  assert.strictEqual(res3.ok, true)
  assert.strictEqual(state.count, 3)
  assert.strictEqual(runActive(batch3Cfg), true)

  // Selecting 智能比例 + 4K must switch both radio controls and report active
  const smart4kCfg = { mode: '图片生成', model: 'Seedream 4.7', ratio: '智能', resolution: '4k', count: 3, timeoutMs: 3000 }
  assert.strictEqual(runActive(smart4kCfg), false)
  const res4 = JSON.parse(await runEnsure(smart4kCfg))
  assert.strictEqual(res4.ok, true, 'ensureConfig 智能+4K failed: ' + JSON.stringify(res4))
  assert.strictEqual(state.ratio, '智能比例')
  assert.strictEqual(state.res, '4K')
  assert.strictEqual(runActive(smart4kCfg), true)

  // Live probe (jimengOutputCapsProbeJS) reads actual webpage ratios, 智能比例, resolutions and closes popover
  const capsGoPath = path.join(__dirname, '..', '..', 'cdp', 'output_caps.go')
  const probeSrc = extractGoBacktickConst(capsGoPath, 'jimengOutputCapsProbeJS')
  const capsRes = await vm.runInContext(probeSrc, sandbox)
  assert.strictEqual(capsRes.ok, true, 'jimengOutputCapsProbeJS failed: ' + JSON.stringify(capsRes))
  assert.strictEqual(capsRes.model, 'Seedream 4.7')
  assert.strictEqual(capsRes.supportsAuto, true)
  assert.strictEqual(capsRes.autoLabel, '智能比例')
  assert.deepStrictEqual(Array.from(capsRes.ratios), ['21:9', '1:1', '3:4', '4:3', '16:9', '9:16'])
  assert.deepStrictEqual(Array.from(capsRes.resolutions), ['1.5K', '2K', '4K'])
  assert.strictEqual(capsRes.currentResolution, '4K')
  assert.strictEqual(activePopup, null, 'probe must close popover after reading options')
}

;(async () => {
  await testComposerIgnoresSizerAndHandlesSixRefs()
  await testHookInterceptsJimengAPIForSixImagesAndFaults()
  await testEnsureConfigSelectsToolbarParamsAndSkipsWhenMatched()
  console.log('jimeng_dom_test.js: all checks passed')
})().catch((err) => {
  console.error(err)
  process.exit(1)
})
