package cdp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEgoSDKDownloadPrivateCleanupAndLimit(t *testing.T) {
	for _, mode := range []string{"saved", "failed", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			transport, server, ctx := testEgoTransport(t)
			s := &Session{ego: transport, ctx: ctx}
			paths := make(chan string, 1)
			go func() {
				var request struct {
					ID     uint64
					Method string
					Params struct {
						Path, Selector string
						Timeout        int64
					}
				}
				if err := json.NewDecoder(server).Decode(&request); err != nil {
					return
				}
				paths <- request.Params.Path
				response := egoResponse{ID: request.ID, Result: json.RawMessage("null")}
				if request.Method != "sdkDownload" || request.Params.Selector != "text=下载图片" || request.Params.Timeout <= 0 || !filepath.IsAbs(request.Params.Path) {
					response.Error = "invalid SDK download request"
				} else if err := os.WriteFile(request.Params.Path, []byte("image bytes"), 0o600); err != nil {
					response.Error = err.Error()
				} else if mode == "failed" {
					response.Error = "SDK download failed"
				} else if mode == "oversized" {
					if err := os.Truncate(request.Params.Path, maxResourceBytes+1); err != nil {
						response.Error = err.Error()
					}
				}
				json.NewEncoder(server).Encode(response)
			}()
			data, err := s.EgoDownload("text=下载图片", time.Second)
			if mode == "saved" {
				if err != nil || !bytes.Equal(data, []byte("image bytes")) {
					t.Fatalf("saved bytes: %q, %v", data, err)
				}
			} else if err == nil || data != nil {
				t.Fatalf("failed download returned bytes: %q, %v", data, err)
			}
			select {
			case path := <-paths:
				if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
					t.Fatalf("private directory was not removed: %s, %v", path, err)
				}
			default:
				t.Fatal("download was not requested")
			}
		})
	}
	if (&Session{}).IsEgo() {
		t.Fatal("Chrome session identified as Ego")
	}
	if _, err := (&Session{}).EgoDownload("button", time.Second); err == nil {
		t.Fatal("SDK request accepted on Chrome")
	}
}

