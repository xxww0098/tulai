const tokenInput = document.querySelector('#token')
const portInput = document.querySelector('#port')
const status = document.querySelector('#status')

const defaults = { token: '', port: 18765, lastError: '', bridgeStatus: 'off', qianwen: 'off', doubao: 'off', pairMode: 'auto', pairState: '' }
const statusLabel = { on: '已连接', off: '未连接', error: '连接失败', reconnecting: '正在重连' }
const platformLabel = { on: '已连接', off: '未连接' }

function pairLine(stored) {
  if (stored.pairMode === 'manual' || stored.pairState === 'need-manual') return '需要手动配对'
  if (stored.pairState === 'paired') return '已自动对接'
  return '需要手动配对'
}

function showStatus(stored) {
  const raw = stored.bridgeStatus || 'off'
  const label = Object.prototype.hasOwnProperty.call(statusLabel, raw) ? statusLabel[raw] : raw
  const qianwen = platformLabel[stored.qianwen] || '未连接'
  const doubao = platformLabel[stored.doubao] || '未连接'
  const lines = [pairLine(stored), '状态: ' + label, '千问: ' + qianwen, '豆包: ' + doubao]
  if (stored.lastError) lines.push(stored.lastError)
  status.textContent = lines.join('\n')
  status.dataset.state = raw === 'reconnecting' ? 'error' : raw
}

function showFields(stored) {
  tokenInput.value = stored.token || ''
  portInput.value = String(stored.port || 18765)
}

chrome.storage.local.get(defaults).then((stored) => {
  showFields(stored)
  showStatus(stored)
})

chrome.storage.onChanged.addListener((changes, area) => {
  if (area !== 'local') return
  if (Object.prototype.hasOwnProperty.call(changes, 'token')) {
    tokenInput.value = changes.token.newValue || ''
  }
  if (Object.prototype.hasOwnProperty.call(changes, 'port')) {
    portInput.value = String(changes.port.newValue || 18765)
  }
  if (changes.bridgeStatus || changes.lastError || changes.qianwen || changes.doubao || changes.pairState || changes.pairMode) {
    chrome.storage.local.get(defaults).then(showStatus)
  }
})

document.querySelector('#form').addEventListener('submit', (event) => {
  event.preventDefault()
  const port = Number(portInput.value)
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    status.textContent = '端口需要是 1 到 65535 的整数'
    delete status.dataset.state
    return
  }
  chrome.storage.local.set({ token: tokenInput.value, port, pairMode: 'manual', pairState: 'need-manual' }).then(() => {
    const sent = chrome.runtime.sendMessage({ type: 'RECONNECT' })
    if (sent && typeof sent.catch === 'function') sent.catch(() => {})
  })
})

const repair = document.querySelector('#repair')
if (repair) {
  repair.addEventListener('click', () => {
    chrome.storage.local.set({ pairMode: 'auto', pairState: '' }).then(() => {
      const sent = chrome.runtime.sendMessage({ type: 'RECONNECT' })
      if (sent && typeof sent.catch === 'function') sent.catch(() => {})
    })
  })
}
