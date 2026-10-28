// 纯函数：重连退避、心跳超时、端口/令牌对不上时给用户看的话。
// background.js 用 importScripts 引入。节点测试直接读这个文件。
globalThis.TULAIBridge = {
  retryMinMs: 1000,
  retryMaxMs: 15000,
  nextRetryMs(current) {
    const base = Number(current) > 0 ? Number(current) : 1000
    return Math.min(base * 2, 15000)
  },
  pingTimedOut(lastPongAt, awaiting, now, everyMs) {
    if (!awaiting) return false
    const every = Number(everyMs) > 0 ? Number(everyMs) : 15000
    return now - lastPongAt > every * 2
  },
  refusedMessage(port) {
    return '连不上 127.0.0.1:' + port + '。确认 tulai 已打开，扩展的端口和令牌与桌面端「浏览器」页一致，然后点「保存并重连」。不用重启 tulai。'
  },
  authMessage(serverMessage) {
    const msg = String(serverMessage || '').trim()
    if (msg) return msg
    return '令牌或端口不一致。在 tulai「浏览器」页核对令牌和端口，填进扩展弹出页后点「保存并重连」。'
  },
}
