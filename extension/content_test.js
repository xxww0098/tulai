// 内容脚本的等待循环：千问先滚再取样，豆包没有 revealResults 时取样逻辑不变。
const assert = require('assert')
const fs = require('fs')
const path = require('path')
const vm = require('vm')

const GRAY = 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7'

function load(page, where) {
  const listeners = []
  const context = vm.createContext({
    console,
    setTimeout,
    clearTimeout,
    URL,
    File: class File { constructor(parts, name) { this.name = name } },
    Uint8Array,
    atob,
    btoa,
    location: where || { href: 'https://www.example.test/chat', hostname: 'www.example.test' },
    document: { readyState: 'complete', querySelectorAll() { return [] } },
    chrome: {
      runtime: {
        onMessage: { addListener(fn) { listeners.push(fn) } },
        sendMessage() { return { catch() {} } },
      },
    },
    TULAIPage: page,
  })
  if (page.resolveSources || page.resolveConversation) {
    context.TULAIDoubaoSourceMap = {
      fromConversation: page.resolveSources,
      resolveFromConversation: page.resolveConversation,
    }
  }
  context.globalThis = context
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'protocol.js'), 'utf8'), context)
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'refuse.js'), 'utf8'), context)
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'quota.js'), 'utf8'), context)
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'content.js'), 'utf8'), context)
  assert.strictEqual(listeners.length, 1)
  function command(payload, options = {}) {
    const env = { v: 1, id: 't-' + payload.name, type: 'command', taskId: options.taskId || 'task-1', idempotencyKey: options.idempotencyKey || '', payload }
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('hung ' + payload.name)), 8000)
      listeners[0]({ type: 'COMMAND_FROM_HOST', envelope: env }, {}, (resp) => {
        clearTimeout(timer)
        resolve(resp)
      })
    })
  }
  command.ask = function (msg) {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('hung ' + (msg && msg.type))), 2000)
      listeners[0](msg, {}, (resp) => {
        clearTimeout(timer)
        resolve(resp)
      })
    })
  }
  command.locateFileInput = function () { return vm.runInContext('locateFileInput()', context) }
  return command
}

function composerPage(extra) {
  const box = { value: '水彩风格' }
  const button = { click() { box.value = '' }, dispatchEvent() {} }
  return Object.assign({
    normalize(text) { return String(text || '').replace(/\s+/g, ' ').trim() },
    composer() { return box },
    readComposer() { return box.value },
    sendButton() { return button },
    loginWall() { return false },
    mark() { return { id: 'mark' } },
    sendAccepted() { return box.value === '' },
    detectCaptcha() { return { blocked: false } },
  }, extra)
}

async function sendThenWait(page) {
  const call = load(page)
  const sent = await call({ name: 'send', prompt: '水彩风格', timeoutMs: 2000 })
  assert.strictEqual(sent.payload.ok, true, JSON.stringify(sent.payload))
  const started = Date.now()
  const waited = await call({ name: 'waitResult', timeoutMs: 3000 })
  return { waited, elapsed: Date.now() - started }
}

