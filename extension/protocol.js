// 与 internal/extbridge 的 v1 信封共用字段名、命令名和错误码。
globalThis.TULAI = {
  version: 1,
  platform: 'qianwen',
  pingEveryMs: 15000,
  commands: {
    ping: 'ping',
    helloAck: 'helloAck',
    upload: 'upload',
    setPrompt: 'setPrompt',
    send: 'send',
    waitResult: 'waitResult',
    download: 'download',
    captchaAssist: 'captchaAssist',
    cancel: 'cancel',
    status: 'status',
    reload: 'reload',
  },
  pageCommands: ['upload', 'setPrompt', 'send', 'waitResult', 'download', 'captchaAssist'],
  errors: {
    E_NOT_CONNECTED: 'E_NOT_CONNECTED',
    E_NO_TAB: 'E_NO_TAB',
    E_NOT_LOGGED_IN: 'E_NOT_LOGGED_IN',
    E_SELECTOR_MISS: 'E_SELECTOR_MISS',
    E_UPLOAD_FAIL: 'E_UPLOAD_FAIL',
    E_SEND_UNCERTAIN: 'E_SEND_UNCERTAIN',
    E_TIMEOUT: 'E_TIMEOUT',
    E_CAPTCHA_HUMAN: 'E_CAPTCHA_HUMAN',
    E_CAPTCHA_SOLVE_FAIL: 'E_CAPTCHA_SOLVE_FAIL',
    E_DOWNLOAD_FAIL: 'E_DOWNLOAD_FAIL',
    E_SW_SLEEP: 'E_SW_SLEEP',
    E_PROTOCOL: 'E_PROTOCOL',
    E_AUTH: 'E_AUTH',
    E_CANCELLED: 'E_CANCELLED',
    E_QUOTA: 'E_QUOTA',
    E_RATE_LIMIT: 'E_RATE_LIMIT',
    E_MODERATION: 'E_MODERATION',
  },
}

function tulaiNewId() {
  if (globalThis.crypto && crypto.randomUUID) return crypto.randomUUID()
  return 'id-' + Date.now().toString(16) + Math.random().toString(16).slice(2)
}

globalThis.TULAI.newId = tulaiNewId

globalThis.TULAI.envelope = function envelope(type, fields) {
  const env = {
    v: globalThis.TULAI.version,
    id: tulaiNewId(),
    type,
    ts: Date.now(),
    platform: globalThis.TULAI.platform,
  }
  if (fields) Object.assign(env, fields)
  return env
}

// platform 省略时仍是千问，和原先单个套接字的 hello 一样。豆包套接字传入 'doubao'。
// account 是这个浏览器配置文件的稳定 id，旧扩展可以不传。
// loggedIn 只有布尔值才写上。没探测到登录时不要传，桌面端不会因此建账号。
globalThis.TULAI.hello = function hello(token, platform, account, loggedIn) {
  const payload = { token: token || '', version: '0.3.10' }
  if (account) payload.account = String(account)
  if (typeof loggedIn === 'boolean') payload.loggedIn = loggedIn
  const env = globalThis.TULAI.envelope('hello', { payload })
  if (platform) env.platform = platform
  return env
}

globalThis.TULAI.reply = function reply(request, type, payload) {
  return {
    v: globalThis.TULAI.version,
    id: request && request.id,
    type,
    ts: Date.now(),
    platform: (request && request.platform) || globalThis.TULAI.platform,
    sessionId: request && request.sessionId,
    taskId: request && request.taskId,
    idempotencyKey: request && request.idempotencyKey,
    payload,
  }
}

globalThis.TULAI.ack = function ack(request, payload) {
  return globalThis.TULAI.reply(request, 'ack', payload)
}

globalThis.TULAI.fail = function fail(request, code, message) {
  const name = request && request.payload && request.payload.name
  return globalThis.TULAI.ack(request, {
    name,
    ok: false,
    code,
    message,
  })
}

// 与 internal/images.IsValidImageBytes 同一组魔数：PNG / JPEG / GIF / WebP。
globalThis.TULAI.looksLikeRaster = function looksLikeRaster(bytes) {
  if (!bytes || bytes.length < 3) return false
  if (bytes.length >= 8 && bytes[0] === 0x89 && bytes[1] === 0x50 && bytes[2] === 0x4E && bytes[3] === 0x47 && bytes[4] === 0x0D && bytes[5] === 0x0A && bytes[6] === 0x1A && bytes[7] === 0x0A) return true
  if (bytes[0] === 0xFF && bytes[1] === 0xD8 && bytes[2] === 0xFF) return true
  if (bytes.length >= 3 && bytes[0] === 0x47 && bytes[1] === 0x49 && bytes[2] === 0x46) return true
  if (bytes.length >= 12 && bytes[0] === 0x52 && bytes[1] === 0x49 && bytes[2] === 0x46 && bytes[3] === 0x46 && bytes[8] === 0x57 && bytes[9] === 0x45 && bytes[10] === 0x42 && bytes[11] === 0x50) return true
  return false
}
