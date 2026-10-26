importScripts('protocol.js', 'bridge_logic.js')

const XBS = globalThis.XBS
const ALARM = 'xbs-bridge-reconnect'
// 一条 WebSocket hello 只登记一个 platform。千问和豆包各开一条连接，命令只发给对应站点的标签页。
const SITES = {
  qianwen: {
    urls: [
      'https://www.tongyi.com/*',
      'https://*.tongyi.com/*',
      'https://tongyi.com/*',
      'https://www.qianwen.com/*',
      'https://*.qianwen.com/*',
      'https://qianwen.com/*',
      'https://chat.qwen.ai/*',
      'https://*.qwen.ai/*',
      'https://qwen.ai/*',
    ],
    noTab: '没有千问标签页（tongyi.com / qianwen.com / qwen.ai）',
  },
  doubao: {
    urls: [
      'https://www.doubao.com/*',
      'https://*.doubao.com/*',
      'https://doubao.com/*',
    ],
    noTab: '没有豆包标签页（doubao.com）',
  },
}

function isQianwenURL(url) {
  try {
    return /(^|\.)((tongyi|qianwen)\.com|qwen\.ai)$/.test(new URL(url).hostname)
  } catch (err) {
    return false
  }
}

function isDoubaoURL(url) {
  try {
    return /(^|\.)doubao\.com$/.test(new URL(url).hostname)
  } catch (err) {
    return false
  }
}

function siteOf(url) {
  if (isQianwenURL(url)) return 'qianwen'
  if (isDoubaoURL(url)) return 'doubao'
  return ''
}

function newLink(platform) {
  return {
    platform,
    socket: null,
    opened: false,
    sessionId: '',
    retryMs: XBSBridge.retryMinMs,
    reconnectTimer: null,
    pingTimer: null,
    connecting: false,
    attempt: 0,
    authError: '',
    linkError: '',
    pingAwaiting: false,
    lastPongAt: 0,
    triedPort: 18765,
  }
}

const links = {
  qianwen: newLink('qianwen'),
  doubao: newLink('doubao'),
}
const tabs = new Map()

function storageGet() {
  return chrome.storage.local.get({ token: '', port: 18765, lastError: '', bridgeStatus: 'off' })
}

function storageSet(values) {
  return chrome.storage.local.set(values)
}

function setBadge(text, color) {
  chrome.action.setBadgeText({ text }).catch(() => {})
  chrome.action.setBadgeBackgroundColor({ color }).catch(() => {})
}

function refreshBadge() {
  const all = Object.values(links)
  const openCount = all.filter((link) => link.opened).length
  const problem = all.map((link) => link.authError || link.linkError).find(Boolean) || ''
  const platforms = {
    qianwen: links.qianwen.opened ? 'on' : 'off',
    doubao: links.doubao.opened ? 'on' : 'off',
  }
  if (openCount === all.length) {
    setBadge('ON', '#1b7f3a')
    remember('on', '', platforms)
    return
  }
  if (problem) {
    setBadge(openCount ? '…' : 'ERR', openCount ? '#8a6d1d' : '#8c2f2f')
    remember(openCount ? 'reconnecting' : 'error', problem, platforms)
    return
  }
  if (all.some((link) => link.connecting || link.reconnectTimer)) {
    setBadge('…', '#8a6d1d')
    remember('reconnecting', openCount ? '有一条连接已断开，正在重连' : '正在连接 tulai', platforms)
    return
  }
  setBadge('OFF', '#666666')
  remember('off', '', platforms)
}

function stopPing(link) {
  if (link.pingTimer) {
    clearInterval(link.pingTimer)
    link.pingTimer = null
  }
}

function startPing(link) {
  stopPing(link)
  link.pingAwaiting = false
  link.lastPongAt = Date.now()
  link.pingTimer = setInterval(() => {
    if (!link.opened || !link.socket || link.socket.readyState !== WebSocket.OPEN) return
    if (XBSBridge.pingTimedOut(link.lastPongAt, link.pingAwaiting, Date.now(), XBS.pingEveryMs)) {
      link.linkError = '心跳没有回应，正在重连。不用重启 tulai。'
      link.authError = ''
      reconnect(link)
      return
    }
    link.pingAwaiting = true
    const env = XBS.envelope('ping')
    env.platform = link.platform
    try {
      link.socket.send(JSON.stringify(env))
    } catch (err) {
      link.linkError = '发送心跳失败，正在重连。'
      reconnect(link)
    }
  }, XBS.pingEveryMs)
}