func TestEgoSDKOfficialActionsNodeFixture(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("需要普通 Node.js 运行纯 mock；不调用 ego-browser")
	}
	ctx, cancel := context.WithTimeout(context.Background(), nodeFixtureTimeout)
	defer cancel()
	source := egoSDKJS + `
const assert = await import('node:assert/strict');
let waiting = false, resolveChooser, resolveDownload, events = [], failSave = false;
const artifact = {
 saveAs: async path => { events.push(['save',path]); if(failSave)throw new Error('interrupted') },
 cancel: async () => { events.push(['cancel']) },
 delete: async () => { events.push(['delete']) },
};
function arm(kind,timeout) {
 assert.equal(waiting,false,'stale SDK waiter'); waiting=true; events.push(['arm',kind]);
 return new Promise((resolve,reject)=>{
  const timer=setTimeout(()=>{waiting=false;reject(new Error('timeout'))},timeout);
  const done=value=>{clearTimeout(timer);waiting=false;resolve(value)};
  if(kind==='chooser')resolveChooser=()=>done({setFiles:async files=>events.push(['files',files])});else resolveDownload=()=>done(artifact);
 });
}

const page={
 evaluate: async expression=>{assert.equal(expression,'state');return {ready:true}},
 snapshot: async ()=>'sdk snapshot',
 keyboard: {press:async key=>events.push(['press',key])},
 fill: async (selector,value)=>events.push(['fill',selector,value]),
 click: async selector=>{
  events.push(['click',selector]);
  if(selector==='broken')throw new Error('entry failed');
  if(selector==='local'||selector==='direct')resolveChooser();
  if(selector==='download')resolveDownload();
 },
 waitForFileChooser: ({timeout})=>arm('chooser',timeout),
 waitForEvent: (event,{timeout})=>{assert.equal(event,'download');return arm('download',timeout)},
 cdp: ()=>{throw new Error('raw CDP is forbidden')},
};
assert.deepEqual(await egoSDKAction(page,'sdkEvaluate',{expression:'state'}),{ready:true});
assert.equal(await egoSDKAction(page,'sdkSnapshot',{}),'sdk snapshot');
await egoSDKAction(page,'sdkFill',{selector:'editor',value:'中文'});
await egoSDKAction(page,'sdkClick',{selector:'button'});
events=[];
await egoSDKAction(page,'sdkPress',{key:'Escape'});
assert.deepEqual(events,[['press','Escape']]);
await assert.rejects(egoSDKAction(page,'sdkUpload',{selectors:['broken'],files:['中文.png'],timeout:10}),/entry failed/);
assert.equal(waiting,false);
events=[];
await egoSDKAction(page,'sdkUpload',{selectors:['entry','local'],files:['中文.png'],timeout:50});
assert.deepEqual(events,[['arm','chooser'],['click','entry'],['click','local'],['files',['中文.png']]]);
events=[];
await egoSDKAction(page,'sdkUpload',{selectors:['direct','must not click'],files:['a'],timeout:50});
assert.equal(events.some(event=>event[1]==='must not click'),false);
await assert.rejects(egoSDKAction(page,'sdkDownload',{selector:'broken',path:'/private/result',timeout:10}),/entry failed/);
assert.equal(waiting,false);
failSave=true;events=[];
await assert.rejects(egoSDKAction(page,'sdkDownload',{selector:'download',path:'/private/result',timeout:50}),/interrupted/);
assert.deepEqual(events,[['arm','download'],['click','download'],['save','/private/result'],['cancel'],['delete']]);
failSave=false;events=[];
await egoSDKAction(page,'sdkDownload',{selector:'download',path:'/private/result',timeout:50});
assert.deepEqual(events,[['arm','download'],['click','download'],['save','/private/result'],['delete']]);
console.log('official SDK fixture passed');
`
	output, err := exec.CommandContext(ctx, node, "--input-type=module", "-e", source).CombinedOutput()
	if err != nil || !strings.Contains(string(output), "official SDK fixture passed") {
		t.Fatalf("pure Node fixture: %v: %s", err, output)
	}
}

