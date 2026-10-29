package cdp

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestLoginQRScriptFindsDialogCode(t *testing.T) {
	for _, needle := range []string{"canvas", "二维码", "二维码已过期", "二维码已失效", "点击刷新", "scrollIntoView", "qrcode", "inDialog"} {
		if !strings.Contains(qrFindJS, needle) && !strings.Contains(qrRefreshJS, needle) {
			t.Fatalf("qr script missing %q", needle)
		}
	}
	for _, needle := range []string{
		"登录", "立即登录", "登录/注册", "扫码登录", "二维码登录",
		`[data-testid*="login" i]`,
		`[data-testid*="signin" i]`,
		`[class*="login-btn" i]`,
		`[class*="LoginButton"]`,
		`[class*="header-login" i]`,
		`[class*="btn-login" i]`,
		`[aria-label="登录"]`,
		"kind:kind",
	} {
		if !strings.Contains(qrOpenJS, needle) {
			t.Fatalf("login opener missing %q", needle)
		}
	}
}

func TestLoginQRClip(t *testing.T) {
	clip, ok := loginQRClip(12, 40, 180, 180)
	if !ok || clip["x"] != 12.0 || clip["width"] != 180.0 || clip["scale"] != 1 {
		t.Fatalf("clip = %#v ok=%v", clip, ok)
	}
	clip, ok = loginQRClip(-10, -4, 100, 90)
	if !ok || clip["x"] != 0.0 || clip["y"] != 0.0 || clip["width"] != 90.0 || clip["height"] != 86.0 {
		t.Fatalf("clamped = %#v ok=%v", clip, ok)
	}
	if _, ok := loginQRClip(0, 0, 10, 10); ok {
		t.Fatal("tiny box should be dropped")
	}
}

func TestDecideQRClicksBeforeTimeout(t *testing.T) {
	if got := decideQR(qrBox{}, false, 0, qrAppearWait); got != "open" {
		t.Fatalf("missing code should open login, got %s", got)
	}
	if got := decideQR(qrBox{OK: true}, false, 0, qrAppearWait); got != "capture" {
		t.Fatalf("visible code should be captured, got %s", got)
	}
	if got := decideQR(qrBox{Expired: true}, true, 0, qrAppearWait); got != "refresh" {
		t.Fatalf("expired code should refresh, got %s", got)
	}
	if got := decideQR(qrBox{}, true, time.Second, qrAppearWait); got != "wait" {
		t.Fatalf("after the click we still wait, got %s", got)
	}
	if got := decideQR(qrBox{}, true, qrAppearWait, qrAppearWait); got != "timeout" {
		t.Fatalf("deadline should time out, got %s", got)
	}
}

func TestShouldClickLoginOnce(t *testing.T) {
	if !shouldClickLogin("login", false, false) || shouldClickLogin("login", true, false) {
		t.Fatal("login entry is clicked once")
	}
	if !shouldClickLogin("scan", true, false) || shouldClickLogin("scan", true, true) {
		t.Fatal("scan tab is a second click")
	}
	if shouldClickLogin("", false, false) || shouldClickLogin("other", false, false) {
		t.Fatal("unknown target is not clicked")
	}
}

func TestLoginClickPoint(t *testing.T) {
	x, y, ok := loginClickPoint(900, 16, 64, 32)
	if !ok || x != 932 || y != 32 {
		t.Fatalf("point = %v,%v ok=%v", x, y, ok)
	}
	if _, _, ok := loginClickPoint(0, 0, 4, 4); ok {
		t.Fatal("tiny target should be ignored")
	}
}

func TestLoginQRHint(t *testing.T) {
	if LoginQRHint(nil) != "" {
		t.Fatal("nil error has no hint")
	}
	if !errors.Is(ErrLoginQR, ErrLoginQR) || LoginQRHint(ErrLoginQR) != ErrLoginQR.Error() || !strings.Contains(LoginQRHint(errors.New("boom")), "登录") {
		t.Fatal("hint should be the Chinese login prompt")
	}
}

func TestLoginOpenerPicksDoubaoAndQianwen(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	script := qrOpenHarness + "\nconst src = " + jsString(qrOpenJS) + ";\n" + qrOpenCases
	out, err := exec.Command("node", "-e", script).CombinedOutput()
	if err != nil {
		t.Fatalf("opener script: %v\n%s", err, out)
	}
}

func jsString(src string) string {
	return "`" + strings.ReplaceAll(src, "`", "") + "`"
}

