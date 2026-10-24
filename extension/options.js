const tokenInput = document.querySelector('#token')
const portInput = document.querySelector('#port')
const status = document.querySelector('#status')

function render(stored) {
  tokenInput.value = stored.token || ''
  portInput.value = String(stored.port || 18765)
  const lines = ['状态: ' + (stored.bridgeStatus || 'off')]
  if (stored.lastError) lines.push('最近一次错误: ' + stored.lastError)
  status.textContent = lines.join('\n')
}

chrome.storage.local.get({ token: '', port: 18765, lastError: '', bridgeStatus: 'off' }).then(render)
chrome.storage.onChanged.addListener((changes, area) => {
  if (area !== 'local') return
  chrome.storage.local.get({ token: '', port: 18765, lastError: '', bridgeStatus: 'off' }).then(render)
})

document.querySelector('#form').addEventListener('submit', (event) => {
  event.preventDefault()
  const port = Number(portInput.value)
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    status.textContent = '端口需要是 1 到 65535 的整数'
    return
  }
  chrome.storage.local.set({ token: tokenInput.value, port }).then(() => {
    chrome.runtime.sendMessage({ type: 'RECONNECT' })
  })
})
