package captcha

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/xxww0098/tulai/internal/logging"
)

func TestVerificationVisibilityReadOnlyNodeFixture(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("需要 Node.js 运行隔离 DOM fixture")
	}
	script := `const assert=require('node:assert/strict'),vm=require('node:vm');
const source=` + strconv.Quote(verificationExpression) + `;
for(const state of ['absent','none','visible','hidden','zero']){
 let actions=0;
 const el={getBoundingClientRect:()=>({width:state==='zero'?0:300,height:40}),click(){actions++},dispatchEvent(){actions++}};
 const window=state==='absent'?{}:{__tulaiCaptcha:{detect:()=>state==='none'?null:el}};
 const result=vm.runInNewContext(source,{window,getComputedStyle:()=>({display:state==='hidden'?'none':'block',visibility:'visible',opacity:'1'})});
 assert.equal(result,state==='visible',state);assert.equal(actions,0,state+' must not alter challenge');
}`
	scriptPath := filepath.Join(t.TempDir(), "verify_fixture.cjs")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatalf("write verification fixture: %v", err)
	}
	if out, err := exec.Command(node, scriptPath).CombinedOutput(); err != nil {
		t.Fatalf("read-only verification fixture: %v\n%s", err, out)
	}
}

func TestBlockingReportsLogOnlyChanges(t *testing.T) {
	start := len(logging.Log.Entries())
	handle := blockingHandler()
	for _, state := range []string{
		`{"blocked":true,"reason":"captcha"}`, `{"blocked":true,"reason":"captcha"}`,
		`{"blocked":true,"reason":"login"}`, `{"blocked":true,"reason":"login"}`,
		`{"blocked":false,"reason":""}`, `{"blocked":true,"reason":"captcha"}`,
	} {
		handle(state)
	}
	count := 0
	for _, entry := range logging.Log.Entries()[start:] {
		if strings.HasPrefix(entry.Text, "页面阻塞中:") {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("repeated blocking reports logged %d times, want 3 changes", count)
	}
}
