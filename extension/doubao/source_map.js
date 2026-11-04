// 用豆包返回的参考图 URI 关联结果；缺失或有歧义时交给主机保留为待核对。
;(function () {
  function imageKey(raw) {
    try {
      const u = new URL(raw)
      if (u.protocol === 'https:' && /(^|\.)byteimg\.com$/i.test(u.hostname)) return u.pathname.split('~tplv-')[0]
    } catch (e) {}
    return ''
  }

  function match(state, names, images) {
    if (!Array.isArray(names) || names.length < 2 || new Set(names).size !== names.length ||
        !Array.isArray(images) || images.length !== names.length) return null
    const keys = images.map(item => imageKey(item && (item.rawUrl || item.url)))
    if (keys.some(key => !key) || new Set(keys).size !== keys.length) return null
    const queue = [state], seen = new WeakSet(), lists = []
    let visited = 0
    while (queue.length && visited++ < 100000) {
      const value = queue.pop()
      if (!value || typeof value !== 'object' || seen.has(value)) continue
      seen.add(value)
      if (Array.isArray(value.message_list)) lists.push(value.message_list)
      Object.values(value).forEach(child => { if (child && typeof child === 'object') queue.push(child) })
    }
    if (queue.length) return null
    let matched = null
    for (const list of lists) {
      let latest = null, ambiguous = false, invalidSequence = false
      const refs = new Map()
      for (const message of list) {
        for (const block of Array.isArray(message && message.content_block) ? message.content_block : []) {
          const content = block && block.content || {}
          const attachments = content.attachment_block && content.attachment_block.attachments
          if (Array.isArray(attachments) && attachments.length) {
            const sequence = String(message.index || '')
            if (!/^\d{1,32}$/.test(sequence)) { invalidSequence = true; continue }
            const group = new Map()
            for (const entry of attachments) {
              const image = entry && entry.image
              const index = image && names.indexOf(image.name)
              if (index >= 0 && typeof image.uri === 'string' && image.uri) group.set(image.uri, (group.get(image.uri) || []).concat(index + 1))
            }
            const order = BigInt(sequence)
            const candidate = attachments.length === names.length && group.size === names.length && [...group.values()].every(indices => indices.length === 1) ? group : null
            if (!latest || order > latest.order) { latest = { order, group: candidate }; ambiguous = false }
            else if (order === latest.order) ambiguous = true
          }
          const creations = content.creation_block && content.creation_block.creations
          for (const creation of Array.isArray(creations) ? creations : []) {
            const image = creation && creation.image, params = image && image.gen_params
            const imageKeys = new Set(['image_ori_raw', 'image_ori', 'image_preview'].map(name => imageKey(image && image[name] && image[name].url)).filter(Boolean))
            const valid = imageKeys.size === 1 && params && Array.isArray(params.img_uris) && params.img_uris.length === 1 &&
              typeof params.img_uris[0] === 'string' && params.img_uris[0] && (!params.img_uri || params.img_uri === params.img_uris[0])
            for (const key of imageKeys) {
              const uris = refs.get(key) || new Set()
              uris.add(valid ? params.img_uris[0] : ''); refs.set(key, uris)
            }
          }
        }
      }
      if (latest && latest.group && !ambiguous && !invalidSequence) {
        const group = latest.group
        const indices = keys.map(key => {
          const uris = refs.get(key)
          if (!uris || uris.size !== 1) return 0
          const found = group.get([...uris][0])
          return found && found.length === 1 ? found[0] : 0
        })
        if (!indices.includes(0) && new Set(indices).size === names.length) {
          if (matched && matched.some((index, i) => index !== indices[i])) return null
          matched = indices
        }
      }
    }
    return matched
  }

  function parse(html) {
    if (typeof html !== 'string' || html.length > 8 * 1024 * 1024) return null
    const doc = new DOMParser().parseFromString(html, 'text/html')
    const script = Array.from(doc.scripts).map(node => node.textContent || '').find(text => /^\s*window\._ROUTER_DATA\s*=/.test(text))
    if (!script) return null
    try { return JSON.parse(script.slice(script.indexOf('=') + 1).replace(/;\s*$/, '')) } catch (e) { return null }
  }

  async function fromConversation(conversation, names, images) {
    let url
    try { url = new URL(conversation) } catch (e) { return null }
    if (url.protocol !== 'https:' || !/(^|\.)doubao\.com$/i.test(url.hostname) || !/^\/chat\/[^/]+$/.test(url.pathname) || url.search || url.hash) return null
    for (let attempt = 0; attempt < 3; attempt++) {
      const controller = new AbortController()
      const timer = setTimeout(() => controller.abort(), 4000)
      try {
        const response = await fetch(url.href, { credentials: 'include', cache: 'no-store', redirect: 'error', signal: controller.signal })
        if (response.ok) {
          const indices = match(parse(await response.text()), names, images)
          if (indices) return indices
        }
      } catch (e) {} finally { clearTimeout(timer) }
      if (attempt < 2) await new Promise(resolve => setTimeout(resolve, 600))
    }
    return null
  }

  globalThis.TULAIDoubaoSourceMap = { match, parse, fromConversation }
})()
