package cdp

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestEgoUploadDiagnosticNodeFixture(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("需要普通 Node.js；不调用 ego-browser")
	}
	ctx, cancel := context.WithTimeout(context.Background(), nodeFixtureTimeout)
	defer cancel()
	source := egoSDKJS + `
const assert = await import('node:assert/strict');
const secret='PRIVATE_CHAT https://private.example/signed?token=secret';
const items=Array.from({length:6},(_,i)=>({
 innerText:i===0?'上传图片\n最多10张':i===1?'上传文档\u200B':i===2?'上传https://x.co':i===3?'上传'+'图'.repeat(38):secret,
 getBoundingClientRect:()=>({x:10+i,y:20,width:i===0?0:100,height:30}),
 getAttribute:()=>i===0?'true':secret,
 style:{display:'block',visibility:i===0?'hidden':'visible'},
}));
globalThis.document={querySelectorAll:query=>{assert.equal(query,'[role="menuitem"]');return items},activeElement:{tagName:'BUTTON',getAttribute:()=> 'menuitem'},hasFocus:()=>false};
globalThis.getComputedStyle=item=>item.style;
let waiting=false,choose,diagnosticError=false,snapshotError=false,snapshots=0,clicks=[],logs=[],files=[];
const original=new Error('original click failure');
const page={
 waitForFileChooser:({timeout})=>{
  assert.equal(waiting,false);waiting=true;
  return new Promise((resolve,reject)=>{
   const timer=setTimeout(()=>{waiting=false;reject(new Error('chooser timeout'))},timeout);
   choose=()=>{clearTimeout(timer);waiting=false;resolve({setFiles:async value=>files.push(value)})};
  });
 },
 click:async selector=>{clicks.push(selector);if(selector==='broken')throw original;if(selector==='local')choose()},
 evaluate:async fn=>{if(diagnosticError)throw new Error(secret);return fn()},
 snapshot:async()=>{snapshots++;if(snapshotError)throw new Error(secret);return '- document "'+secret+'"\n- @17 menuitem "上传文档"\n- menuitem "上传图片" [ref=18]\n- text "'+secret+'"'},
 cdp:()=>{throw new Error('raw CDP is forbidden')},
};
await egoSDKAction(page,'sdkUpload',{selectors:['entry','local','must-not-click'],files:['私有源图.png'],timeout:30},message=>logs.push(message));
assert.deepEqual(clicks,['entry','local']);
assert.deepEqual(files,[['私有源图.png']]);
const summaries=logs.filter(log=>log.includes(' menuItems='));
const details=logs.filter(log=>log.includes(' item#')&&!log.includes(' labelCodePoints#'));
const pointLogs=logs.filter(log=>log.includes(' labelCodePoints#'));
assert.equal(summaries.length,4);
assert.equal(details.length,16);
assert.equal(pointLogs.length,20);
assert.equal(snapshots,1,'only later steps take one SDK snapshot');
assert.deepEqual(logs.filter(log=>log.includes(' snapshot label=')),[
 '上传步骤 2/3 snapshot label=上传文档 role=menuitem ref=@17',
 '上传步骤 2/3 snapshot label=上传图片 role=menuitem ref=@18',
]);
assert.deepEqual(JSON.parse(pointLogs[0].match(/ labelCodePoints#\d+=(.*)$/)[1]),Array.from('上传图片\n最多10张').map(char=>char.codePointAt(0)));
assert.ok(pointLogs.some(log=>log.includes('8203')),'invisible label characters must be observable');
assert.match(summaries[0],/1\/3 开始 chooserSettled=false menuItems=6 image=1 document=0 other=5 active=BUTTON\/menuitem hasFocus=false/);
assert.match(summaries[1],/1\/3 成功 chooserSettled=false/);
assert.match(summaries[2],/2\/3 开始 chooserSettled=false/);
assert.match(summaries[3],/2\/3 成功 chooserSettled=true/);
for(const log of logs){
 assert.ok(Buffer.byteLength(log,'utf8')<=320,'each line must pass the engine 400-byte limit');
 assert.equal(log.includes(secret),false);
 assert.equal(log.includes('私有源图'),false);
}
assert.deepEqual(JSON.parse(details[0].match(/ item#\d+=(.*)$/)[1]),{kind:'image',rect:[10,20,0,30],display:'block',visibility:'hidden',ariaHidden:'true'});
assert.equal(JSON.parse(details[1].match(/ item#\d+=(.*)$/)[1]).ariaHidden,'unset');
diagnosticError=true;clicks=[];logs=[];
await assert.rejects(egoSDKAction(page,'sdkUpload',{selectors:['broken','must-not-click'],files:['a'],timeout:5},message=>logs.push(message)),error=>error===original);
assert.deepEqual(clicks,['broken']);
assert.equal(waiting,false);
assert.equal(logs.length,2);
assert.match(logs[0],/1\/2 开始 chooserSettled=false 菜单诊断不可用/);
assert.match(logs[1],/1\/2 失败 chooserSettled=false 菜单诊断不可用/);
assert.equal(logs.some(log=>log.includes(secret)),false);
// 日志回调失败同样不能覆盖动作错误，也不能残留 chooser。
await assert.rejects(egoSDKAction(page,'sdkUpload',{selectors:['broken'],files:['a'],timeout:5},()=>{throw new Error('log failed')}),error=>error===original);
assert.equal(waiting,false);
diagnosticError=false;clicks=[];
await egoSDKAction(page,'sdkUpload',{selectors:['local'],files:['b'],timeout:30},()=>{});
assert.deepEqual(clicks,['local']);
snapshotError=true;clicks=[];logs=[];
await egoSDKAction(page,'sdkUpload',{selectors:['entry','local'],files:['c'],timeout:30},message=>logs.push(message));
assert.deepEqual(clicks,['entry','local'],'snapshot failure must not change actions');
assert.ok(logs.some(log=>log.includes('snapshot 诊断不可用')));
assert.equal(logs.some(log=>log.includes(secret)),false);
assert.deepEqual(egoUploadSnapshotLines({role:'document',name:secret,children:[
 {role:'menuitem',name:'上传文档',ref:'@21',private:secret},
 {role:'button',accessibleName:'上传图片\n'+secret,refId:22},
 {role:secret,name:'上传图片',ref:secret},
]}),[
 'label=上传文档 role=menuitem ref=@21',
 'label=上传图片 role=button ref=@22',
 'label=上传图片 role=other ref=none',
]);
assert.equal(egoUploadSnapshotLines(Array.from({length:8},(_,i)=>({name:'上传图片',role:'menuitem',ref:i}))).length,4);
assert.deepEqual(egoUploadSnapshotLines({snapshot:'@23 menuitem "上传图片"'}),['label=上传图片 role=menuitem ref=@23']);
assert.deepEqual(egoUploadSnapshotLines(['@24 menuitem "上传文档"',secret]),['label=上传文档 role=menuitem ref=@24']);
console.log('upload diagnostic fixture passed');
`
	output, err := exec.CommandContext(ctx, node, "--input-type=module", "-e", source).CombinedOutput()
	if err != nil || !strings.Contains(string(output), "upload diagnostic fixture passed") {
		t.Fatalf("pure Node upload diagnostic fixture: %v: %s", err, output)
	}
}
