package cdp

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestEgoConsoleMessagesNodeFixture(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("需要普通 Node.js；不调用 ego-browser")
	}
	ctx, cancel := context.WithTimeout(context.Background(), nodeFixtureTimeout)
	defer cancel()
	source := egoSDKJS + `
const assert = await import('node:assert/strict');
const consoleEvent = (...args) => ({method:'Runtime.consoleAPICalled', params:{args}});
const owned = consoleEvent({value:'[验证码] 开始识别'});
const events = [
 ...Array.from({length:600}, () => consoleEvent({type:'object',description:'Error'})),
 consoleEvent({value:'website warning'}),
 consoleEvent({description:'Error: website failure'}),
 consoleEvent(),
 {method:'Runtime.consoleAPICalled'},
 {method:'Runtime.bindingCalled',params:{name:'tulaiBridge',payload:'[验证码] binding'}},
 {event:'error',error:'SDK request failed'},
 owned, owned,
 consoleEvent({value:'[验证码]'}, {value:'识别完成'}, {value:0}),
 consoleEvent({description:'[验证码] 需要人工处理'}),
];
assert.deepEqual(egoConsoleMessages(events),[
 '[验证码] 开始识别',
 '[验证码] 识别完成 0',
 '[验证码] 需要人工处理',
]);
assert.deepEqual(egoConsoleMessages([owned]),['[验证码] 开始识别']);
console.log('console filter fixture passed');
`
	output, err := exec.CommandContext(ctx, node, "--input-type=module", "-e", source).CombinedOutput()
	if err != nil || !strings.Contains(string(output), "console filter fixture passed") {
		t.Fatalf("pure Node console fixture: %v: %s", err, output)
	}
}
