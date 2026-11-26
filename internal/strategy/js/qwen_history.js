// tulai 千问聊天历史记录删除页面脚本：适配 react-window 虚拟列表与 Radix 下拉菜单。
(function () {
  if (typeof window !== 'undefined' && window.__tulaiQwenHistory) return

  const RATE_LIMIT_RE = /过于频繁|操作频繁|请求过于频繁|请稍后再试|访问太频繁|稍后重试|rate limit|too many requests/i
  const CAPTCHA_RE = /滑动.*验证|向右滑动|安全验证|请完成验证|人机验证/

  function normalize(s) {
    return String(s || '').replace(/\uFEFF/g, '').replace(/\s+/g, ' ').trim()
  }

  function all(root, sel) {
    if (!root || !root.querySelectorAll) return []
    try { return Array.from(root.querySelectorAll(sel)) } catch (e) { return [] }
  }

  function styleOf(el) {
    try { return getComputedStyle(el) } catch (e) { return { display: '', visibility: '', opacity: '1', pointerEvents: '' } }
  }

  function visible(el) {
    if (!el || el.isConnected === false) return false
    const r = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 0, height: 0 }
    if (r.width <= 4 || r.height <= 4) return false
    const st = styleOf(el)
    return st.display !== 'none' && st.visibility !== 'hidden' && parseFloat(st.opacity || '1') > 0.05
  }

  function enabled(el) {
    return visible(el) && !el.disabled && (!el.getAttribute || el.getAttribute('aria-disabled') !== 'true')
  }

  function conversationIdFromHref(href) {
    try {
      const u = new URL(String(href || ''), (typeof location !== 'undefined' && location && location.href) || 'https://www.qianwen.com/')
      const parts = u.pathname.split('/').filter(Boolean)
      const i = parts.indexOf('chat')
      return i >= 0 && parts[i + 1] ? parts[i + 1] : ''
    } catch (e) {
      return ''
    }
  }

  function sleep(ms) {
    return new Promise(function (resolve) { setTimeout(resolve, ms) })
  }

  function visualOrder(el) {
    if (el && el.getBoundingClientRect) {
      const r = el.getBoundingClientRect()
      if (typeof r.top === 'number' && !isNaN(r.top) && (r.top !== 0 || r.height > 0)) return r.top
    }
    let cur = el
    for (let d = 0; cur && d < 4; d++, cur = cur.parentElement) {
      const styleStr = String((cur.getAttribute && cur.getAttribute('style')) || (cur.style && cur.style.transform) || '')
      const m = /translateY\((-?\d+(?:\.\d+)?)px\)/i.exec(styleStr)
      if (m) return parseFloat(m[1])
      const idx = cur.getAttribute && cur.getAttribute('data-react-window-index')
      if (idx != null && idx !== '') return Number(idx) * 100
    }
    return 0
  }

  function scrollerOf(doc) {
    const panel = doc && doc.querySelector && doc.querySelector('[data-testid="qianwen-layout-left-panel"]')
    return (panel && panel.querySelector && (panel.querySelector('[role="list"].sider-scrollbar') || panel.querySelector('.sider-scrollbar') || panel.querySelector('[role="list"]'))) ||
      (doc && doc.querySelector && (doc.querySelector('[role="list"].sider-scrollbar') || doc.querySelector('.sider-scrollbar'))) ||
      null
  }

  function conversationItems(doc) {
    const scope = scrollerOf(doc) || (doc && doc.querySelector && doc.querySelector('[data-testid="qianwen-layout-left-panel"]')) || doc
    const nodes = all(scope, 'div[data-session-id][data-session-switch-target="normal"],[data-session-id][data-session-switch-target="normal"]')
    const seen = new Set()
    const items = []
    for (let i = 0; i < nodes.length; i++) {
      const el = nodes[i]
      if (!el || el.isConnected === false) continue
      const id = normalize(el.getAttribute && el.getAttribute('data-session-id'))
      if (!id || seen.has(id)) continue
      seen.add(id)
      const moreBtn = (el.querySelector && (
        el.querySelector('button[data-session-switch-target="session-tools"]') ||
        el.querySelector('button[aria-haspopup="menu"]')
      )) || null
      const title = normalize(el.innerText || '').slice(0, 80)
      items.push({
        id,
        title,
        el,
        moreBtn,
        active: (el.getAttribute && el.getAttribute('data-session-active')) === 'true',
        order: visualOrder(el),
      })
    }
    items.sort(function (a, b) { return a.order - b.order })
    return items
  }

  function hasVisibleLoginWall(doc) {
    if (conversationItems(doc).length > 0) return false
    const loginBtn = all(doc, 'button,[role="button"],a').some(function (el) {
      if (!visible(el)) return false
      return /^(登录|立即登录|登录\/注册|登录千问)$/.test(normalize(el.innerText))
    })
    return loginBtn
  }

  function checkBlocked(doc) {
    if (hasVisibleLoginWall(doc)) return { stopped: 'not_logged_in', error: '账号未登录' }
    const captchaNodes = all(doc, '#nc_wrapper,#baxia-dialog-content,.baxia-dialog,iframe[src*="captcha"],iframe[src*="baxia"],[class*="captcha"],[class*="baxia"]')
    if (captchaNodes.some(function (el) {
      if (el.tagName === 'IFRAME') return visible(el)
      return visible(el) && (el.id === 'nc_wrapper' || el.id === 'baxia-dialog-content' || CAPTCHA_RE.test(normalize(el.innerText)))
    })) {
      return { stopped: 'captcha', error: '触发安全验证' }
    }
    const rate = all(doc, '[role="alert"],[role="dialog"],[class*="toast"],[class*="notice"],.ant-message-notice').some(function (el) {
      if (!visible(el)) return false
      const t = normalize(el.innerText)
      return t.length > 0 && t.length <= 240 && RATE_LIMIT_RE.test(t)
    })
    if (rate) return { stopped: 'rate_limit', error: '触发操作频率限制' }
    return null
  }

  async function press(el) {
    if (!el) return false
    if (typeof window !== 'undefined' && typeof window.tulaiClick === 'function') {
      try { if (await window.tulaiClick(el)) return true } catch (e) {}
    }
    try { el.click(); return true } catch (e) { return false }
  }

  async function hover(el) {
    if (!el) return false
    if (typeof window !== 'undefined' && typeof window.tulaiHover === 'function') {
      try { if (await window.tulaiHover(el)) return true } catch (e) {}
    }
    try {
      if (typeof el.dispatchEvent === 'function' && typeof MouseEvent === 'function') {
        el.dispatchEvent(new MouseEvent('mouseover', { bubbles: true, cancelable: true }))
        el.dispatchEvent(new MouseEvent('mouseenter', { bubbles: false, cancelable: false }))
      }
      if (typeof el.onmouseenter === 'function') el.onmouseenter()
      return true
    } catch (e) {
      return false
    }
  }

  function sidebarVisible(doc) {
    const s = scrollerOf(doc)
    if (s && visible(s)) return true
    const panel = doc && doc.querySelector && doc.querySelector('[data-testid="qianwen-layout-left-panel"]')
    return !!(panel && visible(panel))
  }

  async function ensureSidebarOpen(doc, timeoutMs) {
    if (sidebarVisible(doc)) return true
    const toggle = all(doc, 'button,[role="button"]').find(function (el) {
      if (!enabled(el)) return false
      const icon = el.querySelector && el.querySelector('[data-icon-type="qwpcicon-sidebarLeft"],[data-icon-type="qwpcicon-sidebarRight"],use[xlink\\:href="#qwpcicon-sidebarLeft"],use[href="#qwpcicon-sidebarLeft"]')
      if (icon) return true
      const label = normalize((el.getAttribute('aria-label') || '') + ' ' + (el.getAttribute('title') || '') + ' ' + (el.innerText || ''))
      return /展开侧边栏|打开侧边栏|显示侧边栏|侧边栏/.test(label) && !/收起|隐藏|关闭/.test(label)
    })
    if (!toggle) return sidebarVisible(doc)
    await press(toggle)
    const deadline = Date.now() + (timeoutMs > 0 ? timeoutMs : 2000)
    while (Date.now() < deadline) {
      if (sidebarVisible(doc)) return true
      await sleep(40)
    }
    return sidebarVisible(doc)
  }

  function openMenus(doc) {
    return all(doc, '[role="menu"],[role="listbox"],[data-radix-menu-content]').filter(function (m) {
      if (!visible(m)) return false
      if (m.getAttribute && m.getAttribute('data-state') === 'closed') return false
      return true
    })
  }

  function findDeleteMenuItem(menu) {
    if (!menu) return null
    const items = all(menu, '[role="menuitem"],[role="option"],button,div,span').filter(function (el) {
      if (!enabled(el)) return false
      if ((el.tagName === 'DIV' || el.tagName === 'SPAN') && !el.getAttribute('role') && el.children && el.children.length > 2) return false
      const t = normalize(el.innerText)
      return /^(删除|删除对话|删除会话|删除记录)$/.test(t) || (t.indexOf('删除') >= 0 && t.length <= 10 && !/取消|清除/.test(t))
    })
    return items.find(function (el) { return /^(删除|删除对话|删除会话|删除记录)$/.test(normalize(el.innerText)) }) || items[0] || null
  }

  function findConfirmDialog(doc, oldDialogs) {
    const dialogs = all(doc, '[role="dialog"],[role="alertdialog"],[data-radix-dialog-content],.ant-modal,[class*="modal"]').filter(function (d) {
      if (!visible(d)) return false
      if (d.getAttribute && d.getAttribute('data-state') === 'closed') return false
      return !oldDialogs || !oldDialogs.has(d)
    })
    for (let i = dialogs.length - 1; i >= 0; i--) {
      const dlg = dialogs[i]
      const btns = all(dlg, 'button,[role="button"]').filter(function (b) {
        if (!enabled(b)) return false
        return /^(确认删除|删除|确定|确认)$/.test(normalize(b.innerText))
      })
      const hit = btns.find(function (b) { return /^(确认删除|删除)$/.test(normalize(b.innerText)) }) || btns[0]
      if (hit) return { dialog: dlg, button: hit }
    }
    return null
  }

  function closeMenus(doc) {
    try { (doc || document).dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })) } catch (e) {}
  }

  async function waitRadixClosed(doc, ms) {
    const deadline = Date.now() + (ms > 0 ? ms : 500)
    while (Date.now() < deadline) {
      if (!all(doc, '[role="menu"][data-state="closed"],[role="dialog"][data-state="closed"],[role="alertdialog"][data-state="closed"]').length) return
      await sleep(25)
    }
  }

  async function findItemWithScroll(doc, targetID) {
    let hit = conversationItems(doc).find(function (it) { return it.id === targetID }) || null
    if (hit) return hit
    const scroller = scrollerOf(doc)
    if (!scroller || typeof scroller.scrollHeight !== 'number' || typeof scroller.clientHeight !== 'number') return null
    const maxScroll = Math.max(0, scroller.scrollHeight - scroller.clientHeight)
    if (maxScroll <= 0) return null
    const step = Math.max(120, Math.floor(scroller.clientHeight * 0.7))
    scroller.scrollTop = 0
    await sleep(60)
    for (let pos = 0; pos <= maxScroll; pos += step) {
      scroller.scrollTop = pos
      await sleep(80)
      hit = conversationItems(doc).find(function (it) { return it.id === targetID }) || null
      if (hit) return hit
      if (pos === maxScroll) break
      if (pos + step > maxScroll) pos = maxScroll - step
    }
    return null
  }

  async function deleteConversation(doc, convID, cancelled, timeoutMs) {
    const blk = checkBlocked(doc)
    if (blk) return Object.assign({ ok: false }, blk)
    await ensureSidebarOpen(doc, 2000)
    const targetID = normalize(convID) || conversationIdFromHref(typeof location !== 'undefined' && location ? location.href : '')
    if (!targetID) return { ok: false, notFound: true, error: '当前页面没有可删除的对话 ID' }
    const deadline = Date.now() + (timeoutMs > 0 ? timeoutMs : 10000)
    await waitRadixClosed(doc, 400)

    const item = await findItemWithScroll(doc, targetID)
    if (!item) return { ok: false, notFound: true, error: '侧边栏未找到千问对话 ' + targetID }
    if (!item.moreBtn) return { ok: false, error: '未找到千问会话操作按钮' }

    await hover(item.el)
    await sleep(60)
    await hover(item.moreBtn)
    await sleep(30)
    const oldMenus = new Set(openMenus(doc))
    if (!await press(item.moreBtn)) return { ok: false, error: '无法点击千问会话更多按钮' }

    let delItem = null
    const menuUntil = Math.min(deadline, Date.now() + 2500)
    while (Date.now() < menuUntil) {
      const menus = openMenus(doc).filter(function (m) { return !oldMenus.has(m) })
      for (let i = menus.length - 1; i >= 0; i--) {
        delItem = findDeleteMenuItem(menus[i])
        if (delItem) break
      }
      if (delItem) break
      await sleep(30)
    }
    if (!delItem) {
      closeMenus(doc)
      return { ok: false, error: '千问菜单中未找到删除项' }
    }

    const oldDialogs = new Set(all(doc, '[role="dialog"],[role="alertdialog"],[data-radix-dialog-content],.ant-modal,[class*="modal"]').filter(visible))
    const fastCap = typeof window !== 'undefined' ? window.__tulaiHistoryFast : null
    if (fastCap && typeof fastCap.arm === 'function') fastCap.arm(targetID)
    try {
      if (!await press(delItem)) {
        closeMenus(doc)
        return { ok: false, error: '无法点击千问删除菜单项' }
      }

      let confirmed = false
      while (Date.now() < deadline) {
        if (cancelled && cancelled()) {
          closeMenus(doc)
          return { ok: false, stopped: 'cancelled', error: '清理已取消' }
        }
        const b = checkBlocked(doc)
        if (b) return Object.assign({ ok: false }, b)
        const dlg = findConfirmDialog(doc, oldDialogs)
        if (dlg && !confirmed) {
          confirmed = true
          if (!await press(dlg.button)) return { ok: false, error: '无法点击千问确认删除按钮' }
          await sleep(60)
          continue
        }
        const stillPresent = conversationItems(doc).some(function (it) { return it.id === targetID })
        const stillDialog = !!findConfirmDialog(doc, oldDialogs)
        if (!stillPresent && !stillDialog) {
          await waitRadixClosed(doc, 400)
          return { ok: true, id: targetID, title: item.title }
        }
        await sleep(40)
      }
      closeMenus(doc)
      return { ok: false, error: '删除千问对话超时未生效' }
    } finally {
      if (fastCap && typeof fastCap.disarm === 'function') fastCap.disarm()
    }
  }

  async function deleteNextHistoryItem(doc, skipIds, keepRecent, cancelled, timeoutMs) {
    const blk = checkBlocked(doc)
    if (blk) return Object.assign({ ok: false }, blk)
    await ensureSidebarOpen(doc, 2000)
    const skip = new Set(Array.isArray(skipIds) ? skipIds.map(String) : [])
    const keep = Math.max(0, Number(keepRecent) || 0)
    const scroller = scrollerOf(doc)
    if (keep === 0 && scroller && scroller.scrollTop > 0) {
      scroller.scrollTop = 0
      await sleep(80)
    }

    function pickTarget() {
      const items = conversationItems(doc)
      const candidates = items.slice(keep).filter(function (it) { return !skip.has(it.id) })
      return { items, target: candidates[0] || null }
    }

    let picked = pickTarget()
    if (!picked.target && scroller && typeof scroller.scrollHeight === 'number' && typeof scroller.clientHeight === 'number') {
      const beforeTop = scroller.scrollTop
      scroller.scrollTop = scroller.scrollHeight
      if (scroller.scrollTop !== beforeTop) {
        await sleep(450)
        picked = pickTarget()
      }
    }
    if (!picked.target) {
      return { ok: true, done: true, remaining: picked.items.length }
    }
    const res = await deleteConversation(doc, picked.target.id, cancelled, timeoutMs)
    const afterItems = conversationItems(doc)
    if (!res.ok) {
      return {
        ok: false,
        id: picked.target.id,
        title: picked.target.title,
        remaining: afterItems.length,
        stopped: res.stopped || '',
        error: res.error || '删除失败',
      }
    }
    return {
      ok: true,
      deleted: true,
      id: res.id || picked.target.id,
      title: res.title || picked.target.title,
      remaining: afterItems.length,
    }
  }

  async function collectHistoryIds(doc, skipIds, keepRecent, maxCount) {
    const blk = checkBlocked(doc)
    if (blk) return Object.assign({ ok: false, ids: [], keepIds: [], titles: {} }, blk)
    await ensureSidebarOpen(doc, 2000)
    const skip = new Set(Array.isArray(skipIds) ? skipIds.map(String) : [])
    const keep = Math.max(0, Number(keepRecent) || 0)
    const limit = Math.max(1, Number(maxCount) || 200)
    const scroller = scrollerOf(doc)
    if (scroller && scroller.scrollTop > 0) {
      scroller.scrollTop = 0
      await sleep(80)
    }

    const ordered = []
    const seen = new Set()
    const titles = {}
    function scan() {
      let added = 0
      const items = conversationItems(doc)
      for (let i = 0; i < items.length; i++) {
        const it = items[i]
        if (!seen.has(it.id)) {
          seen.add(it.id)
          ordered.push(it.id)
          titles[it.id] = it.title || ''
          added++
        }
      }
      return added
    }

    scan()
    if (scroller && ordered.length < keep + limit) {
      let idleSteps = 0
      const step = Math.max(160, Math.floor((scroller.clientHeight || 400) * 0.75))
      for (let round = 0; round < 40 && ordered.length < keep + limit; round++) {
        const prevTop = scroller.scrollTop
        const prevHeight = scroller.scrollHeight
        scroller.scrollTop = Math.min(scroller.scrollHeight, prevTop + step)
        const atBottom = scroller.scrollTop + scroller.clientHeight >= scroller.scrollHeight - 20
        await sleep(atBottom ? 350 : 120)
        const added = scan()
        if (added === 0 && scroller.scrollTop === prevTop && scroller.scrollHeight === prevHeight) {
          if (++idleSteps >= 2) break
        } else {
          idleSteps = 0
        }
      }
      scroller.scrollTop = 0
    }

    const keepIds = ordered.slice(0, keep)
    const keepSet = new Set(keepIds)
    const ids = ordered.filter(function (id) { return !keepSet.has(id) && !skip.has(id) }).slice(0, limit)
    return { ok: true, ids: ids, keepIds: keepIds, titles: titles, totalSeen: ordered.length }
  }

  const api = {
    conversationItems,
    ensureSidebarOpen,
    checkBlocked,
    deleteConversation,
    deleteNextHistoryItem,
    collectHistoryIds,
  }
  if (typeof window !== 'undefined') window.__tulaiQwenHistory = api
  if (typeof module !== 'undefined' && module.exports) module.exports = api
})()