const qrOpenHarness = `
function el(tag, attrs, rect, text, kids) {
  return {
    tagName: String(tag || '').toUpperCase(),
    attrs: attrs || {},
    className: (attrs && attrs.class) || '',
    id: (attrs && attrs.id) || '',
    rect: rect || {x:0,y:0,width:80,height:32},
    text: text || '',
    children: kids || [],
    parentElement: null,
    disabled: false,
    get innerText() { return this.text },
    getAttribute(name) { return this.attrs[name] || null },
    getBoundingClientRect() { return this.rect },
    scrollIntoView() {}
  }
}
function link(parent, child) {
  child.parentElement = parent
  parent.children.push(child)
  return child
}
function matchesOne(node, sel) {
  sel = sel.trim()
  if (!sel) return false
  let tag = ''
  let rest = sel
  const tm = sel.match(/^([a-zA-Z0-9_-]+)/)
  if (tm && !sel.startsWith('[')) {
    tag = tm[1].toUpperCase()
    rest = sel.slice(tm[1].length)
  }
  if (tag && node.tagName !== tag) return false
  const attrs = [...rest.matchAll(/\[([^\]]+)\]/g)]
  if (!tag && attrs.length === 0) return false
  for (const a of attrs) {
    const m = a[1].match(/^([a-zA-Z0-9_-]+)(\*)?="([^"]*)"(?:\s+([iI]))?$/)
    if (!m) return false
    let got = m[1] === 'class' ? String(node.className || '') : String(node.attrs[m[1]] || '')
    let want = m[3]
    if (m[4]) { got = got.toLowerCase(); want = want.toLowerCase() }
    if (m[2] ? !got.includes(want) : got !== want) return false
  }
  return true
}
function matches(node, selector) {
  return selector.split(',').some((part) => matchesOne(node, part))
}
function queryAll(root, selector) {
  const out = []
  ;(function walk(node) {
    if (node !== root && matches(node, selector)) out.push(node)
    for (const child of node.children || []) walk(child)
  })(root)
  return out
}
function mount(root) {
  global.window = { innerWidth: 1280 }
  global.getComputedStyle = (node) => node.style || { visibility: 'visible', display: 'block', opacity: '1' }
  global.document = {
    children: [root],
    querySelectorAll(selector) { return queryAll(this, selector) }
  }
  root.parentElement = null
}
function run() { return eval(src) }
`

const qrOpenCases = `
function expect(name, hit, kind, x) {
  if (!hit || !hit.ok || hit.kind !== kind || Math.abs(hit.x - x) > 1) {
    throw new Error(name + ' ' + JSON.stringify(hit))
  }
}
{
  const root = el('div', {}, {x:0,y:0,width:1280,height:800}, '')
  const header = link(root, el('button', {}, {x:1100,y:16,width:64,height:32}, '登录'))
  link(root, el('a', {}, {x:20,y:700,width:80,height:28}, '登录'))
  link(root, el('button', {}, {x:900,y:16,width:96,height:32}, '退出登录'))
  mount(root)
  expect('doubao header', run(), 'login', header.rect.x)
}
{
  const root = el('div', {}, {x:0,y:0,width:1280,height:800}, '')
  const btn = link(root, el('div', {class:'btn-login'}, {x:1080,y:20,width:88,height:36}, '立即登录'))
  mount(root)
  expect('qianwen login', run(), 'login', btn.rect.x)
}
{
  const root = el('div', {}, {x:0,y:0,width:1280,height:800}, '')
  const btn = link(root, el('div', {'data-testid':'header-login-entry'}, {x:1000,y:18,width:40,height:40}, ''))
  mount(root)
  expect('testid fallback', run(), 'login', btn.rect.x)
}
{
  const root = el('div', {class:'login-modal', role:'dialog'}, {x:360,y:120,width:560,height:480}, '扫码登录 手机号登录')
  link(root, el('button', {}, {x:1100,y:16,width:64,height:32}, '登录'))
  const tab = link(root, el('div', {role:'tab'}, {x:420,y:150,width:88,height:32}, '扫码登录'))
  link(root, el('div', {role:'tab'}, {x:520,y:150,width:88,height:32}, '手机号登录'))
  mount(root)
  expect('scan tab', run(), 'scan', tab.rect.x)
}
{
  const root = el('div', {}, {x:0,y:0,width:1280,height:800}, '')
  link(root, el('button', {}, {x:1000,y:16,width:96,height:32}, '退出登录'))
  mount(root)
  const hit = run()
  if (hit && hit.ok) throw new Error('logout was clicked ' + JSON.stringify(hit))
}
`
