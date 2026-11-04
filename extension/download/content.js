(() => {
  const host = document.createElement('div')
  host.dataset.tulaiDownloadHelper = ''
  host.style.cssText = 'display:none;position:fixed;right:20px;bottom:20px;z-index:2147483647'
  const shadow = host.attachShadow({ mode: 'closed' })
  const style = document.createElement('style')
  style.textContent = `
    :host { color-scheme: light; font-family: -apple-system, BlinkMacSystemFont, sans-serif; }
    :host([data-tulai-temu]) { right: 80px !important; }
    * { box-sizing: border-box; }
    [hidden] { display: none !important; }
    .panel { width: 246px; max-width: calc(100vw - 32px); padding: 12px; color: #1a2638;
      background: #fff; border: 1px solid #d8e0ee; border-radius: 14px; box-shadow: 0 8px 28px #1b2b4933; }
    .panel.compact { width: auto; padding: 0; border: 0; background: transparent; box-shadow: none; }
    .header { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 7px; }
    .title { font-size: 13px; font-weight: 700; letter-spacing: .01em; }
    .count { margin: 0 0 10px; color: #50617b; font-size: 12px; font-variant-numeric: tabular-nums; }
    .actions { display: flex; gap: 7px; }
    button { border: 0; font: 600 13px/1.3 -apple-system, BlinkMacSystemFont, sans-serif; cursor: pointer; }
    button:focus-visible { outline: 3px solid #91adff; outline-offset: 2px; }
    button:disabled { opacity: .65; cursor: wait; }
    .primary, .secondary, .reopen { min-height: 40px; border-radius: 9px; padding: 9px 12px; white-space: nowrap; }
    .primary, .reopen { color: #fff; background: #315efb; }
    .primary:hover:not(:disabled), .reopen:hover { background: #244cde; }
    .primary { flex: 1; }
    .secondary { color: #2448ad; background: #eef3ff; border: 1px solid #d2dffd; }
    .secondary:hover:not(:disabled) { background: #e0eaff; }
    .collapse { min-width: 32px; min-height: 28px; padding: 2px 4px; color: #50617b; background: transparent; font-weight: 500; }
    .collapse:hover { color: #1a2638; }
    .status { margin: 9px 0 0; color: #50617b; font-size: 12px; line-height: 1.45; overflow-wrap: anywhere; }
    .status[data-error] { color: #b42330; }
    .compact .status { max-width: 240px; padding: 7px 10px; background: #fff; border: 1px solid #d8e0ee; border-radius: 8px; }
    .status:empty { display: none; }
    @media (max-width: 600px) { :host([data-tulai-temu]) { right: 12px !important; } }
  `
  const panel = document.createElement('section')
  panel.className = 'panel compact'
  panel.setAttribute('aria-label', 'tulai 下载助手')
  const header = document.createElement('div')
  header.className = 'header'
  const title = document.createElement('span')
  title.className = 'title'
  title.textContent = 'tulai 下载助手'
  const collapse = document.createElement('button')
  collapse.type = 'button'
  collapse.className = 'collapse'
  collapse.textContent = '收起'
  collapse.setAttribute('aria-label', '收起下载助手')
  header.append(title, collapse)
  const count = document.createElement('p')
  count.className = 'count'
  const actions = document.createElement('div')
  actions.className = 'actions'
  const primary = document.createElement('button')
  primary.type = 'button'
  primary.className = 'primary'
  const auto = document.createElement('button')
  auto.type = 'button'
  auto.className = 'secondary'
  actions.append(primary, auto)
  const status = document.createElement('p')
  status.className = 'status'
  status.setAttribute('role', 'status')
  status.setAttribute('aria-live', 'polite')
  panel.append(header, count, actions, status)
  const reopen = document.createElement('button')
  reopen.type = 'button'
  reopen.className = 'reopen'
  reopen.textContent = 'tulai 下载'
  reopen.setAttribute('aria-label', '展开下载助手')
  reopen.hidden = true
  shadow.append(style, panel, reopen)
  document.documentElement.append(host)

  let searchKey = '', rows = new Map(), truncated = false, blocked = false
  let autoTimer = null, running = false, lastMore = 0, quiet = 0, lastHeight = 0, lastY = -1, lastCount = -1
  let refreshTimer = null, statusTimer = null, cacheTimer = null, sending = false, starting = false
  let loadingCache = Promise.resolve(), cacheReady = false
  const maxCards = 10000

  function temuSearchKey() {
    try {
      const u = new URL(location.href)
      if (u.protocol !== 'https:' || !['temu.com', 'www.temu.com'].includes(u.hostname) ||
          !/^\/(?:[a-z]{2}(?:-[a-z]{2})?\/)?search_result\.html$/i.test(u.pathname) ||
          u.searchParams.getAll('search_key').length !== 1 || !u.searchParams.get('search_key').trim()) return ''
      const parts = []
      for (const part of u.search.slice(1).split('&')) {
        const split = part.indexOf('=')
        const key = decodeURIComponent((split < 0 ? part : part.slice(0, split)).replace(/\+/g, ' '))
        const value = decodeURIComponent((split < 0 ? '' : part.slice(split + 1)).replace(/\+/g, ' '))
        if (!key.startsWith('refer_page_') && !key.startsWith('_x_') && key !== 'search_method' && key !== 'srch_enter_source' && key !== 'is_back') parts.push(`${encodeURIComponent(key)}=${encodeURIComponent(value)}`)
      }
      return 'https://temu.com' + u.pathname + '?' + parts.sort().join('&')
    } catch (_) { return '' }
  }
  function message(text, error = false, clear = false) {
    if (statusTimer) clearTimeout(statusTimer)
    statusTimer = null
    status.textContent = text
    status.toggleAttribute('data-error', error)
    if (clear) statusTimer = setTimeout(() => { status.textContent = ''; statusTimer = null }, 6000)
  }
  function controls() {
    const isSearch = !!searchKey
    panel.classList.toggle('compact', !isSearch)
    header.hidden = count.hidden = auto.hidden = !isSearch
    host.toggleAttribute('data-tulai-temu', isSearch)
    count.textContent = `已识别 ${rows.size} 件商品`
    primary.textContent = sending ? '正在发送…' : running ? '停止并下载' : isSearch ? '下载已加载' : '下载到 tulai'
    auto.textContent = starting ? '正在准备…' : running ? '暂停加载' : '自动加载'
    auto.setAttribute('aria-pressed', String(running))
    primary.disabled = sending || starting || (isSearch && (blocked || rows.size === 0))
    auto.disabled = sending || starting || blocked || rows.size === 0 || (!running && rows.size >= maxCards)
  }
  function stopAuto(note = '') {
    running = false
    if (autoTimer) clearTimeout(autoTimer)
    autoTimer = null
    if (note) message(note)
    controls()
  }
  async function saveCache() {
    if (!searchKey || rows.size === 0) return true
    if (!cacheReady || temuSearchKey() !== searchKey) return false
    try {
      const result = await browser.runtime.sendMessage({ type: 'TEMU_CACHE_SAVE', key: searchKey, url: location.href, rows: [...rows.values()], truncated })
      return !!(result && result.ok)
    } catch (_) { return false }
  }
  function scheduleCache() {
    if (cacheTimer) return
    const key = searchKey
    cacheTimer = setTimeout(async () => {
      cacheTimer = null
      await loadingCache
      if (key === searchKey) await saveCache()
    }, 500)
  }
  function rememberSearch() {
    const key = temuSearchKey()
    if (key !== searchKey) {
      if (running) stopAuto('页面已变化，请在当前搜索页重新开始')
      else message('')
      if (cacheTimer) clearTimeout(cacheTimer)
      cacheTimer = null
      searchKey = key
      rows = new Map()
      cacheReady = false
      truncated = blocked = false
      delete host.dataset.tulaiTemuSnapshot
      loadingCache = key ? browser.runtime.sendMessage({ type: 'TEMU_CACHE_LOAD', key, url: location.href }).then(saved => {
        if (searchKey !== key || temuSearchKey() !== key || !saved || !saved.ok || !Array.isArray(saved.rows)) return
        cacheReady = true
        for (const row of saved.rows) {
          if (row && typeof row.id === 'string' && !rows.has(row.id) && rows.size < maxCards) rows.set(row.id, row)
        }
        truncated ||= !!saved.truncated
        controls()
        if (rows.size > 0) host.style.display = 'block'
      }).catch(() => {}) : Promise.resolve()
    }
    if (!key) return
    const snapshot = tulaiTemuSnapshot()
    blocked = snapshot.blocked
    let changed = false
    for (const row of snapshot.rows) {
      if (!rows.has(row.id) && rows.size >= maxCards) { truncated = true; break }
      const old = rows.get(row.id)
      if (!old || old.image !== row.image || old.link !== row.link || old.title !== row.title) changed = true
      rows.set(row.id, row)
    }
    if (snapshot.truncated && !truncated) changed = true
    truncated ||= snapshot.truncated
    if (changed) scheduleCache()
    if (blocked) {
      if (running) stopAuto()
      if (status.textContent !== '请先完成网页登录或安全验证') message('请先完成网页登录或安全验证', true)
    } else if (status.textContent === '请先完成网页登录或安全验证') message('')
    if ((truncated || rows.size >= maxCards) && running) stopAuto('已达到 10000 件上限，请下载或缩小搜索范围')
  }
  function httpImage(value) {
    if (!value || !String(value).trim()) return false
    try {
      const url = new URL(value, document.baseURI)
      return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password
    } catch (_) { return false }
  }
  function hasImages() {
    const images = document.images
    // shortcut: inspect at most 10000 images; expand with the capture limit for larger pages.
    for (let i = 0; i < Math.min(images.length, 10000); i++) {
      const img = images[i]
      if (img.complete && img.naturalWidth > 1 && img.naturalHeight > 1 && httpImage(img.currentSrc || img.src)) return true
    }
    const walker = document.createTreeWalker(document.documentElement, NodeFilter.SHOW_ELEMENT)
    let element = document.documentElement
    // shortcut: inspect at most 2000 links and backgrounds; expand if larger pages need it.
    for (let i = 0; i < 2000 && element; i++, element = walker.nextNode()) {
      if (element.tagName === 'A' && httpImage(element.href) && /\.(?:jpe?g|png|gif|webp|avif|bmp|tiff?|svg|heic|heif)$/i.test(new URL(element.href).pathname)) return true
      const match = /url\(["']?(.*?)["']?\)/i.exec(getComputedStyle(element).backgroundImage)
      if (match && httpImage(match[1])) return true
    }
    return false
  }
  function refresh() {
    rememberSearch()
    controls()
    const temuWall = /^(?:www\.)?temu\.com$/i.test(location.hostname) && /^\/(?:login|bgn_verification)\.html$/i.test(location.pathname)
    host.style.display = !temuWall && (searchKey ? rows.size > 0 || blocked : hasImages()) ? 'block' : 'none'
  }
  function schedule() {
    if (refreshTimer) return
    refreshTimer = setTimeout(() => { refreshTimer = null; refresh() }, 250)
  }
  async function step() {
    if (!running) return
    refresh()
    if (!running) return
    const list = document.querySelector('.js-search-goodsList')
    const candidates = list ? list.querySelectorAll('[role="button"], button') : []
    let more = null
    for (let i = 0; i < candidates.length; i++) {
      const node = candidates[i]
      const label = (node.getAttribute('aria-label') || node.textContent || '').trim()
      const rect = node.getBoundingClientRect()
      if (!node.closest('.EKDT7a3v') && !node.disabled && node.getAttribute('aria-disabled') !== 'true' &&
        /^(?:see more(?: items)?|load more|查看更多|显示更多|加载更多)$/i.test(label) &&
        rect.width > 0 && rect.height > 0 && rect.top < innerHeight * 1.4 && rect.bottom > 0) { more = node; break }
    }
    if (more && Date.now() - lastMore > 1800) {
      const key = searchKey
      await loadingCache
      const saved = await saveCache()
      if (!running) return
      if (key !== searchKey || key !== temuSearchKey()) { stopAuto('页面已变化，请在当前搜索页重新开始'); refresh(); return }
      if (!saved) { stopAuto('无法暂存商品，请先下载已加载的结果'); return }
      more.click(); lastMore = Date.now()
    }
    const height = document.documentElement.scrollHeight
    const y = window.scrollY
    quiet = height === lastHeight && y === lastY && rows.size === lastCount ? quiet + 1 : 0
    lastHeight = height; lastY = y; lastCount = rows.size
    if (quiet >= 5) { stopAuto('暂时没有更多商品，可以下载已加载的结果'); return }
    window.scrollBy({ top: Math.max(400, Math.floor(innerHeight * .8)), behavior: 'auto' })
    autoTimer = setTimeout(step, 1000)
  }
  async function send() {
    if (sending || starting) return
    if (running) stopAuto()
    sending = true
    controls()
    message('')
    try {
      refresh()
      const key = searchKey
      await loadingCache
      if (key !== searchKey || key !== temuSearchKey()) { message('页面已变化，请重新点击下载', true); return }
      if (key) {
        if (!cacheReady) { message('无法恢复已加载商品，请刷新页面后重试', true); return }
        if (blocked || rows.size === 0) { message('请等待商品加载或完成网页验证', true); return }
        const value = JSON.stringify({ url: location.href, rows: [...rows.values()], truncated })
        if (new TextEncoder().encode(value).length > 8 * 1024 * 1024) { message('商品信息过多，请缩小搜索范围后下载', true); return }
        host.dataset.tulaiTemuSnapshot = value
      }
      const hex = Array.from(crypto.getRandomValues(new Uint8Array(16)), value => value.toString(16).padStart(2, '0')).join('')
      const marker = `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
      host.dataset.tulaiDownloadRequest = marker
      const result = await browser.runtime.sendMessage({ type: 'DOWNLOAD_PAGE', marker })
      message(result && result.message || '无法发送，请重试', !result || !result.ok, !!(result && result.ok))
    } catch (_) {
      message('无法连接 tulai，请确认应用正在运行', true)
    } finally {
      sending = false
      delete host.dataset.tulaiDownloadRequest
      controls()
    }
  }
  primary.addEventListener('click', event => { if (event.isTrusted) { event.stopPropagation(); send() } })
  auto.addEventListener('click', async event => {
    if (!event.isTrusted || sending || starting) return
    event.stopPropagation()
    if (running) { stopAuto('已暂停，可继续加载或下载'); return }
    refresh()
    if (!searchKey || blocked || rows.size === 0) return
    const key = searchKey
    starting = true; controls()
    await loadingCache
    const saved = await saveCache()
    starting = false
    if (key !== searchKey || key !== temuSearchKey()) { refresh(); return }
    if (!saved) { message('无法暂存商品，请先下载已加载的结果', true); controls(); return }
    running = true; quiet = 0; lastY = -1; lastHeight = 0; lastCount = -1; lastMore = 0
    message('正在自动加载，可随时停止并下载')
    controls()
    step()
  })
  collapse.addEventListener('click', event => {
    if (!event.isTrusted) return
    panel.hidden = true; reopen.hidden = false
  })
  reopen.addEventListener('click', event => {
    if (!event.isTrusted) return
    panel.hidden = false; reopen.hidden = true
  })
  document.addEventListener('load', schedule, true)
  document.addEventListener('scroll', schedule, { passive: true })
  new MutationObserver(changes => {
    if (changes.some(change => change.target !== host && !host.contains(change.target))) schedule()
  }).observe(document.documentElement, { subtree: true, childList: true, attributes: true, attributeFilter: ['href', 'src', 'srcset', 'data-src', 'data-tooltip', 'data-state', 'style', 'class', 'hidden', 'aria-hidden'] })
  refresh()
})()
