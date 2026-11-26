const assert = require('assert')
const fs = require('fs')
const path = require('path')
const vm = require('vm')
const { TextEncoder } = require('util')

class Element {
  constructor(tag) {
    this.tagName = tag.toUpperCase()
    this.children = []
    this.dataset = {}
    this.style = {}
    this.attributes = {}
    this.listeners = {}
    this.hidden = false
    this.disabled = false
    this.classes = new Set()
    this.classList = {
      toggle: (name, on) => {
        const next = on === undefined ? !this.classes.has(name) : !!on
        if (next) this.classes.add(name)
        else this.classes.delete(name)
        return next
      },
      contains: name => this.classes.has(name),
    }
  }
  append(...children) { this.children.push(...children) }
  attachShadow() { this.shadow = new Element('shadow'); return this.shadow }
  setAttribute(name, value) { this.attributes[name] = String(value) }
  getAttribute(name) { return this.attributes[name] || '' }
  removeAttribute(name) { delete this.attributes[name] }
  toggleAttribute(name, on) { if (on) this.attributes[name] = ''; else delete this.attributes[name] }
  addEventListener(type, fn) { this.listeners[type] = fn }
  contains(target) { return this.children.includes(target) }
  closest() { return null }
  click() {
    if (!this.disabled && this.listeners.click) this.listeners.click({ isTrusted: true, stopPropagation() {} })
  }
}

function row(id) {
  return { id, title: `商品 ${id}`, image: `https://img.kwcdn.com/${id}.jpg`, link: `https://www.temu.com/item-g-${id}.html` }
}
const first = row('601099513224343'), second = row('601099513224344'), third = row('601099513224345')
const tick = () => new Promise(resolve => setImmediate(resolve))

class XrayURL extends URL {
  get searchParams() {
    const params = super.searchParams
    return new Proxy(params, { get(target, key) {
      if (key === 'keys' || key === Symbol.iterator) throw new Error('Xray iterator denied')
      const value = Reflect.get(target, key, target)
      return typeof value === 'function' ? value.bind(target) : value
    } })
  }
}