function sendToHost(link, env) {
  if (!link.socket || link.socket.readyState !== WebSocket.OPEN) return false
  if (link.sessionId && !env.sessionId) env.sessionId = link.sessionId
  if (!env.platform) env.platform = link.platform
  link.socket.send(JSON.stringify(env))
  return true
}

function remember(status, lastError, platforms) {
  return storageSet(Object.assign({
    bridgeStatus: status,
    lastError: lastError || '',
  }, platforms || {}))
}

function scheduleReconnect(link) {
  if (link.reconnectTimer) return
  const wait = link.retryMs
  link.retryMs = XBSBridge.nextRetryMs(link.retryMs)
  link.reconnectTimer = setTimeout(() => {
    link.reconnectTimer = null
    connect(link)
  }, wait)
  refreshBadge()
}

function reconnect(link) {
  if (link.reconnectTimer) {
    clearTimeout(link.reconnectTimer)
    link.reconnectTimer = null
  }
  const current = link.socket
  link.attempt += 1
  link.socket = null
  link.connecting = false
  link.opened = false
  link.sessionId = ''
  link.pingAwaiting = false
  stopPing(link)
  if (current) {
    current.onclose = null
    current.onerror = null
    current.onmessage = null
    current.onopen = null
    try { current.close() } catch (err) {}
  }
  connect(link)
}

function reconnectAll() {
  Object.values(links).forEach(reconnect)
}

async function connect(link) {
  if (link.connecting || (link.socket && link.socket.readyState !== WebSocket.CLOSED)) return
  link.connecting = true
  link.attempt += 1
  const attempt = link.attempt
  const cfg = await storageGet()
  if (link.attempt !== attempt) return
  const port = Number(cfg.port) || 18765
  link.triedPort = port
  let ws
  try {
    ws = new WebSocket('ws://127.0.0.1:' + port + '/')
  } catch (err) {
    link.connecting = false
    if (!link.authError) link.linkError = XBSBridge.refusedMessage(port)
    refreshBadge()
    scheduleReconnect(link)
    return
  }
  link.socket = ws
  link.connecting = false
  link.opened = false
  link.sessionId = ''
  const openTimer = setTimeout(() => {
    if (link.socket === ws && !link.opened) {
      if (!link.authError) link.linkError = XBSBridge.refusedMessage(port)
      try { ws.close() } catch (err) {}
    }
  }, 8000)
  ws.onopen = () => {
    clearTimeout(openTimer)
    try {
      ws.send(JSON.stringify(XBS.hello(cfg.token, link.platform)))
    } catch (err) {
      link.linkError = XBSBridge.refusedMessage(port)
      try { ws.close() } catch (e) {}
    }
  }
  ws.onmessage = (ev) => {
    if (typeof ev.data !== 'string') return
    onHostMessage(link, ws, ev.data)
  }
  ws.onerror = () => {
    if (!link.authError) link.linkError = XBSBridge.refusedMessage(port)
  }
  ws.onclose = () => {
    clearTimeout(openTimer)
    if (link.socket !== ws) return
    const wasOpen = link.opened
    link.socket = null
    link.opened = false
    link.sessionId = ''
    link.pingAwaiting = false
    stopPing(link)
    if (wasOpen) {
      link.authError = ''
      if (!link.linkError) link.linkError = '连接已断开，正在重连。不用重启 tulai。'
    } else if (!link.authError && !link.linkError) {
      link.linkError = XBSBridge.refusedMessage(port)
    }
    refreshBadge()
    scheduleReconnect(link)
  }
}

function connectAll() {
  Object.values(links).forEach((link) => {
    const open = link.opened && link.socket && link.socket.readyState === WebSocket.OPEN
    if (open && XBSBridge.pingTimedOut(link.lastPongAt, link.pingAwaiting, Date.now(), XBS.pingEveryMs)) {
      link.linkError = '心跳没有回应，正在重连。不用重启 tulai。'
      reconnect(link)
      return
    }
    if (open) return
    if (link.socket && link.socket.readyState === WebSocket.CONNECTING) return
    connect(link)
  })
}

