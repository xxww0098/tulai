// 用豆包返回的参考图 URI 关联结果；缺失或有歧义时交给主机保留为待核对。
;(function () {
  function imageKey(raw) {
    try {
      const u = new URL(raw)
      if (u.protocol === 'https:' && /(^|\.)byteimg\.com$/i.test(u.hostname)) return u.pathname.split('~tplv-')[0]
    } catch (e) {}
    return ''
  }

  function collectLists(state) {
    const queue = [state], seen = new WeakSet(), lists = []
    let visited = 0
    while (queue.length && visited++ < 100000) {
      const value = queue.pop()
      if (!value || typeof value !== 'object' || seen.has(value)) continue
      seen.add(value)
      if (Array.isArray(value.message_list)) lists.push(value.message_list)
      Object.values(value).forEach(child => { if (child && typeof child === 'object') queue.push(child) })
    }
    return queue.length ? null : lists
  }

  function analyzeList(list, names) {
    let latest = null, ambiguous = false, invalidSequence = false
    const refs = new Map()
    const keySeqs = new Map()
    const uriOwners = new Map()
    const items = []
    for (const message of list) {
      const msgSeq = /^\d{1,32}$/.test(String(message && message.index || '')) ? BigInt(String(message.index)) : null
      for (const block of Array.isArray(message && message.content_block) ? message.content_block : []) {
        const content = block && block.content || {}
        const attachments = content.attachment_block && content.attachment_block.attachments
        if (Array.isArray(attachments) && attachments.length) {
          if (msgSeq === null) { invalidSequence = true; continue }
          const group = new Map()
          for (const entry of attachments) {
            const image = entry && entry.image
            const index = image && names.indexOf(image.name)
            if (index >= 0 && typeof image.uri === 'string' && image.uri) group.set(image.uri, (group.get(image.uri) || []).concat(index + 1))
          }
          const candidate = attachments.length === names.length && group.size === names.length && [...group.values()].every(indices => indices.length === 1) ? group : null
          if (!latest || msgSeq > latest.order) { latest = { order: msgSeq, group: candidate }; ambiguous = false }
          else if (msgSeq === latest.order) ambiguous = true
        }
        const creations = content.creation_block && content.creation_block.creations
        for (const creation of Array.isArray(creations) ? creations : []) {
          const image = creation && creation.image, params = image && image.gen_params
          const rawUrl = (image && image.image_ori_raw && image.image_ori_raw.url) || ''
          const oriUrl = (image && image.image_ori && image.image_ori.url) || ''
          const previewUrl = (image && image.image_preview && image.image_preview.url) || ''
          const imageKeys = new Set([rawUrl, oriUrl, previewUrl].map(imageKey).filter(Boolean))
          const valid = imageKeys.size === 1 && params && Array.isArray(params.img_uris) && params.img_uris.length === 1 &&
            typeof params.img_uris[0] === 'string' && params.img_uris[0] && (!params.img_uri || params.img_uri === params.img_uris[0])
          for (const key of imageKeys) {
            const uris = refs.get(key) || new Set()
            uris.add(valid ? params.img_uris[0] : '')
            refs.set(key, uris)
            if (valid && msgSeq !== null) {
              const seqs = keySeqs.get(key) || []
              seqs.push(msgSeq)
              keySeqs.set(key, seqs)
            }
            if (params && Array.isArray(params.img_uris)) {
              for (const uri of params.img_uris) {
                if (typeof uri === 'string' && uri) {
                  const owners = uriOwners.get(uri) || []
                  owners.push({ key, seq: msgSeq })
                  uriOwners.set(uri, owners)
                }
              }
            }
          }
          if (valid && msgSeq !== null) {
            const key = [...imageKeys][0]
            const bestUrl = rawUrl || oriUrl || previewUrl
            if (bestUrl) items.push({ seq: msgSeq, key, url: bestUrl, rawUrl })
          }
        }
      }
    }
    return { latest, ambiguous, invalidSequence, refs, keySeqs, uriOwners, items }
  }

  function matchLists(lists, names, keys) {
    let matched = null
    let winning = null
    for (const list of lists) {
      const analysis = analyzeList(list, names)
      const { latest, ambiguous, invalidSequence, refs, keySeqs, uriOwners } = analysis
      if (latest && latest.group && !ambiguous && !invalidSequence) {
        const group = latest.group
        const indices = keys.map(key => {
          const seqs = keySeqs.get(key)
          if (!seqs || !seqs.some(seq => seq > latest.order)) return 0
          const uris = refs.get(key)
          if (!uris || uris.size !== 1) return 0
          const uri = [...uris][0]
          const entries = uriOwners.get(uri) || []
          const activeOwners = new Set()
          for (const entry of entries) {
            if (entry.seq === null || entry.seq > latest.order) activeOwners.add(entry.key)
          }
          if (activeOwners.size !== 1) return 0
          const found = group.get(uri)
          return found && found.length === 1 ? found[0] : 0
        })
        if (!indices.includes(0) && new Set(indices).size === keys.length) {
          if (matched && matched.some((index, i) => index !== indices[i])) return null
          matched = indices
          winning = analysis
        }
      }
    }
    return matched ? { indices: matched, winning } : null
  }

  function match(state, names, images) {
    if (!Array.isArray(names) || names.length < 2 || new Set(names).size !== names.length ||
        !Array.isArray(images) || images.length < 1 || images.length > names.length) return null
    const keys = images.map(item => imageKey(item && (item.rawUrl || item.url)))
    if (keys.some(key => !key) || new Set(keys).size !== keys.length) return null
    const lists = collectLists(state)
    if (!lists) return null
    const res = matchLists(lists, names, keys)
    return res ? res.indices : null
  }

  function resolve(state, names, images) {
    if (!Array.isArray(names) || names.length < 2 || new Set(names).size !== names.length ||
        !Array.isArray(images) || images.length < 1 || images.length > names.length) return null
    const keys = images.map(item => imageKey(item && (item.rawUrl || item.url)))
    if (keys.some(key => !key) || new Set(keys).size !== keys.length) return null
    const lists = collectLists(state)
    if (!lists) return null
    const base = matchLists(lists, names, keys)
    if (!base) return null
    if (images.length < names.length && base.winning && base.winning.latest) {
      const present = new Set(keys)
      const extraImages = []
      const extraKeys = []
      for (const item of base.winning.items) {
        if (item.seq <= base.winning.latest.order || present.has(item.key)) continue
        present.add(item.key)
        extraImages.push({ url: item.url, rawUrl: item.rawUrl || undefined })
        extraKeys.push(item.key)
      }
      if (extraImages.length > 0 && images.length + extraImages.length <= names.length) {
        const candidateKeys = keys.concat(extraKeys)
        const merged = matchLists(lists, names, candidateKeys)
        if (merged && merged.indices.slice(0, images.length).every((v, i) => v === base.indices[i])) {
          return { images: images.concat(extraImages), indices: merged.indices }
        }
      }
    }
    return { images, indices: base.indices }
  }

  function parse(html) {
    if (typeof html !== 'string' || html.length > 8 * 1024 * 1024) return null
    const doc = new DOMParser().parseFromString(html, 'text/html')
    const script = Array.from(doc.scripts).map(node => node.textContent || '').find(text => /^\s*window\._ROUTER_DATA\s*=/.test(text))
    if (!script) return null
    try { return JSON.parse(script.slice(script.indexOf('=') + 1).replace(/;\s*$/, '')) } catch (e) { return null }
  }

  function validConversationURL(conversation) {
    try {
      const url = new URL(conversation)
      if (url.protocol !== 'https:' || !/(^|\.)doubao\.com$/i.test(url.hostname) || !/^\/chat\/[^/]+$/.test(url.pathname) || url.search || url.hash) return null
      return url.href
    } catch (e) {
      return null
    }
  }

  async function resolveFromConversation(conversation, names, images) {
    const href = validConversationURL(conversation)
    if (!href) return null
    let best = null
    for (let attempt = 0; attempt < 3; attempt++) {
      const controller = new AbortController()
      const timer = setTimeout(() => controller.abort(), 4000)
      try {
        const response = await fetch(href, { credentials: 'include', cache: 'no-store', redirect: 'error', signal: controller.signal })
        if (response.ok) {
          const resolved = resolve(parse(await response.text()), names, images)
          if (resolved) {
            best = resolved
            if (resolved.images.length === names.length) return resolved
          }
        }
      } catch (e) {} finally { clearTimeout(timer) }
      if (attempt < 2) await new Promise(resolveDelay => setTimeout(resolveDelay, 600))
    }
    return best
  }

  async function fromConversation(conversation, names, images) {
    const href = validConversationURL(conversation)
    if (!href) return null
    for (let attempt = 0; attempt < 3; attempt++) {
      const controller = new AbortController()
      const timer = setTimeout(() => controller.abort(), 4000)
      try {
        const response = await fetch(href, { credentials: 'include', cache: 'no-store', redirect: 'error', signal: controller.signal })
        if (response.ok) {
          const indices = match(parse(await response.text()), names, images)
          if (indices) return indices
        }
      } catch (e) {} finally { clearTimeout(timer) }
      if (attempt < 2) await new Promise(resolveDelay => setTimeout(resolveDelay, 600))
    }
    return null
  }

  globalThis.TULAIDoubaoSourceMap = { match, resolve, parse, fromConversation, resolveFromConversation }
})()
