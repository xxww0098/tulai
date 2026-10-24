importScripts('protocol.js')

const XBS = globalThis.XBS
const ALARM = 'xbs-qianwen-reconnect'
const TAB_URLS = ['https://chat.qwen.ai/*', 'https://*.qwen.ai/*']

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
  if (!XBS.stubCommands.includes(name) && name !== XBS.commands.cancel) {
    sendToHost(XBS.fail(env, XBS.errors.E_PROTOCOL, '未知命令 ' + name))
    return
  }
  const tab = await pickTab()
  if (!tab) {
    sendToHost(XBS.fail(env, XBS.errors.E_NO_TAB, '没有 chat.qwen.ai 标签页'))
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

chrome.runtime.onMessage.addListener((msg, sender) => {
  if (!msg) return
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
  if (!tab.url || !/https:\/\/([^/]+\.)?qwen\.ai\//.test(tab.url)) return
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
