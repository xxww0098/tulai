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
    this.classList = { toggle() {} }
  }
  append(...children) { this.children.push(...children) }
  attachShadow() { this.shadow = new Element('shadow'); return this.shadow }
  setAttribute(name, value) { this.attributes[name] = String(value) }
  getAttribute(name) { return this.attributes[name] || '' }
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
  let listener, removeTab, sent, failLoad = false
  const background = vm.createContext({
    URL, TextEncoder,
    browser: {
      tabs: { async get(id) { return { id, url: tabs.get(id), active: true } }, onRemoved: { addListener(fn) { removeTab = fn } } },
      runtime: { onMessage: { addListener(fn) { listener = fn } } },
    },
    TULAI_DOWNLOAD: { endpoint: 'http://127.0.0.1/download', token: 'test' },
    async fetch(_url, request) { sent = JSON.parse(request.body); return { async json() { return { ok: true, message: '已加入 tulai 下载列表' } } } },
  })
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'download/background.js'), 'utf8'), background)

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
    const window = { scrollY: 0, scrollBy({ top }) { this.scrollY += top } }
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
      state, location, host, panel, count, primary, auto, status, collapse: header.children[1], reopen: host.shadow.children[2],
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
  initial.primary.click()
  await tick()
  assert.ok(sent && sent.marker, '没有向 tulai 提交任务')
  assert.strictEqual(initial.primary.textContent, '下载已加载')
  assert.strictEqual(initial.status.textContent, '已加入 tulai 下载列表')
  assert.deepStrictEqual(JSON.parse(initial.host.dataset.tulaiTemuSnapshot).rows.map(item => item.id), [first.id, second.id])
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
  assert.deepStrictEqual(JSON.parse(returned.host.dataset.tulaiTemuSnapshot).rows.map(item => item.id).sort(), [first.id, second.id].sort())

  returned.location.href = 'https://www.temu.com/search_result.html?search_key=other'
  tabs.set(1, returned.location.href)
  returned.state.cards = [third]
  returned.refresh()
  await tick()
  assert.strictEqual(returned.count.textContent, '已识别 1 件商品')
  returned.primary.click()
  await tick()
  assert.deepStrictEqual(JSON.parse(returned.host.dataset.tulaiTemuSnapshot).rows.map(item => item.id), [third.id], '新搜索不能继承旧商品')
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
}

main().catch(err => { console.error(err); process.exitCode = 1 })
