// 用豆包返回的参考图 URI 关联结果；缺失或有歧义时交给主机保留为待核对。
;(function () {
  function imageKey(raw) {
    try {
      const u = new URL(raw)
      if (u.protocol === 'https:' && !u.username && !u.password && !u.port && /(^|\.)byteimg\.com$/i.test(u.hostname)) {
        return u.pathname.split('~tplv-')[0]
      }
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
          const sourceIndices = [...group.values()].flat()
          const candidate = attachments.length === names.length && group.size === names.length &&
            sourceIndices.length === names.length && new Set(sourceIndices).size === names.length ? group : null
          if (!latest || msgSeq > latest.order) { latest = { order: msgSeq, group: candidate }; ambiguous = false }
          else if (msgSeq === latest.order) ambiguous = true
        }
        const creations = content.creation_block && content.creation_block.creations
        for (const creation of Array.isArray(creations) ? creations : []) {
          const image = creation && creation.image, params = image && image.gen_params
          const raw = image && image.image_ori_raw && image.image_ori_raw.url
          const urls = [raw, image && image.image_ori && image.image_ori.url, image && image.image_preview && image.image_preview.url].filter(imageKey)
          const rawUrl = imageKey(raw) ? raw : ''
          const imageKeys = new Set(urls.map(imageKey))
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
            items.push({ seq: msgSeq, key, url: urls[0], rawUrl })
          }
        }
      }
    }
    return { latest, ambiguous, invalidSequence, refs, keySeqs, uriOwners, items }
  }

  function sourceIndex(analysis, key) {
    const { latest, refs, keySeqs, uriOwners } = analysis
    if (!keySeqs.get(key)?.some(seq => seq > latest.order)) return 0
    const uris = refs.get(key)
    if (!uris || uris.size !== 1) return 0
    const uri = [...uris][0]
    const entries = uriOwners.get(uri) || []
    const activeOwners = new Set(entries.filter(entry => entry.seq === null || entry.seq > latest.order).map(entry => entry.key))
    if (activeOwners.size !== 1) return 0
    return latest.group.get(uri)?.[0] || 0
  }

  function matchLists(lists, names, keys) {
    let matched = null
    let winning = null
    for (const list of lists) {
      const analysis = analyzeList(list, names)
      const { latest, ambiguous, invalidSequence } = analysis
      if (latest && latest.group && !ambiguous && !invalidSequence) {
        const indices = keys.map(key => sourceIndex(analysis, key))
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

  // 没有页面上的图卡时，只从会话里唯一对应本批附件的消息列表恢复。
  function recover(state, names) {
    if (!Array.isArray(names) || names.length < 2 || new Set(names).size !== names.length) return null
    const lists = collectLists(state)
    if (!lists) return null
    const matched = lists.map(list => analyzeList(list, names)).filter(analysis =>
      analysis.latest?.group && !analysis.ambiguous && !analysis.invalidSequence)
    if (matched.length !== 1) return null
    const analysis = matched[0]
    const bySource = new Map()
    for (const item of analysis.items) {
      if (item.seq <= analysis.latest.order) continue
      const index = sourceIndex(analysis, item.key)
      if (index && (!bySource.has(index) || item.rawUrl)) bySource.set(index, item)
    }
    const indices = [...bySource.keys()].sort((a, b) => a - b)
    if (!indices.length) return null
    const images = indices.map(index => {
      const item = bySource.get(index)
      return { url: item.url, rawUrl: item.rawUrl || undefined }
    })
    return { images, indices }
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
      if (url.protocol !== 'https:' || url.username || url.password || url.port || !/(^|\.)doubao\.com$/i.test(url.hostname) ||
          !/^\/chat\/[^/]+$/.test(url.pathname) || url.search || url.hash) return null
      return url.href
    } catch (e) {
      return null
    }
  }

  async function readConversation(href) {
    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), 4000)
    try {
      const response = await fetch(href, { credentials: 'include', cache: 'no-store', redirect: 'error', signal: controller.signal })
      return response.ok ? parse(await response.text()) : null
    } catch (e) {
      return null
    } finally {
      clearTimeout(timer)
    }
  }

  async function recoverFromConversation(conversation, names) {
    const href = validConversationURL(conversation)
    return href ? recover(await readConversation(href), names) : null
  }

  async function resolveFromConversation(conversation, names, images) {
    const href = validConversationURL(conversation)
    if (!href) return null
    let best = null
    for (let attempt = 0; attempt < 3; attempt++) {
      const resolved = resolve(await readConversation(href), names, images)
      if (resolved) {
        best = resolved
        if (resolved.images.length === names.length) return resolved
      }
      if (attempt < 2) await new Promise(resolveDelay => setTimeout(resolveDelay, 600))
    }
    return best
  }

  async function fromConversation(conversation, names, images) {
    const href = validConversationURL(conversation)
    if (!href) return null
    for (let attempt = 0; attempt < 3; attempt++) {
      const indices = match(await readConversation(href), names, images)
      if (indices) return indices
      if (attempt < 2) await new Promise(resolveDelay => setTimeout(resolveDelay, 600))
    }
    return null
  }

  // 从流式 SSE 捕获的附件与生成项直接构建严格 1-to-1 映射，跳过 8MB SSR 页面抓包。
  // 任何附件不全、URI 缺失或一对多歧义都返回 null，由调用方回退到完整对话核对。
  function analyzeSSE(sseState, names) {
    if (!sseState || !Array.isArray(names) || names.length < 2 || new Set(names).size !== names.length) return null
    const attachments = Array.isArray(sseState.attachments) ? sseState.attachments : []
    const creations = Array.isArray(sseState.creations) ? sseState.creations : []
    if (!attachments.length || !creations.length) return null
    const group = new Map()
    const seenNames = new Set()
    for (const entry of attachments) {
      if (!entry || typeof entry.name !== 'string' || typeof entry.uri !== 'string' || !entry.uri) continue
      const idx = names.indexOf(entry.name)
      if (idx < 0) continue
      if (seenNames.has(entry.name)) {
        // 同一文件名在流中多次出现但 URI 不同则有歧义。
        if (!group.get(entry.uri) || group.get(entry.uri)[0] !== idx + 1) return null
        continue
      }
      seenNames.add(entry.name)
      group.set(entry.uri, (group.get(entry.uri) || []).concat(idx + 1))
    }
    const sourceIndices = [...group.values()].flat()
    if (group.size !== names.length || sourceIndices.length !== names.length || new Set(sourceIndices).size !== names.length) {
      return null
    }
    const refs = new Map()
    const uriOwners = new Map()
    const byKey = new Map()
    for (const creation of creations) {
      const raw = creation && creation.rawUrl
      const url = creation && creation.url
      const key = imageKey(raw || url)
      if (!key) continue
      const imgUris = Array.isArray(creation.imgUris) ? creation.imgUris : []
      const imgUri = typeof creation.imgUri === 'string' ? creation.imgUri : ''
      const valid = imgUris.length === 1 && typeof imgUris[0] === 'string' && imgUris[0] && (!imgUri || imgUri === imgUris[0])
      const uris = refs.get(key) || new Set()
      uris.add(valid ? imgUris[0] : '')
      refs.set(key, uris)
      for (const uri of imgUris) {
        if (typeof uri === 'string' && uri) {
          const owners = uriOwners.get(uri) || new Set()
          owners.add(key)
          uriOwners.set(uri, owners)
        }
      }
      if (valid) {
        const prev = byKey.get(key)
        if (!prev || (!prev.rawUrl && imageKey(raw))) {
          byKey.set(key, { key, url: url || raw, rawUrl: imageKey(raw) ? raw : '' })
        }
      }
    }
    function indexFor(key) {
      const uris = refs.get(key)
      if (!uris || uris.size !== 1) return 0
      const uri = [...uris][0]
      if (!uri) return 0
      const owners = uriOwners.get(uri)
      if (!owners || owners.size !== 1) return 0
      return group.get(uri)?.[0] || 0
    }
    return { indexFor, byKey }
  }

  function resolveFromSSE(sseState, names, images) {
    if (!Array.isArray(images) || images.length < 1 || !Array.isArray(names) || images.length > names.length) return null
    const keys = images.map(item => imageKey(item && (item.rawUrl || item.url)))
    if (keys.some(key => !key) || new Set(keys).size !== keys.length) return null
    const analysis = analyzeSSE(sseState, names)
    if (!analysis) return null
    const indices = keys.map(key => analysis.indexFor(key))
    if (indices.includes(0) || new Set(indices).size !== keys.length) return null
    const present = new Set(keys)
    const outImages = images.map((item, i) => {
      const fromSSE = analysis.byKey.get(keys[i])
      if (fromSSE && fromSSE.rawUrl && !item.rawUrl) {
        return { url: item.url, rawUrl: fromSSE.rawUrl }
      }
      return item
    })
    const outIndices = indices.slice()
    if (outImages.length < names.length) {
      const usedSources = new Set(outIndices)
      for (const [key, item] of analysis.byKey) {
        if (present.has(key)) continue
        const idx = analysis.indexFor(key)
        if (!idx || usedSources.has(idx)) continue
        present.add(key)
        usedSources.add(idx)
        outImages.push({ url: item.url, rawUrl: item.rawUrl || undefined })
        outIndices.push(idx)
      }
    }
    if (outImages.length > names.length || new Set(outIndices).size !== outIndices.length) return null
    return { images: outImages, indices: outIndices }
  }

  function recoverFromSSE(sseState, names) {
    const analysis = analyzeSSE(sseState, names)
    if (!analysis) return null
    const bySource = new Map()
    for (const [key, item] of analysis.byKey) {
      const idx = analysis.indexFor(key)
      if (idx && (!bySource.has(idx) || item.rawUrl)) bySource.set(idx, item)
    }
    const indices = [...bySource.keys()].sort((a, b) => a - b)
    if (!indices.length) return null
    const images = indices.map(idx => {
      const item = bySource.get(idx)
      return { url: item.url, rawUrl: item.rawUrl || undefined }
    })
    return { images, indices }
  }

  function parseBotState(ext) {
    if (!ext || typeof ext !== 'object') return { agentPsm: '', agentName: '', useCreation: false, intention: '' }
    let botState = ext.bot_state
    if (typeof botState === 'string' && botState.trim()) {
      try { botState = JSON.parse(botState) } catch (e) { botState = null }
    }
    const obj = botState && typeof botState === 'object' ? botState : null
    return {
      agentPsm: String((obj && obj.agent_psm) || ext.agent_psm || '').trim(),
      agentName: String((obj && obj.agent_name) || ext.agent_name || '').trim(),
      useCreation: String(ext.use_creation || '').trim() === '1',
      intention: String(ext.intention || '').trim(),
    }
  }

  function messageSeq(message) {
    return /^\d{1,32}$/.test(String(message && message.index || '')) ? BigInt(String(message.index)) : null
  }

  function isUserMessage(message) {
    if (!message || typeof message !== 'object') return false
    if (message.user_type === 1 || message.user_type === '1') return true
    if (message.user_type === 2 || message.user_type === '2') return false
    const blocks = Array.isArray(message.content_block) ? message.content_block : []
    return blocks.some(block => Array.isArray(block && block.content && block.content.attachment_block && block.content.attachment_block.attachments) && block.content.attachment_block.attachments.length > 0)
  }

  function isAssistantMessage(message) {
    if (!message || typeof message !== 'object') return false
    if (message.user_type === 2 || message.user_type === '2') return true
    if (message.user_type === 1 || message.user_type === '1') return false
    const ext = message.ext && typeof message.ext === 'object' ? message.ext : null
    if (ext && (ext.bot_state || ext.agent_psm || ext.agent_name || ext.use_creation || ext.intention)) return true
    const blocks = Array.isArray(message.content_block) ? message.content_block : []
    return blocks.some(block => {
      const content = block && block.content
      return !!(content && (content.creation_block || content.code_block))
    })
  }

  function selectTurnAssistants(list, names) {
    const withSeq = list.map(message => ({ message, seq: messageSeq(message) }))
    if (Array.isArray(names) && names.length >= 2) {
      const analysis = analyzeList(list, names)
      if (!analysis.latest || !analysis.latest.group || analysis.ambiguous || analysis.invalidSequence) return null
      let nextUserOrder = null
      for (const item of withSeq) {
        if (item.seq !== null && item.seq > analysis.latest.order && isUserMessage(item.message)) {
          if (nextUserOrder === null || item.seq < nextUserOrder) nextUserOrder = item.seq
        }
      }
      return withSeq
        .filter(item => item.seq !== null && item.seq > analysis.latest.order && (nextUserOrder === null || item.seq < nextUserOrder) && isAssistantMessage(item.message))
        .map(item => item.message)
    }
    let maxUserSeq = null
    for (const item of withSeq) {
      if (item.seq !== null && isUserMessage(item.message)) {
        if (maxUserSeq === null || item.seq > maxUserSeq) maxUserSeq = item.seq
      }
    }
    if (maxUserSeq !== null) {
      return withSeq
        .filter(item => item.seq !== null && item.seq > maxUserSeq && isAssistantMessage(item.message))
        .map(item => item.message)
    }
    const assistants = withSeq.filter(item => isAssistantMessage(item.message))
    if (!assistants.length) return []
    let maxAsstSeq = null
    for (const item of assistants) {
      if (item.seq !== null && (maxAsstSeq === null || item.seq > maxAsstSeq)) maxAsstSeq = item.seq
    }
    if (maxAsstSeq !== null) {
      return assistants.filter(item => item.seq === maxAsstSeq).map(item => item.message)
    }
    return [assistants[assistants.length - 1].message]
  }

  function summarizeTurnRoute(assistants) {
    if (!Array.isArray(assistants) || !assistants.length) return null
    let hasCreations = false
    let hasCodeBlock = false
    let sawCreationRoute = false
    let sawShellRoute = false
    let shellName = ''
    let seenPsm = ''
    let seenName = ''
    const texts = []
    for (const message of assistants) {
      const route = parseBotState(message && message.ext)
      const psmLower = route.agentPsm.toLowerCase()
      if (route.agentPsm && !seenPsm) seenPsm = route.agentPsm
      if (route.agentName && !seenName) seenName = route.agentName
      if (psmLower === 'flow.agent.creation' || route.useCreation || route.intention === 'agent_creation') {
        sawCreationRoute = true
      } else if (psmLower === 'flow.agent.shell') {
        sawShellRoute = true
        if (route.agentName && !shellName) shellName = route.agentName
      }
      for (const block of Array.isArray(message && message.content_block) ? message.content_block : []) {
        const content = block && block.content || {}
        const creations = content.creation_block && content.creation_block.creations
        if (Array.isArray(creations) && creations.length > 0) hasCreations = true
        if (content.code_block) hasCodeBlock = true
        const text = content.text_block && typeof content.text_block.text === 'string'
          ? content.text_block.text.replace(/\s+/g, ' ').trim()
          : ''
        if (text) texts.push(text)
      }
    }
    const textPreview = texts.join(' ').slice(0, 120)
    if (hasCreations || sawCreationRoute) {
      return { degraded: false, agentPsm: 'flow.agent.creation', agentName: 'Agent-Creation', hasCreations, hasCodeBlock, textPreview }
    }
    if (sawShellRoute) {
      return { degraded: true, agentPsm: 'flow.agent.shell', agentName: shellName || '豆包', hasCreations: false, hasCodeBlock, textPreview }
    }
    return { degraded: false, agentPsm: seenPsm, agentName: seenName, hasCreations: false, hasCodeBlock, textPreview }
  }

  // 检查豆包当前回合是否被服务端静默改道至纯文本代理 flow.agent.shell（剥夺生图 Agent-Creation 路由）。
  function inspectRoute(state, names) {
    const lists = collectLists(state)
    if (!lists || !lists.length) return null
    const multi = Array.isArray(names) && names.length >= 2
    const summaries = []
    for (const list of lists) {
      const assistants = selectTurnAssistants(list, names)
      if (assistants === null) continue
      const summary = summarizeTurnRoute(assistants)
      if (summary) summaries.push(summary)
    }
    if (multi) return summaries.length === 1 ? summaries[0] : null
    if (!summaries.length) return null
    if (summaries.length > 1) {
      const first = summaries[0]
      if (summaries.some(item => item.degraded !== first.degraded || item.agentPsm !== first.agentPsm)) return null
    }
    return summaries[0]
  }

  async function inspectRouteFromConversation(conversation, names) {
    const href = validConversationURL(conversation)
    if (!href) return null
    const state = await readConversation(href)
    return state ? inspectRoute(state, names) : null
  }

  globalThis.TULAIDoubaoSourceMap = { match, resolve, recover, parse, fromConversation, resolveFromConversation, recoverFromConversation, resolveFromSSE, recoverFromSSE, inspectRoute, inspectRouteFromConversation }
})()
