// 与 internal/extbridge 的 v1 信封共用字段名、命令名和错误码。
globalThis.XBS = {
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
  },
  stubCommands: ['upload', 'setPrompt', 'send', 'waitResult', 'download', 'captchaAssist'],
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
  },
}

function xbsNewId() {
  if (globalThis.crypto && crypto.randomUUID) return crypto.randomUUID()
  return 'id-' + Date.now().toString(16) + Math.random().toString(16).slice(2)
}

globalThis.XBS.newId = xbsNewId

globalThis.XBS.envelope = function envelope(type, fields) {
  const env = {
    v: globalThis.XBS.version,
    id: xbsNewId(),
    type,
    ts: Date.now(),
    platform: globalThis.XBS.platform,
  }
  if (fields) Object.assign(env, fields)
  return env
}

globalThis.XBS.hello = function hello(token) {
  return globalThis.XBS.envelope('hello', {
    payload: { token: token || '', version: '0.1.0' },
  })
}

globalThis.XBS.reply = function reply(request, type, payload) {
  return {
    v: globalThis.XBS.version,
    id: request && request.id,
    type,
    ts: Date.now(),
    platform: (request && request.platform) || globalThis.XBS.platform,
    sessionId: request && request.sessionId,
    taskId: request && request.taskId,
    idempotencyKey: request && request.idempotencyKey,
    payload,
  }
}

globalThis.XBS.ack = function ack(request, payload) {
  return globalThis.XBS.reply(request, 'ack', payload)
}

globalThis.XBS.fail = function fail(request, code, message) {
  const name = request && request.payload && request.payload.name
  return globalThis.XBS.ack(request, {
    name,
    ok: false,
    code,
    message,
  })
}