function onHostMessage(link, ws, data) {
  let env
  try {
    env = JSON.parse(data)
  } catch (err) {
    return
  }
  if (!link.opened) {
    if (env.type === 'error') {
      const message = (env.payload && env.payload.message) || (env.payload && env.payload.code) || 'hello 失败'
      link.authError = XBSBridge.authMessage(message)
      link.linkError = ''
      refreshBadge()
      ws.close()
      return
    }
    if (env.type === 'ack' && env.payload && env.payload.name === XBS.commands.helloAck) {
      link.opened = true
      link.authError = ''
      link.linkError = ''
      link.sessionId = env.sessionId || ''
      link.retryMs = XBSBridge.retryMinMs
      link.pingAwaiting = false
      link.lastPongAt = Date.now()
      startPing(link)
      refreshBadge()
    }
    return
  }
  if (env.type === 'ping') {
    sendToHost(link, XBS.reply(env, 'pong'))
    return
  }
  if (env.type === 'pong') {
    link.pingAwaiting = false
    link.lastPongAt = Date.now()
    if (link.linkError && link.linkError.indexOf('心跳') === 0) link.linkError = ''
    return
  }
  if (env.type === 'ack' || env.type === 'error') return
  if (env.type === 'cmd') {
    dispatchCommand(link, env)
  }
}

async function dispatchCommand(link, env) {
  const name = env.payload && env.payload.name
  if (name === XBS.commands.ping) {
    sendToHost(link, XBS.ack(env, { name, ok: true }))
    return
  }
  if (name === XBS.commands.status) {
    sendToHost(link, XBS.ack(env, { name, ok: true, tabs: await snapshotTabs(link.platform) }))
    return
  }
  if (name === XBS.commands.cancel && !(await pickTab(link.platform))) {
    sendToHost(link, XBS.ack(env, { name, ok: true, message: '没有进行中的页面任务' }))
    return
  }
  if (!XBS.pageCommands.includes(name) && name !== XBS.commands.cancel) {
    sendToHost(link, XBS.fail(env, XBS.errors.E_PROTOCOL, '未知命令 ' + name))
    return
  }
  const tab = await pickTab(link.platform)
  if (!tab) {
    sendToHost(link, XBS.fail(env, XBS.errors.E_NO_TAB, SITES[link.platform].noTab))
    return
  }
  // 豆包在后台标签时，合成 pointer/click 常常点了也不发送（E_SEND_UNCERTAIN）。
  // 下发页内命令前先把标签页拉到前台。
  await focusTab(tab)
  markTab(tab.id, 'busy', env.taskId || '', tab.url || '')
  try {
    const response = await chrome.tabs.sendMessage(tab.id, {
      type: 'COMMAND_FROM_HOST',
      envelope: env,
    })
    if (!response) {
      sendToHost(link, XBS.fail(env, XBS.errors.E_SELECTOR_MISS, '内容脚本没有返回结果'))
      return
    }
    sendToHost(link, response)
  } catch (err) {
    sendToHost(link, XBS.fail(env, XBS.errors.E_NO_TAB, String(err && err.message || err)))
  } finally {
    markTab(tab.id, 'idle', '', tab.url || '')
  }
}

function markTab(tabId, state, taskId, url) {
  const current = tabs.get(tabId) || {}
  tabs.set(tabId, {
    tabId,
    state,
    taskId: taskId || '',
    url: url || current.url || '',
    updatedAt: Date.now(),
  })
}

async function snapshotTabs(platform) {
  const found = await chrome.tabs.query({ url: SITES[platform].urls })
  const seen = new Set()
  const out = []
  for (const tab of found) {
    seen.add(tab.id)
    const known = tabs.get(tab.id)
    out.push({
      tabId: tab.id,
      state: known && known.state === 'busy' ? 'busy' : 'idle',
      url: tab.url || '',
      taskId: known ? known.taskId || '' : '',
    })
  }
  for (const [tabId] of tabs) {
    if (!seen.has(tabId) && siteOf((tabs.get(tabId) || {}).url) === platform) tabs.delete(tabId)
  }
  return out
}

