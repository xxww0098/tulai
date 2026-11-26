package strategy

import (
	"os/exec"
	"strings"
	"testing"

	extbundle "github.com/xxww0098/tulai/extension"
)

func TestHookSyncsWindowDoubaoTurnAndComposerDetectsTurnFaults(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not installed")
	}
	// 在 Node vm 中真实执行 hook.js + refuse.js + composer.js，验证 __tulaiDoubaoTurn 同步与复用回合额度/审核识别。
	script := `
const assert = require('assert');
const vm = require('vm');
const hookJS = process.env.HOOK_JS;
const refuseJS = process.env.REFUSE_JS;
const composerJS = process.env.COMPOSER_JS;

let replyText = '';
const ctx = vm.createContext({
  location: { hostname: 'www.doubao.com', href: 'https://www.doubao.com/chat/reuse-1' },
  URL,
  setTimeout,
  clearTimeout,
  XMLHttpRequest: class { open() {} send() {} },
  MutationObserver: class { observe() {} },
  document: {
    readyState: 'complete',
    body: { innerText: '' },
    images: [],
    addEventListener() {},
    querySelectorAll() { return []; },
  },
  __tulaiWashPage: {
    mark(doc, prompt) { return { prompt, turnId: 42 }; },
    newAssistants() { return []; },
    revealResults() {},
    assistantText(doc, turn) { return turn && turn.turnId === 42 ? replyText : ''; },
  },
});
ctx.window = ctx;
ctx.globalThis = ctx;
vm.runInContext(hookJS, ctx);
vm.runInContext(refuseJS, ctx);
vm.runInContext(composerJS, ctx);

ctx.__tulaiMarkTask();
assert.strictEqual(ctx.__tulaiDoubaoTurn, null, 'MarkTask 必须重置 window.__tulaiDoubaoTurn');
assert.strictEqual(ctx.__tulaiMarkDoubaoTurn('把主体高清重绘'), true);
assert.ok(ctx.__tulaiDoubaoTurn && ctx.__tulaiDoubaoTurn.turnId === 42, 'MarkDoubaoTurn 必须同步赋值 window.__tulaiDoubaoTurn');

replyText = '今日图片生成免费次数已用完，请明天再试。';
let state = ctx.tulaiPageState();
assert.strictEqual(state.quota, true, '复用回合的额度耗尽回复必须被 tulaiPageState 识别');

replyText = '内容不符合相关规范，暂时无法为你生成图片。';
state = ctx.tulaiPageState();
assert.ok(state.moderation && state.moderation.length > 0, '复用回合的审核拒绝回复必须被 tulaiPageState 识别');

ctx.__tulaiMarkTask();
assert.strictEqual(ctx.__tulaiDoubaoTurn, null, '下一轮 MarkTask 必须清掉上一轮的 window.__tulaiDoubaoTurn');
console.log('doubao turn state ok');
`
	cmd := exec.Command("node", "-e", script)
	cmd.Env = append(cmd.Environ(),
		"HOOK_JS="+hookJS,
		"REFUSE_JS="+extbundle.RefuseJS(),
		"COMPOSER_JS="+composerJS,
	)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "doubao turn state ok") {
		t.Fatalf("node verification failed: err=%v out=%s", err, string(out))
	}
}

func TestFreshLandedAcceptsSPAChatTransition(t *testing.T) {
	if !freshLanded("spa", "old-id", chatReady{Ready: true, ID: "", Node: "n1"}) {
		t.Fatal("SPA fresh transition to empty chat id should be accepted")
	}
	if freshLanded("spa", "old-id", chatReady{Ready: false, ID: "old-id", Node: "n1"}) {
		t.Fatal("unready SPA state must not be accepted")
	}
}

func TestComposerCleanRemovesFullTenImageWashBatch(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not installed")
	}
	script := `
const assert = require('assert');
const vm = require('vm');
const composerJS = process.env.COMPOSER_JS;

let clicks = 0;
const dels = Array.from({ length: 10 }, (_, i) => ({ id: 'del-' + i }));
const comp = {
  tagName: 'TEXTAREA',
  offsetParent: {},
  clientWidth: 200,
  clientHeight: 40,
  disabled: false,
  parentElement: null,
  getAttribute() { return ''; },
};
const scope = {
  tagName: 'DIV',
  hasAttribute(name) { return name === 'data-chat-input-body'; },
  querySelectorAll() { return dels; },
  parentElement: null,
};
comp.parentElement = scope;

const ctx = vm.createContext({
  setTimeout,
  clearTimeout,
  getComputedStyle() { return { visibility: 'visible', display: 'block' }; },
  tulaiClick(el) { clicks++; return Promise.resolve(true); },
  document: {
    body: {},
    documentElement: {},
    querySelectorAll(sel) { return sel.includes('chat_input_input') ? [comp] : []; },
  },
});
ctx.window = ctx;
ctx.globalThis = ctx;
vm.runInContext(composerJS, ctx);

ctx.tulaiCleanComposer().then((ok) => {
  assert.strictEqual(ok, true);
  assert.strictEqual(clicks, 10, 'tulaiCleanComposer 必须清理满批 10 张附件，实际清理 ' + clicks);
  console.log('clean 10 ok');
}).catch((err) => { console.error(err); process.exit(1); });
`
	cmd := exec.Command("node", "-e", script)
	cmd.Env = append(cmd.Environ(), "COMPOSER_JS="+composerJS)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "clean 10 ok") {
		t.Fatalf("node verification failed: err=%v out=%s", err, string(out))
	}
}
