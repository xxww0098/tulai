importScripts('protocol.js')

const XBS = globalThis.XBS
const ALARM = 'xbs-qianwen-reconnect'
// 桌面端千问首页是 www.tongyi.com，ego.js 也认 qianwen.com。chat.qwen.ai 保留给国际站。
const TAB_URLS = [
  'https://www.tongyi.com/*',
  'https://*.tongyi.com/*',
  'https://tongyi.com/*',
  'https://www.qianwen.com/*',
  'https://*.qianwen.com/*',
  'https://qianwen.com/*',
  'https://chat.qwen.ai/*',
  'https://*.qwen.ai/*',
  'https://qwen.ai/*',
]

function isQianwenURL(url) {
  try {
    return /(^|\.)((tongyi|qianwen)\.com|qwen\.ai)$/.test(new URL(url).hostname)
  } catch (err) {
    return false
  }
}

let socket = null
let opened = false
let sessionId = ''
let retryMs = 1000
let reconnectTimer = null
let pingTimer = null
let connecting = false
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

function stopPing() {
  if (pingTimer) {
    clearInterval(pingTimer)
    pingTimer = null
  }
}

function startPing() {
  stopPing()
  pingTimer = setInterval(() => {
    if (!opened || !socket || socket.readyState !== WebSocket.OPEN) return
    socket.send(JSON.stringify(XBS.envelope('ping')))
  }, XBS.pingEveryMs)
}

function sendToHost(env) {
  if (!socket || socket.readyState !== WebSocket.OPEN) return false
  if (sessionId && !env.sessionId) env.sessionId = sessionId
  socket.send(JSON.stringify(env))
  return true
}

function remember(status, lastError) {
  return storageSet({ bridgeStatus: status, lastError: lastError || '' })
}

function scheduleReconnect() {
  if (reconnectTimer) return
  const wait = retryMs
  retryMs = Math.min(retryMs * 2, 15000)
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null
    connect()
  }, wait)
}

function reconnect() {
  const current = socket
  socket = null
  connecting = false
  opened = false
  sessionId = ''
  stopPing()
  if (current) {
    current.onclose = null
    current.close()
  }
  connect()
}

async function connect() {
  if (connecting || (socket && socket.readyState !== WebSocket.CLOSED)) return
  connecting = true
  const cfg = await storageGet()
  const port = Number(cfg.port) || 18765
  let ws
  try {
    ws = new WebSocket('ws://127.0.0.1:' + port + '/')
  } catch (err) {
    connecting = false
    setBadge('ERR', '#8c2f2f')
    await remember('error', String(err && err.message || err))
    scheduleReconnect()
    return
  }
  socket = ws
  connecting = false
  opened = false
  sessionId = ''
  ws.onopen = () => {
    ws.send(JSON.stringify(XBS.hello(cfg.token)))
  }
  ws.onmessage = (ev) => {
    if (typeof ev.data !== 'string') return
    onHostMessage(ws, ev.data)
  }
  ws.onerror = () => {}
  ws.onclose = () => {
    if (socket !== ws) return
    const wasOpen = opened
    socket = null
    opened = false
    sessionId = ''
    stopPing()
    if (wasOpen) {
      setBadge('OFF', '#666666')
      remember('off', '')
    } else {
      setBadge('ERR', '#8c2f2f')
    }
    scheduleReconnect()
  }
}

function onHostMessage(ws, data) {
  let env
  try {
    env = JSON.parse(data)
  } catch (err) {
    return
  }
  if (!opened) {
    if (env.type === 'error') {
      const code = env.payload && env.payload.code
      const message = (env.payload && env.payload.message) || code || 'hello 失败'
      setBadge('ERR', '#8c2f2f')
      remember('error', message)
      ws.close()
      return
    }
    if (env.type === 'ack' && env.payload && env.payload.name === XBS.commands.helloAck) {
      opened = true
      sessionId = env.sessionId || ''
      retryMs = 1000
      startPing()
      setBadge('ON', '#1b7f3a')
      remember('on', '')
    }
    return
  }
  if (env.type === 'ping') {
    sendToHost(XBS.reply(env, 'pong'))
    return
  }
  if (env.type === 'pong' || env.type === 'ack' || env.type === 'error') return
  if (env.type === 'cmd') {
    dispatchCommand(env)
  }
}

async function dispatchCommand(env) {
  const name = env.payload && env.payload.name
  if (name === XBS.commands.ping) {
    sendToHost(XBS.ack(env, { name, ok: true }))
    return
  }
  if (name === XBS.commands.status) {
    sendToHost(XBS.ack(env, { name, ok: true, tabs: await snapshotTabs() }))
    return
  }
  if (name === XBS.commands.cancel && !(await pickTab())) {
    sendToHost(XBS.ack(env, { name, ok: true, message: '没有进行中的页面任务' }))
    return
  }
  if (!XBS.pageCommands.includes(name) && name !== XBS.commands.cancel) {
    sendToHost(XBS.fail(env, XBS.errors.E_PROTOCOL, '未知命令 ' + name))
    return
  }
  const tab = await pickTab()
  if (!tab) {
    sendToHost(XBS.fail(env, XBS.errors.E_NO_TAB, '没有千问标签页（tongyi.com / qianwen.com / qwen.ai）'))
    return
  }
  markTab(tab.id, 'busy', env.taskId || '', tab.url || '')
  try {
    const response = await chrome.tabs.sendMessage(tab.id, {
      type: 'COMMAND_FROM_HOST',
      envelope: env,
    })
    if (!response) {
      sendToHost(XBS.fail(env, XBS.errors.E_SELECTOR_MISS, '内容脚本没有返回结果'))
      return
    }
    sendToHost(response)
  } catch (err) {
    sendToHost(XBS.fail(env, XBS.errors.E_NO_TAB, String(err && err.message || err)))
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

async function snapshotTabs() {
  const found = await chrome.tabs.query({ url: TAB_URLS })
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
  for (const [tabId, info] of tabs) {
    if (!seen.has(tabId)) tabs.delete(tabId)
    else if (info) info.url = info.url
  }
  return out
}

async function pickTab() {
  const found = await chrome.tabs.query({ url: TAB_URLS })
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
      if (bytes.length > 0 && bytes.length <= 3 * 1024 * 1024) return { bytesBase64: bytesToBase64(bytes) }
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
    reconnect()
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
  if (!isQianwenURL(tab.url || '')) return
  const current = tabs.get(tabId)
  markTab(tabId, current && current.state === 'busy' ? 'busy' : 'idle', current ? current.taskId : '', tab.url)
})

function ensureAlarm() {
  chrome.alarms.create(ALARM, { periodInMinutes: 0.5 })
}

chrome.runtime.onInstalled.addListener(() => {
  ensureAlarm()
  connect()
})

chrome.runtime.onStartup.addListener(() => {
  ensureAlarm()
  connect()
})

chrome.alarms.onAlarm.addListener((alarm) => {
  if (alarm.name === ALARM) connect()
})

chrome.storage.onChanged.addListener((changes, area) => {
  if (area !== 'local') return
  if (changes.token || changes.port) reconnect()
})

ensureAlarm()
connect()