async function focusTab(tab) {
  if (!tab || tab.id == null) return
  try {
    await chrome.tabs.update(tab.id, { active: true })
  } catch (e) {}
  if (tab.windowId != null) {
    try {
      await chrome.windows.update(tab.windowId, { focused: true })
    } catch (e) {}
  }
}

async function pickTab(platform) {
  const found = await chrome.tabs.query({ url: SITES[platform].urls })
  const idle = found.find((tab) => {
    const info = tabs.get(tab.id)
    return !info || info.state !== 'busy'
  })
  return idle || found[0] || null
}

function bytesToBase64(bytes) {
  let binary = ''
  const chunk = 0x8000
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode.apply(null, bytes.subarray(i, i + chunk))
  }
  return btoa(binary)
}

// 页面 fetch 被跨域拦住时，后台再用扩展的 cookie 拉一次。太大就不走消息，改让浏览器下载并回传路径。
async function fetchOrDownload(url) {
  try {
    const res = await fetch(url, { credentials: 'include' })
    if (res.ok) {
      const bytes = new Uint8Array(await res.arrayBuffer())
      if (!XBS.looksLikeRaster(bytes)) return { error: '下载内容不是 PNG、JPEG、WebP 或 GIF' }
      if (bytes.length <= 3 * 1024 * 1024) return { bytesBase64: bytesToBase64(bytes) }
    }
  } catch (err) {}
  try {
    const saved = await downloadToFile(url)
    if (saved) return { path: saved }
  } catch (err) {
    return { error: String(err && err.message || err) }
  }
  return { error: '下载没有得到文件' }
}

function downloadToFile(url) {
  return new Promise((resolve, reject) => {
    chrome.downloads.download({ url, saveAs: false, conflictAction: 'uniquify' }, (id) => {
      if (chrome.runtime.lastError || id == null) {
        reject(new Error((chrome.runtime.lastError && chrome.runtime.lastError.message) || 'download failed'))
        return
      }
      const timer = setTimeout(() => {
        chrome.downloads.onChanged.removeListener(onChange)
        reject(new Error('download timeout'))
      }, 60000)
      function onChange(delta) {
        if (!delta || delta.id !== id) return
        if (delta.state && delta.state.current === 'complete') {
          clearTimeout(timer)
          chrome.downloads.onChanged.removeListener(onChange)
          chrome.downloads.search({ id }, (items) => {
            resolve((items && items[0] && items[0].filename) || '')
          })
        } else if (delta.error) {
          clearTimeout(timer)
          chrome.downloads.onChanged.removeListener(onChange)
          reject(new Error(delta.error.current || 'download failed'))
        }
      }
      chrome.downloads.onChanged.addListener(onChange)
    })
  })
}

chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => {
  if (!msg) return
  if (msg.type === 'FETCH_URL') {
    fetchOrDownload(msg.url).then(sendResponse).catch((err) => {
      sendResponse({ error: String(err && err.message || err) })
    })
    return true
  }
  if (msg.type === 'RECONNECT') {
    reconnectAll()
    return
  }
  if (msg.type === 'TAB_READY' && sender.tab) {
    markTab(sender.tab.id, 'idle', '', sender.tab.url || msg.href || '')
  }
})

chrome.tabs.onRemoved.addListener((tabId) => {
  tabs.delete(tabId)
})

chrome.tabs.onUpdated.addListener((tabId, _info, tab) => {
  if (!siteOf(tab.url || '')) return
  const current = tabs.get(tabId)
  markTab(tabId, current && current.state === 'busy' ? 'busy' : 'idle', current ? current.taskId : '', tab.url)
})

function ensureAlarm() {
  chrome.alarms.create(ALARM, { periodInMinutes: 0.5 })
}

chrome.runtime.onInstalled.addListener(() => {
  ensureAlarm()
  connectAll()
})

chrome.runtime.onStartup.addListener(() => {
  ensureAlarm()
  connectAll()
})

chrome.alarms.onAlarm.addListener((alarm) => {
  if (alarm.name === ALARM) connectAll()
})

chrome.storage.onChanged.addListener((changes, area) => {
  if (area !== 'local') return
  if (changes.token || changes.port) reconnectAll()
})

ensureAlarm()
connectAll()