async function main() {
  const tabs = new Map()
  const temuTabs = new Map()
  let sent, failLoad = false
  const removeTab = id => temuTabs.delete(id)
  async function listener(message, sender) {
    if (!message || !['DOWNLOAD_PAGE', 'TEMU_CACHE_LOAD', 'TEMU_CACHE_SAVE'].includes(message.type)) return undefined
    const tabURL = tabs.get(sender.tab.id)
    const url = new URL(tabURL)
    if (message.type !== 'DOWNLOAD_PAGE') {
      if (tabURL !== message.url || url.protocol !== 'https:' || !['temu.com', 'www.temu.com'].includes(url.hostname) ||
          !/^\/(?:[a-z]{2}(?:-[a-z]{2})?\/)?search_result\.html$/i.test(url.pathname) ||
          typeof message.key !== 'string' || message.key.length > 8192) return { ok: false }
      if (message.type === 'TEMU_CACHE_LOAD') {
        const cached = temuTabs.get(sender.tab.id)
        return { ok: true, rows: cached && cached.key === message.key ? cached.rows : [], truncated: !!(cached && cached.key === message.key && cached.truncated) }
      }
      if (!Array.isArray(message.rows) || message.rows.length > 10000 || new TextEncoder().encode(JSON.stringify(message.rows)).length > 8 * 1024 * 1024) return { ok: false }
      temuTabs.set(sender.tab.id, { key: message.key, rows: message.rows, truncated: !!message.truncated })
      return { ok: true }
    }
    if (!/^[0-9a-f-]{36}$/i.test(message.marker || '')) return { ok: false, message: '请在网页中点击下载按钮' }
    sent = { url: tabURL, marker: message.marker }
    return { ok: true, message: '已加入 tulai 下载列表' }
  }

  function page(id, href, initialCards) {
    const state = { cards: initialCards, blocked: false }
    const location = new URL(href)
    tabs.set(id, href)
    const more = new Element('div')
    more.setAttribute('aria-label', 'See more items')
    more.getBoundingClientRect = () => ({ width: 100, height: 40, top: 200, bottom: 240 })
    more.click = () => { state.cards = [second]; more.getBoundingClientRect = () => ({ width: 0, height: 0, top: 0, bottom: 0 }) }
    const list = { querySelectorAll: () => [more] }
    const root = new Element('html')
    root.scrollHeight = 3000
    const document = {
      documentElement: root, baseURI: 'https://www.temu.com/', title: '搜索', images: [], listeners: {},
      createElement: tag => new Element(tag),
      querySelector: selector => selector === '.js-search-goodsList' ? list : null,
      addEventListener(type, fn) { this.listeners[type] = fn },
    }
    const timers = new Map()
    let nextTimer = 0
    const window = { scrollY: 0, scrollBy({ top }) { this.scrollY += top }, listeners: {}, addEventListener(type, fn) { this.listeners[type] = fn } }
    const context = vm.createContext({
      document, window, location, URL: XrayURL, TextEncoder, Uint8Array, Map, Date, innerHeight: 800,
      crypto: { getRandomValues(a) { a.fill(7); return a } },
      browser: { runtime: { async sendMessage(message) {
        if (failLoad && message.type === 'TEMU_CACHE_LOAD') throw new Error('background unavailable')
        return listener(message, { tab: { id }, frameId: 0 })
      } } },
      tulaiTemuSnapshot: () => ({ url: location.href, rows: state.cards, blocked: state.blocked, truncated: false }),
      MutationObserver: class { observe() {} },
      setTimeout(fn, ms) { const timer = ++nextTimer; timers.set(timer, { fn, ms }); return timer },
      clearTimeout(timer) { timers.delete(timer) },
    })
    vm.runInContext(fs.readFileSync(path.join(__dirname, 'download/content.js'), 'utf8'), context)
    const host = root.children[0], panel = host.shadow.children[1]
    const [header, count, actions, status] = panel.children
    const [primary, auto] = actions.children
    return {
      state, location, root, host, panel, count, primary, auto, status, collapse: header.children[1], reopen: host.shadow.children[2], window,
      refresh() { document.listeners.scroll(); this.runTimer(250) },
      runTimer(ms) {
        const next = [...timers.entries()].find(([, timer]) => timer.ms === ms)
        assert.ok(next, `找不到 ${ms}ms 定时器`)
        timers.delete(next[0]); next[1].fn()
      },
    }
  }

  const initial = page(1, 'https://www.temu.com/search_result.html?search_key=lamp', [first])
  await tick()
  assert.strictEqual(initial.count.textContent, '已识别 1 件商品')
  assert.strictEqual(initial.primary.textContent, '下载已加载')
  assert.strictEqual(initial.auto.textContent, '自动加载')
  initial.auto.click()
  await tick()
  assert.strictEqual(initial.primary.textContent, '停止并下载')
  assert.strictEqual(initial.auto.textContent, '暂停加载')
  assert.strictEqual(initial.auto.attributes['aria-pressed'], 'true')
  initial.runTimer(1000)
  await tick()
  assert.strictEqual(initial.count.textContent, '已识别 2 件商品', '已移除的商品仍应计入')
  assert.ok(initial.panel.classList.contains('temu'), 'Temu 搜索面板应使用 ShadowRoot 内 .panel.temu 类')
  assert.strictEqual(Object.keys(initial.host.attributes).some(k => /tulai/i.test(k)), false, '宿主轻 DOM 属性不得含 tulai')
  initial.primary.click()
  await tick()
  assert.ok(sent && sent.marker, '没有向下载服务提交任务')
  assert.strictEqual(initial.primary.textContent, '下载已加载')
  assert.strictEqual(initial.status.textContent, '已加入 tulai 下载列表')
  assert.deepStrictEqual(JSON.parse(initial.host.getAttribute('data-pb-dl-snap')).rows.map(item => item.id), [first.id, second.id])
  assert.strictEqual(initial.host.getAttribute('data-pb-dl-req'), '', '请求结束后应清除请求标记属性')
  initial.collapse.click()
  assert.ok(initial.panel.hidden && !initial.reopen.hidden)
  initial.reopen.click()
  assert.ok(!initial.panel.hidden && initial.reopen.hidden)

  // Simulate a full login navigation: the old content script is gone, but its tab cache survives.
  const returned = page(1, 'https://temu.com/search_result.html?is_back=1&search_key=lamp&search_method=user', [second])
  await tick()
  assert.strictEqual(returned.count.textContent, '已识别 2 件商品', '登录返回后应恢复旧商品')
  returned.primary.click()
  await tick()
  assert.deepStrictEqual(JSON.parse(returned.host.getAttribute('data-pb-dl-snap')).rows.map(item => item.id).sort(), [first.id, second.id].sort())

  returned.location.href = 'https://www.temu.com/search_result.html?search_key=other'
  tabs.set(1, returned.location.href)
  returned.state.cards = [third]
  returned.refresh()
  await tick()
  assert.strictEqual(returned.count.textContent, '已识别 1 件商品')
  returned.primary.click()
  await tick()
  assert.deepStrictEqual(JSON.parse(returned.host.getAttribute('data-pb-dl-snap')).rows.map(item => item.id), [third.id], '新搜索不能继承旧商品')
  returned.state.blocked = true
  returned.refresh()
  assert.ok(returned.primary.disabled && returned.auto.disabled, '验证页面不得继续下载或自动滚动')

  const isolated = page(2, 'https://www.temu.com/search_result.html?search_key=lamp', [third])
  await tick()
  assert.strictEqual(isolated.count.textContent, '已识别 1 件商品', '不同标签页不能共享商品')
  const key = 'https://temu.com/search_result.html?search_key=lamp'
  assert.strictEqual((await listener({ type: 'TEMU_CACHE_LOAD', key, url: 'https://www.temu.com/other' }, { tab: { id: 2 }, frameId: 0 })).ok, false)
  removeTab(1)
  const reopened = page(1, 'https://www.temu.com/search_result.html?search_key=lamp', [third])
  await tick()
  assert.strictEqual(reopened.count.textContent, '已识别 1 件商品', '关闭标签页后缓存应清除')

  failLoad = true
  const unavailable = page(3, 'https://www.temu.com/search_result.html?search_key=lamp', [first])
  await tick()
  unavailable.auto.click()
  await tick()
  assert.strictEqual(unavailable.auto.textContent, '自动加载', '缓存不可用时不能开始自动加载')
  unavailable.primary.click()
  await tick()
  assert.strictEqual(unavailable.status.textContent, '无法恢复已加载商品，请刷新页面后重试')

  // Transient TEMU_CACHE_LOAD failure recovers on user download click once cache becomes reachable.
  failLoad = false
  unavailable.primary.click()
  await tick()
  assert.strictEqual(unavailable.status.textContent, '已加入 tulai 下载列表', '缓存短暂失败后点击下载应重试恢复')

  // SPA popstate/hashchange updates Temu search state and localized login/verification walls hide the panel.
  unavailable.location.href = 'https://www.temu.com/zh-hant/login.html'
  tabs.set(3, unavailable.location.href)
  assert.strictEqual(typeof unavailable.window.listeners.popstate, 'function', '应监听 popstate')
  assert.strictEqual(typeof unavailable.window.listeners.hashchange, 'function', '应监听 hashchange')
  unavailable.window.listeners.popstate()
  unavailable.runTimer(250)
  await tick()
  assert.strictEqual(unavailable.host.style.display, 'none', '多语言 Temu 登录页应隐藏浮窗')

  // Disconnected host is re-attached on refresh.
  unavailable.host.isConnected = false
  const beforeChildren = unavailable.root.children.length
  unavailable.refresh()
  assert.strictEqual(unavailable.root.children.length, beforeChildren + 1, '宿主被移除后应自动重新挂载')

  // Verify open Shadow DOM image detection and multi-layer CSS background-image detection.
  testShadowAndMultiLayerBackground()

  // Verify temu.js structural fallback handles rotated card classes, aria-hidden image wrappers,
  // multi-image cards, and /goods.html?goods_id=... links.
  testTemuSnapshotFallback()
}

