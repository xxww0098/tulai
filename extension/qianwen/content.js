// 内容脚本把主机命令落到千问页面。选择器在 page.js，和 ego.js 的锚点一致。
// 发送用 envelope.idempotencyKey 记住已经点过的一次：超时重试只复查结果，不再点发送。
const XBS = globalThis.XBS
const Page = globalThis.XBSPage

const uploadParts = new Map()
const uploadApplied = new Map()
const sendState = new Map()
const downloadCache = new Map()
let pageMark = null
let cancelRequested = false

chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  if (!msg || msg.type !== 'COMMAND_FROM_HOST') return
  const env = msg.envelope || {}
  handleCommand(env).then(sendResponse).catch((err) => {
    sendResponse(XBS.fail(env, XBS.errors.E_PROTOCOL, String(err && err.message || err)))
  })
  return true
})

chrome.runtime.sendMessage({ type: 'TAB_READY', href: location.href }).catch(() => {})

async function handleCommand(env) {
  const name = env.payload && env.payload.name
  if (name === XBS.commands.cancel) {
    cancelRequested = true
    return XBS.ack(env, { name, ok: true, message: '已请求取消页面内等待' })
  }
  cancelRequested = false
  const payload = env.payload || {}
  switch (name) {
    case XBS.commands.upload:
      return upload(env, payload)
    case XBS.commands.setPrompt:
      return setPrompt(env, payload)
    case XBS.commands.send:
      return sendPrompt(env, payload)
    case XBS.commands.waitResult:
      return waitResult(env, payload)
    case XBS.commands.download:
      return download(env, payload)
    case XBS.commands.captchaAssist:
      return captchaAssist(env)
    default:
      return XBS.fail(env, XBS.errors.E_PROTOCOL, '内容脚本未实现命令 ' + name)
  }
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

function loginOrMiss(env, name, message) {
  if (Page.loginWall(document)) {
    return XBS.fail(env, XBS.errors.E_NOT_LOGGED_IN, '页面没有输入框，看起来还未登录')
  }
  return XBS.fail(env, XBS.errors.E_SELECTOR_MISS, message)
}

async function upload(env, payload) {
  const comp = Page.composer(document)
  if (!comp) return loginOrMiss(env, payload.name, '未找到可写入的输入框')
  if (payload.clean) {
    const buttons = Page.removeButtons(document, comp)
    buttons.forEach((button) => {
      try { button.click() } catch (e) {}
    })
    if (buttons.length) await delay(200)
  }
  const uploadId = String(payload.uploadId || '')
  if (uploadApplied.get(uploadId)) {
    return XBS.ack(env, { name: payload.name, ok: true, deduped: true, message: '这张源图已经交给输入区' })
  }
  const total = Number(payload.total) || 0
  const offset = Number(payload.offset) || 0
  let part = uploadParts.get(uploadId)
  if (!part) {
    part = { total, chunks: new Map(), fileName: payload.fileName || 'image', mime: payload.mime || 'application/octet-stream' }
    uploadParts.set(uploadId, part)
  }
  if (payload.dataBase64) part.chunks.set(offset, Page.base64ToBytes(payload.dataBase64))
  part.total = total
  if (!payload.finish) {
    return XBS.ack(env, { name: payload.name, ok: true, offset, received: true })
  }
  const bytes = concatChunks(part)
  if (total && bytes.length !== total) {
    uploadParts.delete(uploadId)
    return XBS.fail(env, XBS.errors.E_UPLOAD_FAIL, '源图分片不完整')
  }
  if (!bytes.length) {
    return XBS.fail(env, XBS.errors.E_UPLOAD_FAIL, '源图是空文件')
  }
  if (!part.assigned) {
    const blocked = Page.unsentBlocksUpload(document, Page.composer(document))
    if (blocked) return XBS.fail(env, XBS.errors.E_UPLOAD_FAIL, blocked)
    const located = await locateFileInput()
    if (located.error) return XBS.fail(env, located.code || XBS.errors.E_SELECTOR_MISS, located.error)
    let file
    try {
      file = new File([bytes], part.fileName, { type: part.mime })
    } catch (err) {
      return XBS.fail(env, XBS.errors.E_UPLOAD_FAIL, '无法构造上传文件')
    }
    part.baseline = new Set(Array.from(document.querySelectorAll('img')))
    if (!Page.assignFiles(located.input, [file])) {
      return XBS.fail(env, XBS.errors.E_UPLOAD_FAIL, '无法把文件写入 input[type=file]')
    }
    // 文件已经交给 input。重试只复查预览，避免同一张图再挂一次。
    part.assigned = true
  }
  const ready = await waitUntil(Number(payload.timeoutMs) || 30000, function () {
    return Page.freshAttachment(document, part.baseline)
  }, 2)
  if (ready === 'cancel') return XBS.fail(env, XBS.errors.E_CANCELLED, '上传等待已取消')
  if (ready !== true) {
    return XBS.fail(env, XBS.errors.E_UPLOAD_FAIL, '未确认当前输入区源图附件已就绪')
  }
  uploadApplied.set(uploadId, true)
  uploadParts.delete(uploadId)
  return XBS.ack(env, { name: payload.name, ok: true })
}

function concatChunks(part) {
  const offsets = Array.from(part.chunks.keys()).sort(function (a, b) { return a - b })
  let size = 0
  offsets.forEach(function (offset) { size += part.chunks.get(offset).length })
  const out = new Uint8Array(size)
  let cursor = 0
  for (let i = 0; i < offsets.length; i++) {
    const chunk = part.chunks.get(offsets[i])
    out.set(chunk, cursor)
    cursor += chunk.length
  }
  return out
}

// 先找隐藏的 file input。没有时才点「添加附件」，并只在菜单项内部已经带着 input 时使用它。
// 不点击单独的「上传图片」菜单，否则会弹出系统文件框，扩展填不进去。
async function locateFileInput() {
  const comp = Page.composer(document)
  let input = Page.fileInput(document, comp)
  if (input) return { input }
  const entry = Page.uploadEntry(document, comp)
  if (!entry) return { error: '未找到当前输入区上传入口', code: XBS.errors.E_SELECTOR_MISS }
  try { entry.click() } catch (e) {}
  const deadline = Date.now() + 2000
  while (Date.now() < deadline) {
    if (cancelRequested) return { error: '上传已取消', code: XBS.errors.E_CANCELLED }
    input = Page.fileInput(document, Page.composer(document))
    if (input) return { input }
    const item = Page.uploadImageItem(document)
    const nested = item && item.querySelector ? item.querySelector('input[type="file"]') : null
    if (nested) return { input: nested }
    await delay(100)
  }
  return { error: '未找到输入区的文件选择框（添加附件 旁没有 input[type=file]）', code: XBS.errors.E_SELECTOR_MISS }
}

async function setPrompt(env, payload) {
  const comp = Page.composer(document)
  if (!comp) return loginOrMiss(env, payload.name, '未找到可写入提示词的输入框')
  const prompt = String(payload.prompt || '')
  if (!Page.setComposer(comp, prompt)) {
    return XBS.fail(env, XBS.errors.E_SELECTOR_MISS, '输入区内容与本次提示词不一致，未发送')
  }
  return XBS.ack(env, { name: payload.name, ok: true })
}

async function sendPrompt(env, payload) {
  const key = String(env.idempotencyKey || payload.sendKey || '')
  if (key && sendState.has(key)) {
    const prev = sendState.get(key)
    if (prev.promise && prev.phase === 'clicking') return prev.promise
    if (prev.phase === 'ok') return XBS.ack(env, { name: payload.name, ok: true, deduped: true })
    if (prev.phase === 'uncertain') {
      return XBS.fail(env, XBS.errors.E_SEND_UNCERTAIN, prev.message || '发送状态未确认，未再次点击')
    }
  }
  const comp = Page.composer(document)
  if (!comp) return loginOrMiss(env, payload.name, '未找到可写入提示词的输入框')
  const prompt = String(payload.prompt || '')
  if (Page.normalize(Page.readComposer(comp)) !== Page.normalize(prompt)) {
    return XBS.fail(env, XBS.errors.E_SELECTOR_MISS, '输入区内容与本次提示词不一致，未发送')
  }
  const button = await waitFor(function () { return Page.sendButton(document, Page.composer(document)) }, 5000)
  if (button === 'cancel') return XBS.fail(env, XBS.errors.E_CANCELLED, '发送已取消')
  if (!button) return XBS.fail(env, XBS.errors.E_SELECTOR_MISS, '未找到已就绪的发送按钮')
  // 点发送之前记边界，和策略里的 MarkTask 一样，避免把源图回显当成结果。
  pageMark = Page.mark(document, prompt)
  const rec = { phase: 'clicking', message: '' }
  const job = confirmSend(env, payload, button, rec)
  rec.promise = job
  if (key) sendState.set(key, rec)
  return job
}

async function confirmSend(env, payload, button, rec) {
  try { button.click() } catch (err) {
    rec.phase = 'uncertain'
    rec.message = '点击发送失败'
    return XBS.fail(env, XBS.errors.E_SEND_UNCERTAIN, rec.message)
  }
  const accepted = await waitUntil(Number(payload.timeoutMs) || 15000, function () {
    return Page.sendAccepted(document, pageMark)
  }, 1)
  if (accepted === true) {
    rec.phase = 'ok'
    rec.promise = null
    return XBS.ack(env, { name: payload.name, ok: true })
  }
  rec.phase = 'uncertain'
  rec.promise = null
  rec.message = accepted === 'cancel' ? '发送已点击，取消时尚未确认平台接受' : '平台未接受消息：发送后输入仍未清空'
  return XBS.fail(env, XBS.errors.E_SEND_UNCERTAIN, rec.message)
}

async function waitResult(env, payload) {
  if (!pageMark) return XBS.fail(env, XBS.errors.E_SELECTOR_MISS, '还没有本轮发送标记，不能收集结果')
  const timeout = Number(payload.timeoutMs) || 120000
  const deadline = Date.now() + timeout
  let stable = 0
  let lastURLs = ''
  while (Date.now() < deadline) {
    if (cancelRequested) return XBS.fail(env, XBS.errors.E_CANCELLED, '等待结果已取消')
    const captcha = Page.detectCaptcha(document)
    if (captcha.blocked) return captchaAck(env, captcha)
    const images = Page.resultImages(document, pageMark)
    const busy = Page.resultsBusy(document, pageMark)
    const urls = images.map(function (item) { return item.url }).join('\n')
    if (!busy && images.length) {
      stable = urls === lastURLs ? stable + 1 : 1
      lastURLs = urls
      if (stable >= 2) {
        return XBS.ack(env, {
          name: payload.name,
          ok: true,
          images: images.map(function (item) { return { url: item.url } }),
        })
      }
    } else {
      stable = 0
      lastURLs = urls
    }
    await delay(400)
  }
  return XBS.fail(env, XBS.errors.E_TIMEOUT, '等待生成结果超时')
}

async function download(env, payload) {
  const url = String(payload.url || '')
  if (!url) return XBS.fail(env, XBS.errors.E_DOWNLOAD_FAIL, '缺少图片地址')
  const offset = Number(payload.offset) || 0
  const limit = Number(payload.limit) || (3 * 1024 * 1024)
  let cached = downloadCache.get(url)
  if (!cached) {
    const fetched = await fetchImage(url)
    if (fetched.error) return XBS.fail(env, XBS.errors.E_DOWNLOAD_FAIL, fetched.error)
    if (fetched.path) {
      return XBS.ack(env, { name: payload.name, ok: true, path: fetched.path, done: true, total: 0 })
    }
    cached = fetched.bytes
    downloadCache.set(url, cached)
  }
  const end = Math.min(cached.length, offset + limit)
  const slice = cached.subarray(offset, end)
  return XBS.ack(env, {
    name: payload.name,
    ok: true,
    dataBase64: Page.bytesToBase64(slice),
    offset,
    total: cached.length,
    done: end >= cached.length,
  })
}

async function fetchImage(url) {
  try {
    const res = await fetch(url, { credentials: 'include', cache: 'no-store' })
    if (res.ok) return { bytes: new Uint8Array(await res.arrayBuffer()) }
  } catch (e) {}
  try {
    const res = await fetch(url, { credentials: 'omit', cache: 'no-store' })
    if (res.ok) return { bytes: new Uint8Array(await res.arrayBuffer()) }
  } catch (e) {}
  try {
    const saved = await chrome.runtime.sendMessage({ type: 'FETCH_URL', url })
    if (saved && saved.bytesBase64) return { bytes: Page.base64ToBytes(saved.bytesBase64) }
    if (saved && saved.path) return { path: saved.path }
    if (saved && saved.error) return { error: saved.error }
  } catch (err) {
    return { error: '下载失败: ' + safeURL(url) }
  }
  return { error: '下载失败: ' + safeURL(url) }
}

function safeURL(url) {
  try {
    const parsed = new URL(url, location.href)
    return parsed.origin + parsed.pathname.slice(0, 80)
  } catch (e) {
    return 'invalid-url'
  }
}

function captchaAssist(env) {
  const found = Page.detectCaptcha(document)
  if (!found.blocked) return XBS.ack(env, { name: XBS.commands.captchaAssist, ok: true, captchaBlocked: false })
  return captchaAck(env, found)
}

function captchaAck(env, found) {
  const crop = Page.captchaCrop(found.root)
  return XBS.ack(env, {
    name: XBS.commands.captchaAssist,
    ok: false,
    code: XBS.errors.E_CAPTCHA_HUMAN,
    message: '页面出现验证码，需要人工处理',
    captchaBlocked: true,
    kind: found.kind || 'unknown',
    crop: crop || '',
  })
}

async function waitFor(fn, timeout) {
  const deadline = Date.now() + timeout
  while (Date.now() < deadline) {
    if (cancelRequested) return 'cancel'
    const value = fn()
    if (value) return value
    await delay(200)
  }
  return null
}

// needStable 次连续命中才算数，用来躲开附件预览刚插入时的抖动。
async function waitUntil(timeout, fn, needStable) {
  const deadline = Date.now() + timeout
  let stable = 0
  while (Date.now() < deadline) {
    if (cancelRequested) return 'cancel'
    if (fn()) {
      stable++
      if (stable >= needStable) return true
    } else {
      stable = 0
    }
    await delay(300)
  }
  return false
}
