package captcha

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"xbs/internal/logging"
)

func TestPreparePagePreservesVisibleVerificationBeforeRenew(t *testing.T) {
	for _, auto := range []bool{false, true} {
		page := &preparePageFixture{ego: true, installed: true, revision: "old", blocked: true, autoSolve: auto}
		if err := PreparePage(page); !errors.Is(err, ErrVerificationRequired) || page.renewals != 0 || page.manualCalls != 1 {
			t.Fatalf("auto=%v error=%v renew=%d manual=%d", auto, err, page.renewals, page.manualCalls)
		}
	}
}

func TestVerificationRequiredRespectsAutomaticModeAndCancellation(t *testing.T) {
	for _, auto := range []bool{false, true} {
		page := &preparePageFixture{blocked: true, autoSolve: auto}
		err := CheckVerificationRequired(page)
		if errors.Is(err, ErrVerificationRequired) == auto || page.manualCalls != map[bool]int{false: 1, true: 0}[auto] {
			t.Fatalf("auto=%v error=%v manual=%d", auto, err, page.manualCalls)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	page := &preparePageFixture{ctx: ctx, ego: true, installed: true, blocked: true, afterRead: cancel}
	if err := PreparePage(page); !errors.Is(err, context.Canceled) || errors.Is(err, ErrVerificationRequired) || page.renewals != 0 || page.manualCalls != 0 {
		t.Fatalf("cancellation must precede manual/renew: error=%v page=%+v", err, page)
	}
}

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
 const window=state==='absent'?{}:{__xbsCaptcha:{detect:()=>state==='none'?null:el}};
 const result=vm.runInNewContext(source,{window,getComputedStyle:()=>({display:state==='hidden'?'none':'block',visibility:'visible',opacity:'1'})});
 assert.equal(result,state==='visible',state);assert.equal(actions,0,state+' must not alter challenge');
}`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
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
