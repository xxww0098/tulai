const assert = require('assert')

// 模拟站点自带的 secsdk 签名包装器（在 history_fast 安装前已挂在 window.fetch 上）
const signedCalls = []
global.window = global
global.location = { href: 'https://www.doubao.com/chat/' }
global.window.fetch = async function secsdkFetch(url, opts) {
  const u = String(url)
  signedCalls.push({ url: u, method: opts && opts.method, body: opts && opts.body, headers: opts && opts.headers })
  // 模拟：如果 URL 里带有固定旧 a_bogus=STALE 则拒绝；无 a_bogus 时 secsdk 自动补签成功
  if (u.includes('a_bogus=STALE')) {
    return { status: 403, text: async () => JSON.stringify({ code: 403, msg: 'bad sign' }), clone() { return this } }
  }
  if (u.includes('/log/track')) {
    return { status: 200, text: async () => '{"ok":true}', clone() { return this } }
  }
  if (u.includes('/samantha/thread/delete')) {
    const parsed = JSON.parse(opts.body)
    if (parsed.conversation_id === 'rate-hit') {
      return { status: 429, text: async () => 'too many requests', clone() { return this } }
    }
    if (parsed.conversation_id === 'biz-err') {
      return { status: 200, text: async () => JSON.stringify({ code: 50001, msg: 'fail' }), clone() { return this } }
    }
    return {
      status: 200,
      text: async () => JSON.stringify({ code: 0, msg: 'ok' }),
      clone() { return { status: 200, text: async () => JSON.stringify({ code: 0, msg: 'ok' }) } },
    }
  }
  return { status: 404, text: async () => 'not found', clone() { return this } }
}

const Fast = require('./history_fast.js')

async function run() {
  // 1) 捕获阶段：同时发出埋点请求和真实删除请求，应忽略埋点并捕获删除模板
  Fast.arm('conv-001')
  await window.fetch('https://www.doubao.com/log/track?id=conv-001', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ event: 'click', id: 'conv-001' }),
  })
  await window.fetch('https://www.doubao.com/samantha/thread/delete?version=1', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': 'tok-abc' },
    body: JSON.stringify({ conversation_id: 'conv-001' }),
  })
  Fast.disarm()

  const tpl = await Fast.takeTemplate(200)
  assert.ok(tpl, '应成功捕获删除模板')
  assert.strictEqual(tpl.transport, 'fetch')
  assert.strictEqual(tpl.method, 'POST')
  assert.strictEqual(tpl.bodyKind, 'json')
  assert.strictEqual(tpl.headers['x-csrf-token'], 'tok-abc')
  assert.strictEqual(tpl.bodyTemplate, '{"conversation_id":"__TULAI_CONV_ID__"}')
  assert.strictEqual(tpl.successSig.codeField, 'code')
  assert.strictEqual(tpl.successSig.codeValue, 0)

  // 2) 单条回放 + 批量并发回放（经过 secsdkFetch）
  const beforeLen = signedCalls.length
  const p1 = await Fast.replayOne(tpl, 'conv-002')
  assert.strictEqual(p1.ok, true, 'replayOne 应成功: ' + JSON.stringify(p1))
  assert.strictEqual(signedCalls.length, beforeLen + 1, '回放必须经过站点 secsdk 包装器')
  assert.strictEqual(signedCalls[signedCalls.length - 1].body, '{"conversation_id":"conv-002"}')

  const wave = await Fast.replayWave(tpl, ['conv-003', 'conv-004', 'conv-005', 'conv-006'], 3)
  assert.strictEqual(wave.ok, true, JSON.stringify(wave))
  assert.strictEqual(wave.deletedIds.length, 4)

  // 3) 业务错误码检测（code != 0 必须识别为失败）
  const bizErr = await Fast.replayOne(tpl, 'biz-err')
  assert.strictEqual(bizErr.ok, false)

  // 4) 限流 429 必须返回 stopped=rate_limit
  const rate = await Fast.replayOne(tpl, 'rate-hit')
  assert.strictEqual(rate.ok, false)
  assert.strictEqual(rate.stopped, 'rate_limit')

  // 5) 瞬态签名参数自动剥离重试
  const staleTpl = Object.assign({}, tpl, {
    urlTemplate: 'https://www.doubao.com/samantha/thread/delete?version=1&a_bogus=STALE',
  })
  const retryRes = await Fast.replayOne(staleTpl, 'conv-007')
  assert.strictEqual(retryRes.ok, true, '应自动剥离旧 a_bogus 后重试成功: ' + JSON.stringify(retryRes))
  assert.strictEqual(retryRes.stripSign, true)

  console.log('history_fast ok')
}

run().catch(err => { console.error(err); process.exitCode = 1 })
