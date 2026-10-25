// 内容脚本把主机命令落到当前页面。千问加载 qianwen/page.js，豆包加载 doubao/page.js，
// 两边都提供 XBSPage。选择器在各自的 page 脚本里，和 ego.js 的锚点一致。
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

async function readyComposer(timeout) {
  return waitFor(function () {
    const comp = Page.composer(document)
    if (comp) return comp
    if (Page.loginWall(document)) return 'login'
    return null
  }, timeout || 8000)
}

async function upload(env, payload) {
  const ready = await readyComposer(8000)
  if (ready === 'cancel') return XBS.fail(env, XBS.errors.E_CANCELLED, '等待输入框已取消')
  if (!ready || ready === 'login') return loginOrMiss(env, payload.name, '未找到可写入的输入框')
  const comp = ready
  if (payload.clean) {
    if (typeof Page.dismissClientPromo === 'function') {
      const promo = Page.dismissClientPromo(document, comp)
      if (promo) {
        try { promo.click() } catch (e) {}
        await delay(200)
      }
    }
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
    let file
    try {
      file = new File([bytes], part.fileName, { type: part.mime })
    } catch (err) {
      return XBS.fail(env, XBS.errors.E_UPLOAD_FAIL, '无法构造上传文件')
    }
    part.baseline = new Set(Array.from(document.querySelectorAll('img')))
    // 千问 page.js 提供 attachLocalImage：有 file input 就赋值，没有则点「上传图片」走主世界桥。
    // 豆包没有这个函数，仍只给已有 input 赋值，不点菜单项。
    const located = typeof Page.attachLocalImage === 'function'
      ? await Page.attachLocalImage(document, file, {
          bridge: typeof Page.createChooserBridge === 'function' ? Page.createChooserBridge(document) : null,
          cancelled: function () { return cancelRequested },
        })
      : await assignExistingInput(file)
    if (located.error) return XBS.fail(env, located.code || XBS.errors.E_SELECTOR_MISS, located.error)
    // 文件已经交给页面。重试只复查预览，避免同一张图再挂一次。
    part.assigned = true
  }
  const attached = await waitUntil(Number(payload.timeoutMs) || 30000, function () {
    if (typeof Page.attachmentRejected === 'function' && Page.attachmentRejected(document)) return 'rejected'
    return Page.freshAttachment(document, part.baseline)
  }, 2)
  if (attached === 'cancel') return XBS.fail(env, XBS.errors.E_CANCELLED, '上传等待已取消')
  if (attached === 'rejected') {
    return XBS.fail(env, XBS.errors.E_UPLOAD_FAIL, '输入区显示上传失败或加载失败，源图未就绪')
  }
  if (attached !== true) {
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

// 豆包（以及没有主世界桥的页面）只给已经出现的 file input 赋值。
async function assignExistingInput(file) {
  const located = await locateFileInput()
  if (located.error) return located
  if (!Page.assignFiles(located.input, [file])) {
    return { error: '无法把文件写入 input[type=file]', code: XBS.errors.E_UPLOAD_FAIL }
  }
  return located
}

// 先找输入区旁的 file input（可以是隐藏的）。菜单已经开着、并且选项里面嵌着 input 时直接用它，
// 避免再点一次入口把菜单关掉。没有 input 时才点入口（千问「添加附件」，豆包 upload_file_button）。
// 不点击单独的「上传图片」或「上传文件或图片」菜单项，否则会弹出系统文件框，扩展填不进去。
async function locateFileInput() {
  const comp = Page.composer(document)
  let input = Page.fileInput(document, comp)
  if (input) return { input }
  const nestedNow = nestedUploadInput()
  if (nestedNow) return { input: nestedNow }
  const entry = Page.uploadEntry(document, comp)
  if (!entry) return { error: '未找到当前输入区上传入口', code: XBS.errors.E_SELECTOR_MISS }
  try { entry.click() } catch (e) {}
  const deadline = Date.now() + 2000
  while (Date.now() < deadline) {
    if (cancelRequested) return { error: '上传已取消', code: XBS.errors.E_CANCELLED }
    input = Page.fileInput(document, Page.composer(document))
    if (input) return { input }
    const nested = nestedUploadInput()
    if (nested) return { input: nested }
    await delay(100)
  }
  return { error: '未找到输入区的文件选择框（上传入口旁没有 input[type=file]）', code: XBS.errors.E_SELECTOR_MISS }
}

function nestedUploadInput() {
  const item = Page.uploadImageItem(document)
  if (!item || typeof item.querySelector !== 'function') return null
  return item.querySelector('input[type="file"]')
}

async function setPrompt(env, payload) {
  const ready = await readyComposer(8000)
  if (ready === 'cancel') return XBS.fail(env, XBS.errors.E_CANCELLED, '等待输入框已取消')
  if (!ready || ready === 'login') return loginOrMiss(env, payload.name, '未找到可写入提示词的输入框')
  const comp = ready
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
    if (prev.phase === 'captcha') {
      const again = Page.detectCaptcha(document)
      if (again.blocked) return captchaAck(env, again, captchaSendMessage(again))
      if (prev.clicked) {
        if (Page.sendAccepted(document, pageMark)) {
          prev.phase = 'ok'
          return XBS.ack(env, { name: payload.name, ok: true, deduped: true })
        }
        return XBS.fail(env, XBS.errors.E_SEND_UNCERTAIN, '验证码已消失，但发送状态仍未确认，未再次点击')
      }
    } else if (prev.phase === 'uncertain') {
      return XBS.fail(env, XBS.errors.E_SEND_UNCERTAIN, prev.message || '发送状态未确认，未再次点击')
    }
  }
  const ready = await readyComposer(8000)
  if (ready === 'cancel') return XBS.fail(env, XBS.errors.E_CANCELLED, '等待输入框已取消')
  if (!ready || ready === 'login') return loginOrMiss(env, payload.name, '未找到可写入提示词的输入框')
  const comp = ready
  const prompt = String(payload.prompt || '')
  if (Page.normalize(Page.readComposer(comp)) !== Page.normalize(prompt)) {
    return XBS.fail(env, XBS.errors.E_SELECTOR_MISS, '输入区内容与本次提示词不一致，未发送')
  }
  const button = await waitFor(function () { return Page.sendButton(document, Page.composer(document)) }, 5000)
  if (button === 'cancel') return XBS.fail(env, XBS.errors.E_CANCELLED, '发送已取消')
  if (!button) return XBS.fail(env, XBS.errors.E_SELECTOR_MISS, '未找到已就绪的发送按钮')
  // 点发送之前记边界，和策略里的 MarkTask 一样，避免把源图回显当成结果。
  pageMark = Page.mark(document, prompt)
  const rec = { phase: 'clicking', message: '', clicked: false }
  const job = confirmSend(env, payload, button, rec)
  rec.promise = job
  if (key) sendState.set(key, rec)
  return job
}

function pressSend(button) {
  // 千问 page.js 提供 activate：先 pointer 再 click，Radix 发送按钮不会只吃到 click。
  // 豆包没有这个函数，仍只 click，上传菜单路径也不从这里走。
  if (button && typeof Page.activate === 'function') return Page.activate(button) !== false
  try { button.click(); return true } catch (e) { return false }
}

function captchaSendMessage(found) {
  if (found && found.baxia) return '发送后出现阿里云滑块验证（Baxia），需要人工处理'
  return '发送后出现验证码，需要人工处理'
}

// 豆包没有这些函数，这里直接放行，发送仍只 click。
// 千问：失败或未解码的预览不点发送。有可用源图时先进入「AI生图」，再重取发送按钮。
async function prepareAttachedSend(payload) {
  if (typeof Page.sendBlockReason !== 'function' && typeof Page.composerHasAttachment !== 'function') return null
  const reason = typeof Page.sendBlockReason === 'function' ? Page.sendBlockReason(document) : ''
  if (reason) return { code: XBS.errors.E_UPLOAD_FAIL, message: reason }
  const hasAttachment = typeof Page.composerHasAttachment === 'function' && Page.composerHasAttachment(document)
  if (!hasAttachment || typeof Page.ensureImageMode !== 'function') return null
  const switched = Page.ensureImageMode(document)
  if (!switched || switched.error) {
    return {
      code: (switched && switched.code) || XBS.errors.E_SELECTOR_MISS,
      message: (switched && switched.error) || '无法切换到「AI生图」，未发送',
    }
  }
  if (typeof Page.imageModeActive === 'function' && !Page.imageModeActive(document)) {
    const active = await waitUntil(2000, function () { return Page.imageModeActive(document) }, 1)
    if (active !== true) {
      return { code: XBS.errors.E_SELECTOR_MISS, message: '点击「AI生图」后模式没有保持选中，未发送' }
    }
  }
  const prompt = String((payload && payload.prompt) || '')
  const comp = Page.composer(document)
  if (Page.normalize(Page.readComposer(comp)) !== Page.normalize(prompt)) {
    // 切到「AI生图」会重绘输入框。再写一次，让 Slate 的文本状态和箭头按钮一起恢复。
    if (typeof Page.setComposer !== 'function' || !Page.setComposer(comp, prompt)) {
      return { code: XBS.errors.E_SELECTOR_MISS, message: '切换模式后输入区内容与提示词不一致，未发送' }
    }
  }
  if (typeof Page.composerHasAttachment === 'function' && !Page.composerHasAttachment(document)) {
    return { code: XBS.errors.E_UPLOAD_FAIL, message: '切换模式后源图不在输入区，未发送' }
  }
  if (typeof Page.sendBlockReason === 'function') {
    const again = Page.sendBlockReason(document)
    if (again) return { code: XBS.errors.E_UPLOAD_FAIL, message: again }
  }
  return { retarget: true }
}

async function confirmSend(env, payload, button, rec) {
  const already = Page.detectCaptcha(document)
  if (already.blocked) {
    rec.phase = 'captcha'
    rec.clicked = false
    rec.promise = null
    rec.message = captchaSendMessage(already)
    return captchaAck(env, already, rec.message)
  }
  const prepared = await prepareAttachedSend(payload)
  if (prepared && prepared.message) {
    // 还没点发送。不能记成 uncertain，否则同一条幂等键会拒绝再点。
    rec.phase = 'blocked'
    rec.clicked = false
    rec.promise = null
    rec.message = prepared.message
    return XBS.fail(env, prepared.code || XBS.errors.E_UPLOAD_FAIL, rec.message)
  }
  let target = button
  if (prepared && prepared.retarget) {
    // 胶囊切换后箭头会先灰掉再亮。只看一眼会把还没就绪的按钮当成找不到。
    target = await waitFor(function () { return Page.sendButton(document, Page.composer(document)) }, 5000)
    if (target === 'cancel') {
      rec.phase = 'blocked'
      rec.clicked = false
      rec.promise = null
      rec.message = '发送已取消'
      return XBS.fail(env, XBS.errors.E_CANCELLED, rec.message)
    }
    if (!target) {
      rec.phase = 'blocked'
      rec.clicked = false
      rec.promise = null
      rec.message = '切换模式后未找到已就绪的发送按钮，未发送'
      return XBS.fail(env, XBS.errors.E_SELECTOR_MISS, rec.message)
    }
  }
  if (!pressSend(target)) {
    rec.phase = 'uncertain'
    rec.clicked = false
    rec.message = '点击发送失败'
    return XBS.fail(env, XBS.errors.E_SEND_UNCERTAIN, rec.message)
  }
  rec.clicked = true
  const deadline = Date.now() + (Number(payload.timeoutMs) || 15000)
  while (Date.now() < deadline) {
    if (cancelRequested) {
      rec.phase = 'uncertain'
      rec.promise = null
      rec.message = '发送已点击，取消时尚未确认平台接受'
      return XBS.fail(env, XBS.errors.E_SEND_UNCERTAIN, rec.message)
    }
    const found = Page.detectCaptcha(document)
    if (found.blocked) {
      rec.phase = 'captcha'
      rec.promise = null
      rec.message = captchaSendMessage(found)
      return captchaAck(env, found, rec.message)
    }
    if (Page.sendAccepted(document, pageMark)) {
      rec.phase = 'ok'
      rec.promise = null
      return XBS.ack(env, { name: payload.name, ok: true })
    }
    await delay(300)
  }
  const late = Page.detectCaptcha(document)
  if (late.blocked) {
    rec.phase = 'captcha'
    rec.promise = null
    rec.message = captchaSendMessage(late)
    return captchaAck(env, late, rec.message)
  }
  rec.phase = 'uncertain'
  rec.promise = null
  rec.message = '平台未接受消息：发送后输入仍未清空'
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
    const images = Page.resultImages(document, pageMark).filter(function (item) {
      return item && item.url && !(typeof Page.isPlaceholderURL === 'function' && Page.isPlaceholderURL(item.url))
    })
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
  if (cached && !XBS.looksLikeRaster(cached)) {
    downloadCache.delete(url)
    cached = null
  }
  if (!cached) {
    const fetched = await fetchImage(url)
    if (fetched.error) return XBS.fail(env, XBS.errors.E_DOWNLOAD_FAIL, fetched.error)
    if (fetched.path) {
      return XBS.ack(env, { name: payload.name, ok: true, path: fetched.path, done: true, total: 0 })
    }
    if (!XBS.looksLikeRaster(fetched.bytes)) {
      return XBS.fail(env, XBS.errors.E_DOWNLOAD_FAIL, '下载内容不是 PNG、JPEG、WebP 或 GIF')
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
  let rejectedBody = false
  const modes = ['include', 'omit']
  for (let i = 0; i < modes.length; i++) {
    try {
      const res = await fetch(url, { credentials: modes[i], cache: 'no-store' })
      if (!res.ok) continue
      const bytes = new Uint8Array(await res.arrayBuffer())
      if (XBS.looksLikeRaster(bytes)) return { bytes }
      rejectedBody = true
    } catch (e) {}
  }
  if (rejectedBody) return { error: '下载内容不是 PNG、JPEG、WebP 或 GIF: ' + safeURL(url) }
  try {
    const saved = await chrome.runtime.sendMessage({ type: 'FETCH_URL', url })
    if (saved && saved.bytesBase64) {
      const bytes = Page.base64ToBytes(saved.bytesBase64)
      if (!XBS.looksLikeRaster(bytes)) return { error: '下载内容不是 PNG、JPEG、WebP 或 GIF: ' + safeURL(url) }
      return { bytes }
    }
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

function captchaAck(env, found, message) {
  const crop = Page.captchaCrop(found && found.root)
  const name = (env && env.payload && env.payload.name) || XBS.commands.captchaAssist
  return XBS.ack(env, {
    name,
    ok: false,
    code: XBS.errors.E_CAPTCHA_HUMAN,
    message: message || '页面出现验证码，需要人工处理',
    captchaBlocked: true,
    kind: (found && found.kind) || 'unknown',
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

// needStable 次连续命中 true 才算数，用来躲开附件预览刚插入时的抖动。
// 其它真值（例如「上传失败」）立刻返回，不当成就绪。
async function waitUntil(timeout, fn, needStable) {
  const deadline = Date.now() + timeout
  let stable = 0
  while (Date.now() < deadline) {
    if (cancelRequested) return 'cancel'
    const value = fn()
    if (value === true) {
      stable++
      if (stable >= needStable) return true
    } else if (value) {
      return value
    } else {
      stable = 0
    }
    await delay(300)
  }
  return false
}