async function main() {
  {
    // setRatio：设上与否都回 ok，区别在 applied；页面脚本不支持时也算未设上。
    const ok = load(composerPage({
      doubaoHost() { return true },
      async ensureImageMode() { return { ok: true } },
      async setOutputRatio(doc, ratio) { return ratio === '4:3' ? { ok: true } : { error: '「比例」菜单没有 ' + ratio, code: 'E_SELECTOR_MISS' } },
    }))
    const applied = await ok({ name: 'setRatio', ratio: '4:3' })
    assert.strictEqual(applied.payload.ok, true)
    assert.strictEqual(applied.payload.applied, true)
    const missed = await ok({ name: 'setRatio', ratio: '5:4' })
    assert.strictEqual(missed.payload.ok, true)
    assert.strictEqual(missed.payload.applied, false)
    assert.match(missed.payload.message, /菜单没有/)
    const legacy = load(composerPage({}))
    const noSupport = await legacy({ name: 'setRatio', ratio: '4:3' })
    assert.strictEqual(noSupport.payload.ok, true)
    assert.strictEqual(noSupport.payload.applied, false)
  }
  for (const withImage of [false, true]) {
    let active = false
    let switches = 0
    let box = { value: '' }
    const actions = []
    const page = composerPage({
      doubaoHost() { return true },
      async ensureImageMode() {
        if (!active) {
          actions.push('mode')
          active = true
          switches++
          box = { value: '' }
        }
        return { ok: true }
      },
      composer() { return box },
      readComposer(comp) { return comp.value },
      setComposer(comp, text) {
        assert.strictEqual(active, true)
        assert.strictEqual(comp, box, '填词必须使用切换模式后的输入框')
        actions.push('prompt')
        comp.value = text
        return true
      },
      removeButtons() { return [] },
      unsentBlocksUpload() { return '' },
      fileInput() { return {} },
      base64ToBytes() { return new Uint8Array([1]) },
      assignFiles() { assert.strictEqual(active, true); actions.push('upload'); return true },
      freshAttachment() { return true },
      sendButton() { return { click() { assert.strictEqual(active, true); actions.push('send'); box.value = '' } } },
      sendAccepted() { return box.value === '' },
    })
    const call = load(page)
    if (withImage) {
      const uploaded = await call({ name: 'upload', uploadId: 'mode-image', total: 1, dataBase64: 'AQ==', finish: true })
      assert.strictEqual(uploaded.payload.ok, true, JSON.stringify(uploaded.payload))
    }
    assert.strictEqual((await call({ name: 'setPrompt', prompt: '水彩风格' })).payload.ok, true)
    assert.strictEqual((await call({ name: 'send', prompt: '水彩风格' })).payload.ok, true)
    assert.deepStrictEqual(actions, withImage ? ['mode', 'upload', 'prompt', 'send'] : ['mode', 'prompt', 'send'])
    assert.strictEqual(switches, 1, '洗图上传、填词和发送之间不能重复切换模式')
  }
  for (const name of ['upload', 'setPrompt', 'send']) {
    const call = load(composerPage({
      doubaoHost() { return true },
      ensureImageMode() { return { error: '点击「图像生成」后模式未生效，未开始任务', code: 'E_SELECTOR_MISS' } },
      setComposer() { assert.fail('切换失败后不能填词') },
      assignFiles() { assert.fail('切换失败后不能上传') },
      sendButton() { assert.fail('切换失败后不能发送') },
    }))
    const failed = await call({ name, prompt: '水彩风格' })
    assert.strictEqual(failed.payload.code, 'E_SELECTOR_MISS', name)
    assert.match(failed.payload.message, /模式未生效/)
  }
  const plainQianwen = load(composerPage({
    setComposer(box, text) { box.value = text; return true },
    ensureImageMode() { assert.fail('千问纯文字任务不应提前切模式') },
  }))
  assert.strictEqual((await plainQianwen({ name: 'setPrompt', prompt: '水彩风格' })).payload.ok, true)

  const qianwen = composerPage({
    reveals: 0,
    ready: false,
    activate(el) { el.click(); return true },
    revealResults() { this.reveals++; this.ready = true },
    resultImages() {
      const gray = [{ url: GRAY }]
      if (!this.ready) return gray
      return gray.concat([
        { url: 'https://cdn.example/watercolor-a.png' },
        { url: 'https://cdn.example/watercolor-b.png' },
      ])
    },
    resultsBusy() { return !this.ready },
    isPlaceholderURL(url) { return String(url || '').startsWith('data:') || String(url || '').startsWith('blob:') },
  })
  const qianwenResult = await sendThenWait(qianwen)
  assert.ok(qianwen.reveals >= 1, '等待循环要先把千问生成卡滚进视口')
  assert.strictEqual(qianwenResult.waited.payload.ok, true, JSON.stringify(qianwenResult.waited.payload))
  assert.deepStrictEqual(qianwenResult.waited.payload.images.map((item) => item.url), [
    'https://cdn.example/watercolor-a.png',
    'https://cdn.example/watercolor-b.png',
  ])
  assert.ok(qianwenResult.elapsed >= 350, '两张实图要连续两次一样才返回，用时 ' + qianwenResult.elapsed)

  const doubao = composerPage({
    resultImages() {
      return [
        { url: 'data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" width="1536" height="1536"/>' },
        { url: 'https://cdn.example/full.png', rawUrl: 'https://p3.byteimg.com/raw.png?signature=123' },
      ]
    },
    resultsBusy() { return false },
    isPlaceholderURL(url) { return /^data:image\/svg/i.test(String(url || '')) || /^blob:/i.test(String(url || '')) },
  })
  assert.strictEqual(typeof doubao.revealResults, 'undefined')
  const doubaoResult = await sendThenWait(doubao)
  assert.strictEqual(doubaoResult.waited.payload.ok, true, JSON.stringify(doubaoResult.waited.payload))
  assert.deepStrictEqual(doubaoResult.waited.payload.images.map((item) => item.url), ['https://cdn.example/full.png'])
  assert.strictEqual(doubaoResult.waited.payload.images[0].rawUrl, 'https://p3.byteimg.com/raw.png?signature=123')
  assert.ok(doubaoResult.elapsed >= 350, '豆包仍要两次稳定取样，用时 ' + doubaoResult.elapsed)

  const reordered = composerPage({
    resultImages() { return [{ url: 'https://p1.byteimg.com/out-b' }, { url: 'https://p1.byteimg.com/out-a' }] },
    resultsBusy() { return false },
    replyDone() { return { done: true } },
    resolveSources(conversation, names) {
      assert.strictEqual(conversation, 'https://www.doubao.com/chat/reordered')
      assert.deepStrictEqual(names, ['a.png', 'b.png'])
      return Promise.resolve([2, 1])
    },
  })
  const reorderedCall = load(reordered, { href: 'https://www.doubao.com/chat/reordered', hostname: 'www.doubao.com' })
  assert.strictEqual((await reorderedCall({ name: 'send', prompt: '水彩风格' })).payload.ok, true)
  const reorderedAck = await reorderedCall({ name: 'waitResult', timeoutMs: 6000, expectedAttachments: ['a.png', 'b.png'] })
  assert.deepStrictEqual(reorderedAck.payload.images.map(item => item.sourceIndex), [2, 1])

  const blocked = composerPage({
    detectCaptcha() { return { blocked: true, kind: 'slider', baxia: true, root: {} } },
    captchaCrop() { return '' },
  })
  const captchaCall = load(blocked)
  const captchaSent = await captchaCall({ name: 'send', prompt: '水彩风格', timeoutMs: 2000 })
  assert.strictEqual(captchaSent.payload.ok, false)
  assert.strictEqual(captchaSent.payload.code, 'E_CAPTCHA_HUMAN')
  assert.strictEqual(captchaSent.payload.kind, 'slider')
  assert.strictEqual(captchaSent.payload.baxia, true)
  assert.strictEqual(captchaSent.payload.captchaBlocked, true)

  const refusedPage = composerPage({
    assistantText() { return '抱歉，我无法生成你描述的图片，内容不符合规范' },
    resultImages() { return [] },
    resultsBusy() { return false },
  })
  const refusedCall = load(refusedPage)
  const refusedSent = await refusedCall({ name: 'send', prompt: '水彩风格', timeoutMs: 2000 })
  assert.strictEqual(refusedSent.payload.ok, true)
  const started = Date.now()
  const refusedWait = await refusedCall({ name: 'waitResult', timeoutMs: 3000 })
  assert.strictEqual(refusedWait.payload.code, 'E_MODERATION')
  assert.match(refusedWait.payload.message, /审核不通过/)
  assert.ok(Date.now() - started < 1000, '审核失败应马上返回，用时 ' + (Date.now() - started))

  const quotaPage = composerPage({
    assistantText() { return '今日图片生成免费额度已用完。开通订阅畅享更多额度。' },
    resultImages() { return [] },
    resultsBusy() { return false },
  })
  const quotaCall = load(quotaPage, { href: 'https://www.doubao.com/chat/new', hostname: 'www.doubao.com' })
  assert.strictEqual((await quotaCall({ name: 'send', prompt: '水彩风格' })).payload.ok, true)
  const quotaWait = await quotaCall({ name: 'waitResult', timeoutMs: 3000 })
  assert.strictEqual(quotaWait.payload.code, 'E_QUOTA')
  assert.match(quotaWait.payload.message, /今日图片生成免费额度已用完/)

  let attachmentChecks = 0
  const uploadPage = composerPage({
    readComposer() { return '' },
    removeButtons() { return [] },
    unsentBlocksUpload() { return '' },
    fileInput() { return {} },
    assignFiles() { return true },
    base64ToBytes() { return new Uint8Array([1]) },
    freshAttachment() { return true },
    attachmentNames() { return ++attachmentChecks < 3 ? [] : ['one.png'] },
  })
  const uploadCall = load(uploadPage, { href: 'https://www.doubao.com/chat/', hostname: 'www.doubao.com' })
  const uploadAck = await uploadCall({ name: 'upload', uploadId: 'one', fileName: 'one.png', total: 1, dataBase64: 'AQ==', finish: true, verifyOrder: true, expectedAttachments: ['one.png'], timeoutMs: 2000 })
  assert.strictEqual(uploadAck.payload.ok, true, '等待附件名称出现后才确认上传')
  assert.ok(attachmentChecks >= 3)
  assert.strictEqual((await uploadCall({ name: 'washChat', mode: 'ready' })).payload.ready, false, '带有未发送附件的输入区不能作为新会话')

  uploadPage.attachmentNames = () => ['stale.png', 'one.png']
  const staleAck = await uploadCall({ name: 'upload', uploadId: 'two', fileName: 'one.png', total: 1, dataBase64: 'AQ==', finish: true, verifyOrder: true, expectedAttachments: ['one.png'], timeoutMs: 500 })
  assert.strictEqual(staleAck.payload.code, 'E_UPLOAD_FAIL', '残留附件必须阻止发送')

  const networkPage = composerPage({
    setComposer(box, value) { box.value = value; return true },
    resultImages() { return [{ url: 'https://cdn.example/result.png' }] },
    resultsBusy() { return false },
  })
  const networkCall = load(networkPage)
  assert.strictEqual((await networkCall({ name: 'send', prompt: '水彩风格' })).payload.ok, true)
  await networkCall.ask({ type: 'NETWORK_FAULT', taskId: 'other-task', kind: 'quota' })
  assert.strictEqual((await networkCall({ name: 'waitResult', timeoutMs: 3000 })).payload.ok, true, '其他任务的响应不能影响本次任务')
  await networkCall.ask({ type: 'NETWORK_FAULT', taskId: 'task-1', kind: 'quota' })
  const afterAcceptance = await networkCall({ name: 'waitResult', timeoutMs: 3000 })
  assert.strictEqual(afterAcceptance.payload.code, 'E_SEND_UNCERTAIN', '已确认发送后不能以额度错误触发换号重发')
  assert.strictEqual((await networkCall({ name: 'setPrompt', prompt: '水彩风格' })).payload.ok, true, '后续批次复用任务编号时不能沿用上次的网络错误')

  let earlyCall
  let clicks = 0
  const earlyPage = composerPage({ activate() {
    clicks++
    earlyCall.ask({ type: 'NETWORK_FAULT', taskId: 'task-1', kind: 'quota' })
    return true
  } })
  earlyCall = load(earlyPage)
  const sendOptions = { idempotencyKey: 'early' }
  assert.strictEqual((await earlyCall({ name: 'send', prompt: '水彩风格' }, sendOptions)).payload.code, 'E_QUOTA')
  assert.strictEqual((await earlyCall({ name: 'send', prompt: '水彩风格' }, sendOptions)).payload.code, 'E_SEND_UNCERTAIN')
  assert.strictEqual(clicks, 1, '同一发送键不能点击两次')

  let simultaneousCall
  const simultaneousPage = composerPage({ activate(el) {
    el.click()
    simultaneousCall.ask({ type: 'NETWORK_FAULT', taskId: 'task-1', kind: 'quota' })
    return true
  } })
  simultaneousCall = load(simultaneousPage)
  assert.strictEqual((await simultaneousCall({ name: 'send', prompt: '水彩风格' })).payload.ok, true, '页面已接受时先确认发送')
  assert.strictEqual((await simultaneousCall({ name: 'waitResult' })).payload.code, 'E_SEND_UNCERTAIN')

  const signed = load(composerPage())
  assert.strictEqual((await signed.ask({ type: 'LOGIN_STATE' })).loggedIn, true)
  const signedOut = load(composerPage({ loginWall() { return true } }))
  assert.strictEqual((await signedOut.ask({ type: 'LOGIN_STATE' })).loggedIn, false)
  for (const name of ['upload', 'setPrompt', 'send']) {
    const attempt = await signedOut({ name, prompt: '水彩风格' })
    assert.strictEqual(attempt.payload.code, 'E_NOT_LOGGED_IN', '未登录时不能操作输入框：' + name)
  }
  let entryChecks = 0
  let opened = false
  const fileInput = {}
  const slowPage = composerPage({
    fileInput() { return opened ? fileInput : null },
    uploadImageItem() { return null },
    uploadEntry() {
      entryChecks++
      return entryChecks >= 3 ? { click() { opened = true } } : null
    },
  })
  const located = await load(slowPage).locateFileInput()
  assert.strictEqual(located.input, fileInput, '等待延迟出现的上传入口，再选择文件')
  assert.strictEqual(entryChecks, 3)
  const chatPage = composerPage({ replyDone() { return { done: true } }, resultsBusy() { return false } })
  const chat = load(chatPage, { href: 'https://www.doubao.com/chat/group-1?private=1', hostname: 'www.doubao.com' })
  assert.strictEqual((await chat({ name: 'send', prompt: '水彩风格' })).payload.ok, true)
  const checked = await chat({ name: 'washChat', mode: 'check', expected: 'https://www.doubao.com/chat/group-1' })
  assert.strictEqual(checked.payload.reusable, true)
  assert.strictEqual(checked.payload.conversation, 'https://www.doubao.com/chat/group-1')
  assert.strictEqual((await chat({ name: 'washChat', mode: 'check', expected: 'https://www.doubao.com/chat/other' })).payload.reusable, false)
  const proto = vm.createContext({ globalThis: {} })
  proto.globalThis = proto
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'protocol.js'), 'utf8'), proto)
  assert.strictEqual(proto.TULAI.hello('t', 'qianwen', 'profile-1').payload.loggedIn, undefined)
  assert.strictEqual(proto.TULAI.hello('t', 'doubao', 'profile-2', true).payload.loggedIn, true)
  assert.strictEqual(proto.TULAI.hello('t', 'qianwen', 'profile-3', false).payload.loggedIn, false)

  let batchReady = false
  let batchScrolled = 0
  const batchPage = composerPage({
    newAssistants() {
      return [{ scrollIntoView(opt) { if (opt && opt.block === 'end') batchScrolled++ } }]
    },
    resultImages() {
      const list = [
        { url: 'https://cdn.example/b1.png' },
        { url: 'https://cdn.example/b2.png' },
      ]
      if (batchReady) list.push({ url: 'https://cdn.example/b3.png' })
      return list
    },
    resultsBusy() { return false },
    replyDone() { return { done: true } },
    resolveSources() { return Promise.resolve([1, 2, 3]) },
  })
  const batchCall = load(batchPage, { href: 'https://www.doubao.com/chat/batch-wait', hostname: 'www.doubao.com' })
  assert.strictEqual((await batchCall({ name: 'send', prompt: '水彩风格' })).payload.ok, true)
  setTimeout(() => { batchReady = true }, 3400)
  const batchAck = await batchCall({
    name: 'waitResult',
    timeoutMs: 12000,
    expectedAttachments: ['a.png', 'b.png', 'c.png'],
  })
  assert.strictEqual(batchAck.payload.ok, true, JSON.stringify(batchAck.payload))
  assert.strictEqual(batchAck.payload.images.length, 3, '多图未齐时不能在 3 秒处提前截断')
  assert.ok(batchScrolled >= 1, '多图未齐时应滚动到回复底部唤醒虚拟列表')

  const sevenDayQuotaPage = composerPage({
    resultImages() { return [] },
    resultsBusy() { return false },
    assistantText() { return '近 7 天额度已用完，预计明天恢复。升级到标准套餐畅享更多额度。 升级正式订阅 ->' },
  })
  const sevenDayQuotaCall = load(sevenDayQuotaPage, { href: 'https://www.doubao.com/chat/quota-7d', hostname: 'www.doubao.com' })
  assert.strictEqual((await sevenDayQuotaCall({ name: 'send', prompt: '水彩风格' })).payload.ok, true)
  const sevenDayAck = await sevenDayQuotaCall({ name: 'waitResult', timeoutMs: 2000 })
  assert.strictEqual(sevenDayAck.payload.ok, false)
  assert.strictEqual(sevenDayAck.payload.code, 'E_QUOTA')
  assert.ok(sevenDayAck.payload.message.includes('近 7 天额度已用完'), sevenDayAck.payload.message)

  async function streamHoldTest() {
    const page = composerPage({
      resultImages() { return [{ url: 'https://cdn.example/stream-1.png' }] },
      resultsBusy() { return false },
      replyDone() { return { done: true } },
    })
    const call = load(page, { href: 'https://www.doubao.com/chat/stream-hold', hostname: 'www.doubao.com' })
    assert.strictEqual((await call({ name: 'send', prompt: '水彩风格' })).payload.ok, true)
    await call.ask({ type: 'NETWORK_STREAM', taskId: 'task-1', streaming: true })
    let settled = false
    const t0 = Date.now()
    const waitPromise = call({ name: 'waitResult', timeoutMs: 9500 }).then((res) => {
      settled = true
      return res
    })
    await new Promise((r) => setTimeout(r, 3400))
    assert.strictEqual(settled, false, 'SSE 流仍在进行时 waitResult 不应提前结束')
    await call.ask({ type: 'NETWORK_STREAM', taskId: 'task-1', streaming: false })
    const ack = await waitPromise
    assert.strictEqual(ack.payload.ok, true, JSON.stringify(ack.payload))
    assert.ok(Date.now() - t0 >= 5500, 'SSE 结束后再稳定 3 秒才应返回结果')
  }

  async function partialScrollTest() {
    const scrollBlocks = []
    const page = composerPage({
      newAssistants() {
        return [{ scrollIntoView(opt) { if (opt && opt.block) scrollBlocks.push(opt.block) } }]
      },
      resultImages() {
        return [
          { url: 'https://cdn.example/p1.png' },
          { url: 'https://cdn.example/p2.png' },
        ]
      },
      resultsBusy() { return false },
      replyDone() { return { done: true } },
      resolveConversation(_conv, _names, images) {
        return Promise.resolve({ images: images.slice(), indices: [3, 1] })
      },
    })
    const call = load(page, { href: 'https://www.doubao.com/chat/partial-wait', hostname: 'www.doubao.com' })
    assert.strictEqual((await call({ name: 'send', prompt: '水彩风格' })).payload.ok, true)
    const ack = await call({
      name: 'waitResult',
      timeoutMs: 5200,
      expectedAttachments: ['a.png', 'b.png', 'c.png'],
    })
    assert.strictEqual(ack.payload.ok, true, JSON.stringify(ack.payload))
    assert.deepStrictEqual(scrollBlocks.slice(0, 3), ['end', 'start', 'center'], '多图未齐时应按 end/start/center 轮换滚动')
    assert.strictEqual(ack.payload.images.length, 2, '部分生成结果应按已关联张数返回')
    assert.deepStrictEqual(ack.payload.images.map((x) => x.sourceIndex), [3, 1], '部分结果必须保留逐张 sourceIndex')
  }

  async function resolveSupplementTest() {
    const page = composerPage({
      resultImages() {
        return [
          { url: 'https://cdn.example/s1.png' },
          { url: 'https://cdn.example/s2.png' },
        ]
      },
      resultsBusy() { return false },
      replyDone() { return { done: true } },
      resolveConversation() {
        return Promise.resolve({
          images: [
            { url: 'https://cdn.example/s1.png' },
            { url: 'https://cdn.example/s2.png' },
            { url: 'https://cdn.example/s3.png' },
          ],
          indices: [3, 1, 2],
        })
      },
    })
    const call = load(page, { href: 'https://www.doubao.com/chat/resolve-wait', hostname: 'www.doubao.com' })
    assert.strictEqual((await call({ name: 'send', prompt: '水彩风格' })).payload.ok, true)
    const ack = await call({
      name: 'waitResult',
      timeoutMs: 5200,
      expectedAttachments: ['a.png', 'b.png', 'c.png'],
    })
    assert.strictEqual(ack.payload.ok, true, JSON.stringify(ack.payload))
    assert.strictEqual(ack.payload.images.length, 3, 'resolveFromConversation 补全屏外图后应返回全部 3 张')
    assert.deepStrictEqual(ack.payload.images.map((x) => x.sourceIndex), [3, 1, 2])
  }

  await Promise.all([streamHoldTest(), partialScrollTest(), resolveSupplementTest()])

  console.log('content wait ok')
}

main().catch((err) => {
  console.error(err)
  process.exit(1)
})
