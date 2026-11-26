const assert = require('assert')
const QwenHistory = require('./qwen_history.js')

class El {
  constructor(tag, attrs = {}, top = 0) {
    this.tagName = tag.toUpperCase()
    this.attrs = { ...attrs }
    this.children = []
    this.parentElement = null
    this.isConnected = true
    this.innerText = ''
    this.textContent = ''
    this.rect = { left: 0, top, width: 120, height: 32 }
    this.style = { display: '', visibility: 'visible', opacity: '1', pointerEvents: 'auto', transform: '' }
    this.disabled = false
    this.scrollTop = 0
    this.scrollHeight = 300
    this.clientHeight = 300
  }
  getAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k) ? this.attrs[k] : null }
  setAttribute(k, v) { this.attrs[k] = String(v) }
  append(child) { child.parentElement = this; this.children.push(child); return child }
  addText(t) { this.innerText += t; this.textContent += t; return this }
  getBoundingClientRect() { return this.rect }
  matches(sel) {
    return sel.split(',').some(part => {
      const s = part.trim()
      if (!s) return false
      const tagMatch = /^([a-z0-9]+)/i.exec(s)
      if (tagMatch && this.tagName !== tagMatch[1].toUpperCase()) return false
      const classMatches = [...s.matchAll(/\.([a-z0-9_-]+)/gi)]
      const cls = String(this.getAttribute('class') || '')
      for (const m of classMatches) {
        if (!cls.split(/\s+/).includes(m[1])) return false
      }
      const attrMatches = [...s.matchAll(/\[([^\]=]+)(?:="([^"]*)")?\]/g)]
      for (const m of attrMatches) {
        const val = this.getAttribute(m[1])
        if (m[2] === undefined) {
          if (val === null) return false
        } else if (val !== m[2]) {
          return false
        }
      }
      return true
    })
  }
  querySelectorAll(sel) {
    const out = []
    const walk = node => {
      for (const c of node.children) {
        if (c.isConnected === false) continue
        if (c.matches(sel)) out.push(c)
        walk(c)
      }
    }
    walk(this)
    return out
  }
  querySelector(sel) { return this.querySelectorAll(sel)[0] || null }
  click() {}
}

global.getComputedStyle = el => el.style
global.KeyboardEvent = function (type, opts) { this.type = type; Object.assign(this, opts) }

async function run() {
  const doc = {
    body: new El('body'),
    querySelector(sel) { return this.body.querySelector(sel) },
    querySelectorAll(sel) { return this.body.querySelectorAll(sel) },
    dispatchEvent() {},
  }
  const left = doc.body.append(new El('div', { 'data-testid': 'qianwen-layout-left-panel' }))
  const list = left.append(new El('div', { role: 'list', class: 'sider-scrollbar' }))

  const deleted = []
  function addSession(id, title, top, active) {
    const wrap = list.append(new El('div', { 'data-react-window-index': String(top / 38) }, top))
    const row = wrap.append(new El('div', {
      'data-session-id': id,
      'data-session-switch-target': 'normal',
      'data-session-active': active ? 'true' : 'false',
    }, top)).addText(title)
    const more = row.append(new El('button', {
      'data-session-id': id,
      'data-session-switch-target': 'session-tools',
      'aria-haspopup': 'menu',
    }, top))
    more.click = () => {
      const menu = doc.body.append(new El('div', { role: 'menu', 'data-state': 'open' }))
      menu.append(new El('div', { role: 'menuitem' })).addText('重命名')
      const del = menu.append(new El('div', { role: 'menuitem' })).addText('删除')
      del.click = () => {
        menu.isConnected = false
        doc.body.children = doc.body.children.filter(c => c !== menu)
        const dlg = doc.body.append(new El('div', { role: 'dialog', 'data-state': 'open' }))
        dlg.append(new El('button')).addText('取消')
        const ok = dlg.append(new El('button')).addText('确认删除')
        ok.click = () => {
          dlg.isConnected = false
          doc.body.children = doc.body.children.filter(c => c !== dlg)
          row.isConnected = false
          wrap.isConnected = false
          list.children = list.children.filter(c => c !== wrap)
          deleted.push(id)
        }
      }
    }
  }

  // 故意打乱 DOM 插入顺序以验证 react-window 按视觉 top 排序
  addSession('s2', '第二条橘猫', 312, false)
  addSession('s1', '第一条水彩', 274, true)
  addSession('s3', '第三条海报', 350, false)

  const ordered = QwenHistory.conversationItems(doc)
  assert.deepStrictEqual(ordered.map(i => i.id), ['s1', 's2', 's3'], 'react-window 条目必须按视觉顺序排序')

  // keepRecent=1 应保留视觉第一条 s1，依次删 s2、s3
  const r1 = await QwenHistory.deleteNextHistoryItem(doc, [], 1, null, 2000)
  assert.strictEqual(r1.ok, true, JSON.stringify(r1))
  assert.strictEqual(r1.id, 's2')

  const r2 = await QwenHistory.deleteNextHistoryItem(doc, [], 1, null, 2000)
  assert.strictEqual(r2.ok, true)
  assert.strictEqual(r2.id, 's3')

  const r3 = await QwenHistory.deleteNextHistoryItem(doc, [], 1, null, 2000)
  assert.strictEqual(r3.ok, true)
  assert.strictEqual(r3.done, true)
  assert.deepStrictEqual(deleted, ['s2', 's3'])

  // 单条删除 s1
  const r4 = await QwenHistory.deleteConversation(doc, 's1', null, 2000)
  assert.strictEqual(r4.ok, true)
  assert.deepStrictEqual(deleted, ['s2', 's3', 's1'])

  console.log('qwen history ok')
}

run().catch(err => { console.error(err); process.exitCode = 1 })