function testShadowAndMultiLayerBackground() {
  const makeNode = (tag, extra = {}) => Object.assign(new Element(tag), { nodeType: 1 }, extra)
  const runNonTemu = ({ root, styles }) => {
    const walkList = rootNode => {
      const list = []
      const visit = node => {
        for (const child of node.children || []) {
          list.push(child)
          visit(child)
        }
      }
      visit(rootNode)
      return list
    }
    const document = {
      documentElement: root,
      baseURI: 'https://example.test/',
      title: 'Gallery',
      images: [],
      listeners: {},
      createElement: tag => makeNode(tag),
      createTreeWalker(start) {
        const items = walkList(start)
        let idx = 0
        return { nextNode() { return idx < items.length ? items[idx++] : null } }
      },
      addEventListener(type, fn) { this.listeners[type] = fn },
    }
    const ctx = vm.createContext({
      document,
      window: { scrollY: 0, scrollBy() {}, addEventListener() {} },
      location: new URL('https://example.test/page'),
      URL,
      NodeFilter: { SHOW_ELEMENT: 1 },
      TextEncoder,
      Uint8Array,
      Map,
      Date,
      innerHeight: 800,
      getComputedStyle: el => styles.get(el) || { backgroundImage: 'none' },
      crypto: { getRandomValues(a) { a.fill(3); return a } },
      browser: { runtime: { async sendMessage() { return { ok: true, message: 'ok' } } } },
      tulaiTemuSnapshot: () => ({ url: 'https://example.test/page', rows: [], blocked: false, truncated: false }),
      MutationObserver: class { observe() {} },
      setTimeout: fn => fn(),
      clearTimeout() {},
    })
    vm.runInContext(fs.readFileSync(path.join(__dirname, 'download/content.js'), 'utf8'), ctx)
    return root.children[root.children.length - 1]
  }

  // Case 1: Image inside open shadowRoot
  const root1 = makeNode('html')
  const widget = makeNode('div')
  const shadowImg = makeNode('img', { complete: true, naturalWidth: 64, naturalHeight: 64, currentSrc: 'https://cdn.example.test/shadow.png' })
  widget.shadowRoot = { nodeType: 11, children: [shadowImg] }
  root1.append(widget)
  const host1 = runNonTemu({ root: root1, styles: new Map() })
  assert.strictEqual(host1.style.display, 'block', 'open shadowRoot 中的图片应触发下载按钮显示')

  // Case 2: Multi-layer background-image where first url() is data: and second is https://
  const root2 = makeNode('html')
  const card = makeNode('div')
  root2.append(card)
  const styles2 = new Map([[card, { backgroundImage: 'url("data:image/svg+xml,<svg/>"), url("https://cdn.example.test/bg.webp")' }]])
  const host2 = runNonTemu({ root: root2, styles: styles2 })
  assert.strictEqual(host2.style.display, 'block', '多层 background-image 中的 HTTP 图片应触发下载按钮显示')
}