func TestEgoRetiredPagesNodeFixture(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("需要普通 Node.js；不调用 ego-browser")
	}
	ctx, cancel := context.WithTimeout(context.Background(), nodeFixtureTimeout)
	defer cancel()
	source := egoSDKJS + `
const assert = await import('node:assert/strict');
const area=value=>({tagName:'TEXTAREA',value});
const input=(type,value)=>({tagName:'INPUT',type,value});
const text=nodeValue=>({nodeType:3,nodeValue});
const el=(tag,attrs={},...childNodes)=>({nodeType:1,tagName:tag,childNodes,getAttribute:name=>attrs[name]??null});
const rich=(...children)=>el('DIV',{},...children);
const cases=[
 {label:'empty-textarea',retired:true,editors:[area(' \n')]},
 {label:'empty-input',retired:true,editors:[input('text','')]},
 {label:'sdk-empty-rich',retired:true,editors:[rich(text('\uFEFF'),el('SPAN',{},text('\u200B ')),el('BR'))]},
 {label:'sdk-placeholder',retired:true,editors:[rich(el('SPAN',{'contenteditable':'false'},text('向千问提问')))]},
 {label:'sdk-slate-placeholder',retired:true,editors:[rich(el('SPAN',{'data-slate-placeholder':'true'},text('输入正文')))]},
 {label:'sdk-attachment',retired:true,editors:[rich(el('SPAN',{'contenteditable':'false'},text('中文源图.png'),el('BUTTON',{},text('移除'))))]},
 {label:'sdk-decoration',retired:true,editors:[rich(el('SPAN',{'aria-hidden':'true'},text('占位文字')))]},
 {label:'ignored-inputs',retired:true,editors:[area(''),input('hidden','csrf token'),input('button','上传'),input('file','source.png')]},
 {label:'current',retired:false,editors:[area('')]},
 {label:'user',openedBy:'user',retired:true,editors:[area('')]},
 {label:'unknown',openedBy:'unknown',retired:true,editors:[area('')]},
 {label:null,retired:true,editors:[area('')]},
 {label:'draft',retired:true,editors:[area('未发送草稿')]},
 {label:'input-draft',retired:true,editors:[input('text','正文')]},
 {label:'rich-draft',retired:true,editors:[rich(el('SPAN',{},text('真实'),el('SPAN',{},text('正文'))))]},
 {label:'mixed-rich-draft',retired:true,editors:[rich(el('SPAN',{'contenteditable':'false'},text('附件.png')),el('SPAN',{},text('保留正文')))]},
 {label:'placeholder-text-draft',retired:true,editors:[rich(text('向千问提问'))]},
 {label:'placeholder-attribute-draft',retired:true,editors:[rich(el('SPAN',{'data-placeholder':'输入正文'},text('实际正文')))]},
 {label:'uncertain-rich',retired:true,editors:[{tagName:'DIV'}]},
 {label:'shadow-rich',retired:true,editors:[{...rich(),shadowRoot:{}}]},
 {label:'mixed-draft',retired:true,editors:[area(''),area('draft')]},
 {label:'no-editor',retired:true,editors:[]},
 {label:'read-error',retired:true,error:true,editors:[area('')]},
 {label:'close-error',retired:true,editors:[area('')],closeError:true},
];
const closed=[],read=[];
const pageFor=item=>({
 evaluate:async fn=>{
  read.push(item.label);
  if(item.error)throw new Error('page unavailable');
  globalThis.sessionStorage={getItem:()=>item.retired?'1':null};
  globalThis.document={querySelectorAll:()=>item.editors};
  globalThis.window={}; // SDK 页面没有旧 composer reader。
  return fn();
 },
 close:async()=>{if(item.closeError)throw new Error('close failed');closed.push(item.label)},
});
const task={
 tabs:async()=>cases.map(item=>({...item,openedBy:item.openedBy??'agent'})),
 page:label=>pageFor(cases.find(item=>item.label===label)),
};
assert.equal(await egoCloseRetiredPages(task),8);
assert.deepEqual(closed,['empty-textarea','empty-input','sdk-empty-rich','sdk-placeholder','sdk-slate-placeholder','sdk-attachment','sdk-decoration','ignored-inputs']);
assert.equal(read.includes('user'),false);
assert.equal(read.includes('unknown'),false);
assert.equal(read.includes(null),false);
// newPage（init 和 RenewPage 共用）必须在回收完成后才创建。
let budgetUsed=8;
const retired={label:'old',retired:true,editors:[rich(el('SPAN',{'contenteditable':'false'},text('附件.png')))]};
const cleanupLogs=[];
const createTask={
 tabs:async()=>Array.from({length:budgetUsed},(_,i)=>({label:i===0?'old':'current'+i,openedBy:'agent'})),
 page:label=>label==='old'?{...pageFor(retired),close:async()=>{budgetUsed--}}:pageFor({label,retired:false}),
 newPage:async()=>{assert.equal(budgetUsed,7,'cleanup must precede creation');budgetUsed++;return 'fresh'},
};
assert.equal(await egoNewPage(createTask,count=>cleanupLogs.push(count)),'fresh');
assert.deepEqual(cleanupLogs,[1]);
const fullTask={tabs:async()=>Array.from({length:8},()=>({openedBy:'user'})),newPage:async()=>{throw new Error('Page budget reached (8/8)')}};
await assert.rejects(egoNewPage(fullTask),error=>error.message.includes('8/8')&&error.message.includes('回收 0')&&error.message.includes('剩余 8')&&!error.message.includes('正文内容'));
console.log('retired cleanup fixture passed');
`
	output, err := exec.CommandContext(ctx, node, "--input-type=module", "-e", source).CombinedOutput()
	if err != nil || !strings.Contains(string(output), "retired cleanup fixture passed") {
		t.Fatalf("pure Node retired-page fixture: %v: %s", err, output)
	}
}
