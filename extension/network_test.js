const assert = require('assert')
const fs = require('fs')
const path = require('path')
const vm = require('vm')

async function main() {
  let onEvent
  let onDetach
  let body = ''
  let failAttach = false
  let blockAttach = null
  let attachEntered = null
  const attached = new Set()
  const calls = []
  const chrome = {
    debugger: {
      onEvent: { addListener(fn) { onEvent = fn } },
      onDetach: { addListener(fn) { onDetach = fn } },
      async attach({ tabId }) {
        if (failAttach) throw new Error('debugger unavailable')
        if (blockAttach) {
          const gate = blockAttach
          if (attachEntered) attachEntered()
          await gate
        }
        attached.add(tabId)
      },
      async detach({ tabId }) { attached.delete(tabId); onDetach({ tabId }) },
      async sendCommand({ tabId }, method) {
        calls.push([tabId, method])
        if (method === 'Network.getResponseBody') return { body, base64Encoded: false }
        return {}
      },
    },
  }
  const context = vm.createContext({ chrome, URL, setTimeout, clearTimeout })
  context.globalThis = context
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'network.js'), 'utf8'), context)
  const network = context.TULAINetwork
  const first = []
  const second = []
  assert.strictEqual(await network.start(11, 'doubao', 'task-1', (value) => first.push(value)), true)
  assert.strictEqual(await network.start(22, 'qianwen', 'task-2', (value) => second.push(value)), true)
  network.mark(11, 'wrong-task')
  await onEvent({ tabId: 11 }, 'Network.requestWillBeSent', { requestId: 'before', request: { method: 'POST', url: 'https://www.doubao.com/chat/completion' } })
  network.mark(11, 'task-1')
  network.mark(22, 'task-2')
  await onEvent({ tabId: 11 }, 'Network.requestWillBeSent', { requestId: 'noise', request: { method: 'POST', url: 'https://evil.example/chat/completion' } })
  await onEvent({ tabId: 11 }, 'Network.requestWillBeSent', { requestId: 'real', request: { method: 'POST', url: 'https://www.doubao.com/samantha/chat/completion?token=secret' } })
  await onEvent({ tabId: 22 }, 'Network.requestWillBeSent', { requestId: 'qwen', request: { method: 'POST', url: 'https://chat2.qianwen.com/api/v2/chat?session=secret' } })
  await onEvent({ tabId: 11 }, 'Network.responseReceived', { requestId: 'before', response: { status: 401 } })
  await onEvent({ tabId: 11 }, 'Network.responseReceived', { requestId: 'noise', response: { status: 403 } })
  assert.strictEqual(first.length, 0, '只观察标记后的目标请求')
  await onEvent({ tabId: 11 }, 'Network.responseReceived', { requestId: 'real', response: { status: 200 } })
  await onEvent({ tabId: 22 }, 'Network.responseReceived', { requestId: 'qwen', response: { status: 429 } })
  body = JSON.stringify({ code: 0, message: '今日免费次数已用完', prompt: 'private prompt', token: 'secret' })
  await onEvent({ tabId: 11 }, 'Network.loadingFinished', { requestId: 'real', encodedDataLength: 100 })
  assert.deepStrictEqual(JSON.parse(JSON.stringify(first)), [{ status: 200 }, { status: 200, code: '0', kind: '' }])
  body = JSON.stringify({ error_code: 42901, error_msg: '请求过于频繁，请稍后再试', token: 'secret' })
  await onEvent({ tabId: 22 }, 'Network.loadingFinished', { requestId: 'qwen', encodedDataLength: 100 })
  assert.deepStrictEqual(JSON.parse(JSON.stringify(second)), [{ status: 429 }, { status: 429, code: '42901', kind: 'rate' }])
  assert.ok(!JSON.stringify([first, second]).includes('secret'), '观察结果不得包含网址、正文或凭证')
  assert.ok(!JSON.stringify(first).includes('private prompt'))
  await network.stop(11, 'wrong-task')
  assert.ok(attached.has(11), '其他任务不能结束本次观察')
  await network.stop(11, 'task-1')
  await network.stop(22, 'task-2')
  assert.strictEqual(attached.size, 0)

  const quota = []
  await network.start(33, 'doubao', 'task-3', (value) => quota.push(value))
  network.mark(33, 'task-3')
  await onEvent({ tabId: 33 }, 'Network.requestWillBeSent', { requestId: 'quota', request: { method: 'POST', url: 'https://www.doubao.com/chat/completion' } })
  await onEvent({ tabId: 33 }, 'Network.responseReceived', { requestId: 'quota', response: { status: 200 } })
  body = 'data: ' + JSON.stringify({ event_type: 2005, event_data: JSON.stringify({ error_code: 12345, error_msg: '今日免费次数已用完', token: 'secret' }) }) + '\n\n'
  await onEvent({ tabId: 33 }, 'Network.loadingFinished', { requestId: 'quota', encodedDataLength: 100 })
  assert.strictEqual(quota[1].kind, 'quota')
  assert.strictEqual(quota[1].code, '12345')
  assert.ok(!JSON.stringify(quota).includes('secret'))
  await network.stop(33, 'task-3')

  const unknown = []
  await network.start(44, 'qianwen', 'task-4', (value) => unknown.push(value))
  network.mark(44, 'task-4')
  await onEvent({ tabId: 44 }, 'Network.requestWillBeSent', { requestId: 'unknown', request: { method: 'POST', url: 'https://chat.qwen.ai/api/v2/chat/completions' } })
  await onEvent({ tabId: 44 }, 'Network.responseReceived', { requestId: 'unknown', response: { status: 429 } })
  body = JSON.stringify({ code: 'QuotaExceeded', message: 'The request has exceeded the quota.' })
  await onEvent({ tabId: 44 }, 'Network.loadingFinished', { requestId: 'unknown', encodedDataLength: 100 })
  assert.strictEqual(unknown[1].kind, '', '含义不明的 quota 不能当成每日额度耗尽')
  body = JSON.stringify({ error_code: 123, message: '免费次数已重置' })
  await onEvent({ tabId: 44 }, 'Network.loadingFinished', { requestId: 'unknown', encodedDataLength: 100 })
  assert.strictEqual(unknown[2].kind, '', '额度重置不能误判为用尽')
  body = JSON.stringify({ code: 'secret-token', message: 'unknown' })
  await onEvent({ tabId: 44 }, 'Network.loadingFinished', { requestId: 'unknown', encodedDataLength: 100 })
  assert.strictEqual(unknown.length, 3, '未知文本码可能包含敏感值，不得记录')
  await network.stop(44, 'task-4')

  const unavailable = []
  failAttach = true
  assert.strictEqual(await network.start(55, 'doubao', 'task-5', (value) => unavailable.push(value)), false)
  assert.strictEqual(unavailable[0].unavailable, true)
  await network.stop(55, 'task-5')
  failAttach = false

  let releaseAttach
  blockAttach = new Promise((resolve) => { releaseAttach = resolve })
  const entered = new Promise((resolve) => { attachEntered = resolve })
  const oldStart = network.start(66, 'doubao', 'old', () => {})
  await entered
  const newEvents = []
  const newStart = network.start(66, 'doubao', 'new', (value) => newEvents.push(value))
  blockAttach = null
  releaseAttach()
  await Promise.all([oldStart, newStart])
  assert.ok(attached.has(66), '旧任务结束不能断开新任务的调试连接')
  network.mark(66, 'new')
  await onEvent({ tabId: 66 }, 'Network.requestWillBeSent', { requestId: 'new', request: { method: 'POST', url: 'https://www.doubao.com/chat/completion' } })
  await onEvent({ tabId: 66 }, 'Network.responseReceived', { requestId: 'new', response: { status: 200 } })
  assert.strictEqual(newEvents[0].status, 200)
  await network.stop(66, 'new')
  assert.ok(calls.some((entry) => entry[1] === 'Network.enable'))
  console.log('network test ok')
}

main().catch((err) => {
  console.error(err)
  process.exit(1)
})