function testTemuSnapshotFallback() {
  class FakeNode {
    constructor(tag, attrs = {}, opts = {}) {
      this.tagName = tag.toUpperCase()
      this.attributes = { ...attrs }
      this.children = []
      this.parentElement = null
      this.hidden = !!opts.hidden
      this.style = opts.style || { display: 'block', visibility: 'visible', opacity: '1' }
      this.rect = opts.rect || { width: 200, height: 240 }
      this.textContent = opts.text || ''
      this.innerText = opts.text || ''
      this.complete = opts.complete !== false
      this.naturalWidth = opts.naturalWidth ?? 400
      this.src = attrs.src || ''
      this.currentSrc = attrs.src || ''
      this.href = attrs.href ? new URL(attrs.href, 'https://www.temu.com/').href : ''
    }
    append(...nodes) {
      for (const n of nodes) {
        n.parentElement = this
        this.children.push(n)
      }
      return this
    }
    getAttribute(name) { return Object.prototype.hasOwnProperty.call(this.attributes, name) ? this.attributes[name] : null }
    getBoundingClientRect() { return this.rect }
    matches(selector) {
      return selector.split(',').map(s => s.trim()).some(part => {
        if (part === 'img') return this.tagName === 'IMG'
        if (part === 'a[href]') return this.tagName === 'A' && !!this.href
        if (part === 'img[alt]') return this.tagName === 'IMG' && !!this.getAttribute('alt')
        if (part === 'img.goods-img-external') return this.tagName === 'IMG' && (this.getAttribute('class') || '').split(/\s+/).includes('goods-img-external')
        if (part === '.goods-img-external img') return this.tagName === 'IMG' && !!this.closest('.goods-img-external')
        if (part === '.goods-img-external') return (this.getAttribute('class') || '').split(/\s+/).includes('goods-img-external')
        if (part === '.js-search-goodsList') return (this.getAttribute('class') || '').split(/\s+/).includes('js-search-goodsList')
        if (part === '.EKDT7a3v') return (this.getAttribute('class') || '').split(/\s+/).includes('EKDT7a3v')
        if (part === 'main') return this.tagName === 'MAIN'
        if (part === 'h2' || part === 'h3') return this.tagName === part.toUpperCase()
        if (part === '[title]') return !!this.getAttribute('title')
        if (part === '[id^="goodName-"]') return (this.getAttribute('id') || '').startsWith('goodName-')
        if (part === '[data-tooltip^="goodName-"]') return (this.getAttribute('data-tooltip') || '').startsWith('goodName-')
        if (part === 'a[href*="-g-"]') return this.tagName === 'A' && this.href.includes('-g-')
        if (part === 'a[href*="goods_id="]') return this.tagName === 'A' && this.href.includes('goods_id=')
        if (part === 'a[href*="goodsId="]') return this.tagName === 'A' && this.href.includes('goodsId=')
        if (part === 'form' || part === 'iframe' || part === 'header' || part === 'nav' || part === 'footer') return this.tagName === part.toUpperCase()
        if (part === '[role="dialog"]') return this.getAttribute('role') === 'dialog'
        if (part === '[role="navigation"]') return this.getAttribute('role') === 'navigation'
        if (part === '[class*="search-goodsList"]') return (this.getAttribute('class') || '').includes('search-goodsList')
        if (part === '[class*="goodsList"]') return (this.getAttribute('class') || '').includes('goodsList')
        return false
      })
    }
    closest(selector) {
      let cur = this
      while (cur) {
        if (cur.matches(selector)) return cur
        cur = cur.parentElement
      }
      return null
    }
    querySelectorAll(selector) {
      const out = []
      const walk = node => {
        for (const child of node.children) {
          if (selector.includes(' ') && !selector.includes(',')) {
            const [parentSel, childSel] = selector.split(/\s+/)
            if (child.matches(childSel) && child.parentElement && child.parentElement.closest(parentSel)) out.push(child)
          } else if (child.matches(selector)) {
            out.push(child)
          }
          walk(child)
        }
      }
      walk(this)
      return out
    }
    querySelector(selector) { return this.querySelectorAll(selector)[0] || null }
  }

  const body = new FakeNode('body')
  const list = new FakeNode('div', { class: 'js-search-goodsList' })
  // Rotated card class (no .EKDT7a3v) with aria-hidden image link wrapper + hover badge image
  const card1 = new FakeNode('div', { class: '_9xRotatedCard' })
  const imgWrap1 = new FakeNode('a', { href: 'https://www.temu.com/paint-by-numbers-g-601099513224343.html', 'aria-hidden': 'true' })
  const badgeImg1 = new FakeNode('img', { src: 'https://img.kwcdn.com/badge/promo.png' }, { rect: { width: 24, height: 24 } })
  const mainImg1 = new FakeNode('img', { src: 'https://img-us.kwcdn.com/product/fancy/oil-paint.jpg?imageView2/2/w/800', alt: '数字油画' }, { rect: { width: 240, height: 240 } })
  imgWrap1.append(badgeImg1, mainImg1)
  const title1 = new FakeNode('h2', {}, { text: '数字油画 DIY' })
  const link1 = new FakeNode('a', { href: 'https://www.temu.com/paint-by-numbers-g-601099513224343.html' })
  link1.append(title1)
  card1.append(imgWrap1, link1)

  // Direct <a> card using /goods.html?goods_id=...
  const card2 = new FakeNode('a', { href: 'https://www.temu.com/goods.html?goods_id=601099513224344&refer_page_name=search', title: '风景油画' })
  const img2 = new FakeNode('img', { src: 'https://aimg.kwcdn.com/product/landscape.jpg' }, { rect: { width: 220, height: 220 } })
  card2.append(img2)

  // Stray non-product image outside cards must be ignored
  const stray = new FakeNode('img', { src: 'https://img.kwcdn.com/product/not-result.jpg' }, { rect: { width: 100, height: 100 } })
  list.append(card1, card2)
  body.append(list, stray)

  const doc = {
    body,
    documentElement: body,
    baseURI: 'https://www.temu.com/',
    title: 'Temu',
    querySelectorAll: sel => body.querySelectorAll(sel),
    querySelector: sel => body.querySelector(sel),
  }
  const ctx = vm.createContext({
    document: doc,
    location: { href: 'https://www.temu.com/search_result.html?search_key=%E6%95%B0%E5%AD%97%E6%B2%B9%E7%94%BB' },
    URL,
    Map,
    Set,
    getComputedStyle: n => n.style,
  })
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'download/temu.js'), 'utf8'), ctx)
  const snap = vm.runInContext('tulaiTemuSnapshot()', ctx)
  assert.strictEqual(snap.blocked, false)
  assert.strictEqual(snap.rows.length, 2, `rotated Temu DOM should extract 2 rows, got ${JSON.stringify(snap)}`)
  assert.strictEqual(snap.rows[0].id, '601099513224343')
  assert.strictEqual(snap.rows[0].title, '数字油画 DIY')
  assert.strictEqual(snap.rows[0].image, 'https://img-us.kwcdn.com/product/fancy/oil-paint.jpg?imageView2/2/w/800')
  assert.strictEqual(snap.rows[1].id, '601099513224344')
  assert.strictEqual(snap.rows[1].title, '风景油画')
  assert.strictEqual(snap.rows[1].image, 'https://aimg.kwcdn.com/product/landscape.jpg')
}

main().catch(err => { console.error(err); process.exitCode = 1 })
