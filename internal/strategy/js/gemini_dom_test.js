// Gemini（gemini.google.com）页面脚本集成单测：覆盖 hook.js（新对话、.ql-clipboard 排除、零态风格模板画廊与用户图排除、生成态/完成态、断页检测）与 composer.js（+ 菜单上传、stop 按钮防误点、附件清理、额度/限流/审核识别）。
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
  const parsed = new URL(initialHref || 'https://gemini.google.com/app')
  const location = {
    hostname: parsed.hostname,
    pathname: parsed.pathname,
    href: parsed.href,
    reloads: 0,
    assign(u) {
      assigned.push(u)
      const next = new URL(u)
      this.href = next.href
      this.hostname = next.hostname
      this.pathname = next.pathname
    },
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
  // 1) hook.js: 会话 ID、.ql-clipboard 排除、零态风格模板画廊排除、生成中与完成后采集、断页检测
  {
    const { ctx, body, location, assigned } = makeEnv('https://gemini.google.com/app')
    const inputArea = el('input-area-v2')
    const rich = el('rich-textarea')
    const editor = el('div', { className: 'ql-editor', attrs: { contenteditable: 'true', 'data-placeholder': '描述你的图片' } })
    const clipboard = el('div', { className: 'ql-clipboard', attrs: { contenteditable: 'true' }, width: 100, height: 20 })
    rich.appendChild(editor)
    rich.appendChild(clipboard)
    inputArea.appendChild(rich)
    body.appendChild(inputArea)

    // 制作图片页面上方的风格模板展示卡片不得混入结果采集
    const showcase = el('discovery-images-page')
    const zeroState = el('modular-zero-state', { className: 'zero-state-container' })
    zeroState.appendChild(el('img', { src: 'https://lh3.googleusercontent.com/gg-mc/style-retro-cartoon=s512', naturalWidth: 512, naturalHeight: 512 }))
    showcase.appendChild(zeroState)
    body.appendChild(showcase)

    assert.strictEqual(ctx.__tulaiConversationKey('https://gemini.google.com/app/9a8b7c6d5e4f'), '9a8b7c6d5e4f')
    assert.strictEqual(ctx.__tulaiConversationKey('https://gemini.google.com/u/1/app/9a8b7c6d5e4f'), '9a8b7c6d5e4f')
    assert.strictEqual(ctx.__tulaiConversationKey('https://gemini.google.com/app'), '')
    assert.strictEqual(ctx.__tulaiConversationKey('https://gemini.google.com/images'), '')
    assert.strictEqual(ctx.__tulaiConversationKey('https://gemini.google.com/app/gems'), '')
    assert.strictEqual(ctx.__tulaiChatReady().ready, true)
    assert.strictEqual(ctx.__tulaiFreshChat(), 'ready')

    // 确认 tulaiMarkComposer 选中 .ql-editor 而非 .ql-clipboard
    assert.strictEqual(ctx.tulaiMarkComposer(), true)
    assert.strictEqual(ctx.__tulaiComposer, editor, '不得误选隐藏的 .ql-clipboard 作为输入框')

    // 切到旧对话后 __tulaiFreshChat 保留 /u/1/app 路径回到 Gemini
    location.href = 'https://gemini.google.com/u/1/app/old-conv-123'
    location.pathname = '/u/1/app/old-conv-123'
    assert.strictEqual(ctx.__tulaiChatReady().ready, false)
    assert.strictEqual(ctx.__tulaiFreshChat(), 'url')
    assert.strictEqual(assigned[0], 'https://gemini.google.com/u/1/app')

    // 开始新一轮生成
    ctx.__tulaiMarkTask()
    location.href = 'https://gemini.google.com/app/new-conv-99'
    location.pathname = '/app/new-conv-99'
    const userQ = el('user-query', { attrs: { 'data-test-id': 'user-query' }, text: '一只橘猫在窗台晒太阳' })
    const userFiles = el('user-query-file-carousel')
    userFiles.appendChild(el('img', { src: 'https://lh3.googleusercontent.com/gg-dl/uploaded-ref=s1024', naturalWidth: 1024, naturalHeight: 1024 }))
    userQ.appendChild(userFiles)
    body.appendChild(userQ)

    assert.strictEqual(ctx.tulaiSendAccepted('https://gemini.google.com/app', '一只橘猫在窗台晒太阳', 0), true)

    // 生成中：stop 按钮与进度条可见
    const stopBtn = el('button', { className: 'send-button stop', attrs: { 'aria-label': '停止响应' } })
    stopBtn.appendChild(el('mat-icon', { attrs: { fonticon: 'stop' } }))
    inputArea.appendChild(stopBtn)
    // 模拟真实 Gemini DOM：<side-navigation-v2> 同时包裹 <bard-sidenav> 与主内容区 <bard-sidenav-content>
    const sideNavV2 = el('side-navigation-v2')
    const sideDrawer = el('bard-sidenav')
    sideDrawer.appendChild(el('img', { src: 'https://lh3.googleusercontent.com/gg-dl/sidebar-thumb=s512', naturalWidth: 512, naturalHeight: 512 }))
    const mainContent = el('bard-sidenav-content')
    sideNavV2.appendChild(sideDrawer)
    sideNavV2.appendChild(mainContent)
    body.appendChild(sideNavV2)

    const resp = el('model-response', { id: 'resp-1', attrs: { 'data-test-id': 'model-response' } })
    const spinner = el('mat-progress-spinner', { width: 32, height: 32 })
    resp.appendChild(spinner)
    mainContent.appendChild(resp)

    let shot = ctx.__tulaiLiveImages()
    assert.strictEqual(shot.busy, true)
    assert.strictEqual(shot.done, false)
    assert.deepStrictEqual(Array.from(shot.images), [], '模板画廊和用户参考图不得进入生成结果')

    // 出图完成：移除 stop 按钮和 spinner，挂载真实结构的 <generated-image><single-image><button><img src="blob:..."></button>、下载按钮与无关 gstatic/头像噪音图
    inputArea.removeChild(stopBtn)
    resp.removeChild(spinner)
    const genBox = el('generated-image')
    const singleBox = el('single-image')
    const previewBtn = el('button', { width: 495, height: 495 })
    const genImg = el('img', {
      src: 'blob:https://gemini.google.com/15d3fa16-f1e0-4109-870a-799f5ee05837',
      loading: 'lazy',
      naturalWidth: 1024,
      naturalHeight: 1024,
    })
    previewBtn.appendChild(genImg)
    singleBox.appendChild(previewBtn)
    genBox.appendChild(singleBox)
    genBox.appendChild(el('img', { src: 'https://www.gstatic.com/lamda/images/sparkle.png', naturalWidth: 256, naturalHeight: 256 }))
    genBox.appendChild(el('img', { src: 'https://lh3.googleusercontent.com/a/user-avatar=s256', naturalWidth: 256, naturalHeight: 256 }))
    resp.appendChild(genBox)
    const actions = el('message-actions')
    actions.appendChild(el('button', { attrs: { 'data-test-id': 'download-generated-image-button', 'aria-label': '下载完整尺寸图片' } }))
    resp.appendChild(actions)

    shot = ctx.__tulaiLiveImages()
    assert.strictEqual(shot.busy, false)
    assert.strictEqual(shot.done, true)
    assert.strictEqual(shot.reason, 'images-ready')
    assert.strictEqual(genImg.loading, 'eager')
    assert.deepStrictEqual(Array.from(shot.images, (i) => i.url), ['blob:https://gemini.google.com/15d3fa16-f1e0-4109-870a-799f5ee05837'])

    // 绑定对话后偏离页面应报 off-chat
    ctx.__tulaiQwenURL = 'new-conv-99'
    location.href = 'https://gemini.google.com/app'
    location.pathname = '/app'
    assert.strictEqual(ctx.__tulaiLiveImages().reason, 'off-chat')
  }

  // 2) composer.js: + 菜单上传、附件清理、stop 按钮不被误当作发送按钮、额度/限流/审核识别
  {
    const { ctx, body, clicked } = makeEnv('https://gemini.google.com/app')
    const inputArea = el('input-area-v2')
    const previewContainer = el('uploader-file-preview-container')
    const preview = el('uploader-file-preview')
    const removeBtn = el('button', {
      attrs: { 'aria-label': '移除文件' },
      onClick() { previewContainer.removeChild(preview) },
    })
    preview.appendChild(removeBtn)
    previewContainer.appendChild(preview)
    inputArea.appendChild(previewContainer)

    const rich = el('rich-textarea')
    rich.appendChild(el('div', { className: 'ql-editor', attrs: { contenteditable: 'true' } }))
    inputArea.appendChild(rich)

    let uploadItemClicked = false
    const menuWrap = el('simplified-input-menu')
    const plusBtn = el('button', {
      attrs: { 'aria-haspopup': 'menu', 'aria-label': '上传和工具' },
      onClick() {
        const overlay = el('div', { className: 'cdk-overlay-container' })
        const menu = el('mat-action-list', { attrs: { role: 'menu' }, width: 240, height: 220 })
        menu.appendChild(el('button', {
          attrs: { 'data-test-id': 'local-images-files-uploader-button', role: 'menuitem' },
          text: '上传文件',
          onClick() {
            uploadItemClicked = true
            ctx.__tulaiChooserOpened = true
          },
        }))
        overlay.appendChild(menu)
        body.appendChild(overlay)
      },
    })
    menuWrap.appendChild(plusBtn)
    inputArea.appendChild(menuWrap)

    // 仅有停止按钮时不得发送
    const stopBtn = el('button', { className: 'send-button stop', attrs: { 'aria-label': '停止响应' } })
    inputArea.appendChild(stopBtn)
    body.appendChild(inputArea)

    assert.strictEqual(await ctx.tulaiCleanComposer(), true)
    assert.strictEqual(previewContainer.childNodes.length, 0, 'Gemini 附件预览应被移除按钮清理')

    const openRes = JSON.parse(await ctx.tulaiOpenUpload())
    assert.strictEqual(openRes.opened, true)
    assert.strictEqual(uploadItemClicked, true, '应通过 + 菜单点击 local-images-files-uploader-button')
    assert.strictEqual(clicked.includes(plusBtn), true)

    // 2b) 新版 Gemini gem-menu：gem-menu-item 带 mat-mdc-tooltip-disabled 与包含「代码」的中文 aria-label，不得误跳过
    {
      const env2 = makeEnv('https://gemini.google.com/app')
      const area2 = el('input-area-v2')
      const rich2 = el('rich-textarea')
      rich2.appendChild(el('div', { className: 'ql-editor', attrs: { contenteditable: 'true' } }))
      area2.appendChild(rich2)
      let gemMenuClicked = false
      const wrap2 = el('simplified-input-menu')
      const plus2 = el('button', {
        attrs: { 'aria-haspopup': 'menu', 'aria-label': '上传和工具' },
        onClick() {
          const ov = el('div', { className: 'cdk-overlay-container' })
          const gm = el('gem-menu', { className: 'gem-menu', attrs: { role: 'menu' }, width: 240, height: 220 })
          const item = el('gem-menu-item', {
            className: 'mat-mdc-tooltip-trigger mat-mdc-tooltip-disabled lm-enabled',
            attrs: { role: 'menuitem', 'aria-label': '上传文件。文档、数据、代码文件', 'aria-disabled': 'false' },
            text: '上传文件',
            width: 224,
            height: 39,
            onClick() {
              gemMenuClicked = true
              env2.ctx.__tulaiChooserOpened = true
            },
          })
          item.appendChild(el('mat-icon', { attrs: { fonticon: 'attach_file' } }))
          gm.appendChild(item)
          ov.appendChild(gm)
          env2.body.appendChild(ov)
        },
      })
      wrap2.appendChild(plus2)
      area2.appendChild(wrap2)
      env2.body.appendChild(area2)

      const res2 = JSON.parse(await env2.ctx.tulaiOpenUpload())
      assert.strictEqual(res2.opened, true)
      assert.strictEqual(gemMenuClicked, true, '新版 gem-menu 上传项带 mat-mdc-tooltip-disabled 与含「代码」aria-label 时仍应点击成功')
    }

    // 2c) 首次上传弹出「Creating content from images and files」同意对话框时自动点击 Agree 并触发文件选择器
    {
      const env3 = makeEnv('https://gemini.google.com/app')
      const area3 = el('input-area-v2')
      const rich3 = el('rich-textarea')
      rich3.appendChild(el('div', { className: 'ql-editor', attrs: { contenteditable: 'true' } }))
      area3.appendChild(rich3)
      let agreed = false
      let uploadClicks = 0
      const wrap3 = el('simplified-input-menu')
      const plus3 = el('button', {
        attrs: { 'aria-haspopup': 'menu', 'aria-label': 'Upload & tools' },
        onClick() {
          const ov = el('div', { className: 'cdk-overlay-container' })
          const gm = el('gem-menu', { className: 'gem-menu', attrs: { role: 'menu' }, width: 240, height: 220 })
          const item = el('gem-menu-item', {
            className: 'mat-mdc-tooltip-disabled',
            attrs: { role: 'menuitem', 'aria-label': 'Upload files. Documents, data, code files' },
            text: 'Upload files',
            width: 224,
            height: 39,
            onClick() {
              uploadClicks++
              if (!agreed) {
                const dlg = el('mat-dialog-container', {
                  attrs: { role: 'dialog' },
                  text: 'Creating content from images and files Make sure you have the necessary rights to any images or files you upload. Prohibited Use Policy.',
                  width: 480,
                  height: 240,
                })
                dlg.appendChild(el('button', { text: 'Cancel', width: 72, height: 36 }))
                dlg.appendChild(el('button', {
                  text: 'Agree',
                  width: 72,
                  height: 36,
                  onClick() {
                    agreed = true
                    env3.body.removeChild(dlg)
                    env3.ctx.__tulaiChooserOpened = true
                  },
                }))
                env3.body.appendChild(dlg)
              } else {
                env3.ctx.__tulaiChooserOpened = true
              }
            },
          })
          item.appendChild(el('mat-icon', { attrs: { fonticon: 'attach_file' } }))
          gm.appendChild(item)
          ov.appendChild(gm)
          env3.body.appendChild(ov)
        },
      })
      wrap3.appendChild(plus3)
      area3.appendChild(wrap3)
      env3.body.appendChild(area3)

      const res3 = JSON.parse(await env3.ctx.tulaiOpenUpload())
      assert.strictEqual(res3.opened, true)
      assert.strictEqual(agreed, true, '首次上传应自动点击 Agree 同意上传声明弹窗')
      assert.strictEqual(uploadClicks >= 1, true)
    }

    // 额度 / 限流 / 审核识别
    const quotaAlert = el('div', { attrs: { role: 'alert' }, text: "You've reached your daily image generation limit." })
    body.appendChild(quotaAlert)
    assert.strictEqual(ctx.tulaiAccountFault().quota, true)
    body.removeChild(quotaAlert)

    const rateAlert = el('div', { attrs: { role: 'alert' }, text: "You're sending too many requests. Please try again later." })
    body.appendChild(rateAlert)
    assert.strictEqual(ctx.tulaiAccountFault().rateLimit, true)
    body.removeChild(rateAlert)

    ctx.__tulaiMarkTask()
    const refuseResp = el('model-response', { text: "I can't generate that image because it violates our safety policies." })
    body.appendChild(refuseResp)
    assert.ok(ctx.tulaiAccountFault().moderation.length > 0, 'Gemini 审核拒绝应被 TULAIRefuse 识别')
  }

  console.log('gemini_dom_test ok')
}

run().catch((err) => {
  console.error(err)
  process.exit(1)
})
