// 桌面端在 127.0.0.1:18764/tulai-pair 上公布当前令牌和桥端口。
// 扩展取不到时保留弹出页里手填的值。
;(function () {
  const url = 'http://127.0.0.1:18764/tulai-pair'

  function parse(body) {
    if (!body || body.product !== 'tulai' || typeof body.token !== 'string' || !body.token.trim()) {
      throw new Error('pair payload')
    }
    const port = Number(body.port)
    if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error('pair port')
    return { token: body.token.trim(), port }
  }

  async function pull(fetchImpl) {
    const res = await fetchImpl(url, { cache: 'no-store' })
    if (!res || !res.ok) throw new Error('pair http')
    return parse(await res.json())
  }

  async function identity(storage) {
    const got = await storage.get({ profileId: '' })
    if (got && got.profileId) return { profileId: String(got.profileId) }
    const profileId = globalThis.crypto && crypto.randomUUID
      ? crypto.randomUUID()
      : 'p-' + Date.now().toString(16) + Math.random().toString(16).slice(2)
    await storage.set({ profileId })
    return { profileId }
  }

  globalThis.TULAIPair = { url, parse, pull, identity }
})()
