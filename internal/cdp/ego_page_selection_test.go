package cdp

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestEgoInitPageSelectionNodeFixture(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("需要普通 Node.js；不调用 ego-browser")
	}
	start := strings.Index(egoBridgeJS, "const tabs = await task.tabs();")
	if start < 0 {
		t.Fatal("init tab selection was not found")
	}
	end := strings.Index(egoBridgeJS[start:], "      if (existing) {")
	if end < 0 {
		t.Fatal("init tab selection boundary was not found")
	}
	source := "async function selectPage(task, params) {\n" + egoBridgeJS[start:start+end] + "\nreturn existing;\n}\n" + `
const assert = await import('node:assert/strict');
const tab = (label, extra={}) => ({label, page:label, url:'https://www.qianwen.com/chat', ...extra});
async function check(tabs, expected, expectedReads, expectedAdopts=[]) {
 const reads=[], adopts=[], original=[...tabs];
 const pageFor=item=>({
  label:item.label??item.page,
  evaluate:async fn=>{
   reads.push(item.label??item.page);
   if(item.error)throw item.error;
   globalThis.sessionStorage={getItem:key=>{assert.equal(key,'__tulaiRetiredHookPage');return item.retired?'1':null}};
   return fn();
  },
 });
 const task={
  tabs:async()=>tabs,
  page:label=>pageFor(tabs.find(item=>item.label===label)),
  adopt:async page=>{adopts.push(page);return pageFor(tabs.find(item=>item.page===page))},
 };
 const selected=await selectPage(task,{hosts:['qianwen.com','www.qianwen.com','www.tongyi.com']});
 assert.equal(selected?.label,expected);
 assert.deepEqual(reads,expectedReads);
 assert.deepEqual(adopts,expectedAdopts);
 assert.deepEqual(tabs,original,'selection must not reorder SDK tab inventory');
}
await check([tab('old'),tab('validated',{active:true})],'validated',['validated']);
await check([tab('old'),tab('retired',{active:true,retired:true})],'old',['retired','old']);
await check([tab('old'),tab('foreign',{active:true,url:'https://example.com'}),tab('invalid',{active:true,url:'bad URL'})],'old',['old']);
await check([tab('first'),tab('second')],'first',['first']);
await check([tab('inactive'),tab('first-active',{active:true}),tab('second-active',{active:true})],'first-active',['first-active']);
await check([tab('first',{active:'true'}),tab('second',{active:1})],'first',['first']);
await check([tab('first',{active:'true'}),tab('actual-active',{active:true})],'actual-active',['actual-active']);
await check([tab('old'),tab(undefined,{page:'unlabelled',active:true})],'unlabelled',['unlabelled'],['unlabelled']);
await check([tab('old'),tab('alias',{active:true,url:'https://www.tongyi.com/'})],'alias',['alias']);
await check([tab('retired',{active:true,retired:true})],undefined,['retired']);
const readFailure=new Error('candidate unavailable');
await assert.rejects(check([tab('fallback'),tab('failed',{active:true,error:readFailure})],null,[]),error=>error===readFailure);
console.log('init page selection fixture passed');
`
	ctx, cancel := context.WithTimeout(context.Background(), nodeFixtureTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, node, "--input-type=module", "-e", source).CombinedOutput()
	if err != nil || !strings.Contains(string(output), "init page selection fixture passed") {
		t.Fatalf("pure Node init selection fixture: %v: %s", err, output)
	}
}
