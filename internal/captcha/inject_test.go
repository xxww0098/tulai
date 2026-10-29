package captcha

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func TestCaptchaDetectionRequiresVerificationContext(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("需要普通 Node.js 运行隔离 DOM fixture")
	}
	script := `const assert=require('node:assert/strict'),vm=require('node:vm');
function token(el,query){
 if(query.startsWith('#'))return el.id===query.slice(1);
 if(query.startsWith('.'))return el.className.split(/\s+/).includes(query.slice(1));
 const m=query.match(/^([a-z]+)?(?:\[([a-z-]+)(\*?=)["']?([^"'\]]+)["']?\])?$/i);
 if(!m||m[1]&&el.tagName!==m[1].toUpperCase())return false;
 return !m[2]||(m[3]==='*='?el.getAttribute(m[2]).includes(m[4]):el.getAttribute(m[2])===m[4]);
}

function matches(el,query){
 const parts=query.trim().split(/\s+/);if(!token(el,parts.pop()))return false;
 for(let i=parts.length-1;i>=0;i--){el=el.parentElement;while(el&&!token(el,parts[i]))el=el.parentElement;if(!el)return false;}
 return true;
}
class Element {
 constructor(tag,attrs={},children=[],text=''){
  this.tagName=tag.toUpperCase();this.attrs=attrs;this.children=children;this.text=text;
  this.id=attrs.id||'';this.className=attrs.class||'';this.width=300;this.height=300;
  children.forEach(child=>child.parentElement=this);
 }
 get innerText(){return [this.text,...this.children.map(child=>child.innerText)].join(' ')}
 get textContent(){return this.innerText}
 getAttribute(name){return this.attrs[name]||''}
 getBoundingClientRect(){return {left:0,top:0,right:300,bottom:300,width:300,height:300}}
 querySelectorAll(query){const descendants=this.children.flatMap(child=>[child,...child.querySelectorAll('*')]);return query==='*'?descendants:descendants.filter(el=>query.split(',').some(part=>matches(el,part)))}
 querySelector(query){return this.querySelectorAll(query)[0]||null}
}
const canvas=()=>new Element('canvas');
const editor=(text='创建画布 标记改图 抠图 擦除 扩图 保存',count=1)=>new Element('div',{class:'dbx-dialog modal overlay'},Array.from({length:count},canvas),text);
let body=new Element('body'),report,events=[];
const document={readyState:'loading',get body(){return body},querySelectorAll:q=>body.querySelectorAll(q),querySelector:q=>body.querySelector(q)};
const window={addEventListener(){},tulaiCall(name,value){events.push([name,value]);return Promise.resolve({success:false})}};
const source=` + strconv.Quote(InjectJS()) + `;
const staleWindow={__tulaiCaptchaInstalled:true};vm.runInNewContext(source,{window:staleWindow});
assert.equal(staleWindow.__tulaiCaptchaRevision,undefined,'old installed guard must not claim a new revision');
vm.runInNewContext(source,{window,document,console:{log(){}},getComputedStyle:()=>({display:'block',visibility:'visible',opacity:'1'}),setInterval:fn=>{report=fn},setTimeout:()=>0});
assert.equal(window.__tulaiCaptchaRevision,` + strconv.Quote(injectRevision) + `);
window.__tulaiCaptcha.setManual(true);
window.__tulaiCaptcha.start();
function check(root,expected,label,extra=[]){
 body=new Element('body',{},[root,...extra]);
 assert.equal(!!window.__tulaiCaptcha.detect(),expected,label);
 report();assert.equal(window.__tulaiCaptcha.status().blocked,expected,label+' blocking state');
}
check(editor(),false,'normal generated-image editor');
check(editor('请拖动画布，滑动缩放图片'),false,'editing instructions are not verification semantics');
check(editor('扫码登录 验证码登录'),false,'login options are not a graphical captcha');
check(editor('captcha image preview'),false,'a captcha mention alone is not a verification instruction');
check(editor('拼图画布',9),false,'multiple canvases alone are not a captcha');
check(new Element('div',{class:'modal'},[new Element('img')],'图片预览 保存'),false,'one preview image alone is not a captcha');
check(editor(),false,'unrelated page text must not classify the editor',[new Element('p',{},[],'安全验证')]);
check(new Element('div',{class:'captcha-widget'},[canvas()]),true,'dedicated captcha container');
check(new Element('div',{class:'modal'},[canvas()],'请完成下方验证码'),true,'local verification prompt');
check(new Element('div',{class:'dialog'},[canvas()],'安全验证：请拖动滑块'),true,'explicit slider verification');
check(new Element('div',{class:'dialog'},[canvas()],'Please drag the slider to verify'),true,'aliyun slider prompt stays verifying');
check(new Element('button',{},[],'Please click me to try again(error:verify)'),true,'aliyun slider retry stays verifying');
check(new Element('iframe',{src:'https://example.test/captcha'}),true,'dedicated captcha iframe');
check(editor(),false,'normal editor clears prior captcha blocking');
window.__tulaiCaptcha.stop();
assert.equal(events.some(([name])=>name==='captchaSolve'),false,'classification fixture must never solve a challenge');
`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("隔离验证码分类 fixture: %v\n%s", err, out)
	}
}

func TestCaptchaAutomaticSolvingRequiresExplicitEnabled(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("需要普通 Node.js 运行隔离 DOM fixture")
	}
	script := `const assert=require('node:assert/strict'),vm=require('node:vm');
const source=` + strconv.Quote(InjectJS()) + `;
const disabledSource=` + strconv.Quote(configuredInjectJS(false)) + `;
const enabledSource=` + strconv.Quote(configuredInjectJS(true)) + `;
async function check(kind,enabled,disableAt){
 const calls=[],actions=[],logs=[],timers=[];
 let actionsAtDisable;
 const rect={x:0,y:0,left:0,top:0,right:300,bottom:40,width:300,height:40};
 const canvas={tagName:'CANVAS',width:100,height:100,getBoundingClientRect:()=>rect};
 const handle={tagName:'DIV',className:'slider-handle',textContent:'按住',getBoundingClientRect:()=>({...rect,right:40,width:40}),querySelectorAll:()=>[]};
 const root={tagName:'DIV',id:'captcha_verify_image',className:'captcha-widget',innerText:kind==='slider'?'安全验证 请按住滑块拖到最右':'请完成下方验证码',getAttribute:()=>'',getBoundingClientRect:()=>rect,
  querySelector:()=>null,querySelectorAll(q){if(q==='canvas')return kind==='grid'?[canvas]:[];if(q==='img')return [];if(q.includes('slider'))return kind==='slider'?[handle]:[];return []}};
 handle.parentElement=root;
 const document={readyState:'loading',body:{innerText:'',querySelectorAll:()=>[]},querySelector:()=>null,
  querySelectorAll:q=>q==='#captcha_verify_image'?[root]:[],
  createElement(){actions.push('capture');return {getContext:()=>({drawImage(){}}),toDataURL:()=>''}},
  elementFromPoint(){actions.push('hit');return {dispatchEvent(){actions.push('drag')}}}};
 const window={addEventListener(){},tulaiCall(name,value){
  calls.push(name);
  if(name===disableAt){window.__tulaiCaptcha.setManual(true);actionsAtDisable=actions.length;return Promise.resolve({success:true,indices:[1],ratio:0.5,base64:'fixture'})}
  return Promise.resolve({success:false,error:'fixture stop'});
 }};
 const context={window,document,console:{log:msg=>logs.push(msg)},getComputedStyle:()=>({display:'block',visibility:'visible',opacity:'1'}),
  setInterval:fn=>timers.push(fn),setTimeout:()=>0};
 vm.runInNewContext(enabled===undefined?source:(enabled?enabledSource:disabledSource),context);
 assert.ok(window.__tulaiCaptcha.detect(),kind+' must remain detectable');
 await window.__tulaiCaptcha.solve();
 const polling=window.__tulaiCaptcha.start();
 timers.forEach(fn=>fn());
 await Promise.resolve();await Promise.resolve();
 window.__tulaiCaptcha.stop();
 assert.ok(calls.includes('captchaBlocking'),kind+' must report blocking');
 if(enabled===true){
  assert.ok(calls.includes(kind==='slider'?'captchaShot':'captchaSolve'),kind+' enabled enters solver');
  if(disableAt)assert.equal(actions.length,actionsAtDisable,kind+' disabling during bridge call stops subsequent actions');
  const listener=window.__tulaiCaptcha,revision=window.__tulaiCaptchaRevision;
  vm.runInNewContext(disabledSource,context);
  assert.equal(window.__tulaiCaptcha,listener,'installed listener must receive config without reinstall');
  assert.equal(window.__tulaiCaptchaRevision,revision,'config must preserve installed revision');
  calls.length=0;actions.length=0;
  await window.__tulaiCaptcha.solve();
  assert.equal(calls.some(name=>name==='captchaShot'||name==='captchaSolve'),false,kind+' installed guard must not retain enabled');
  assert.deepEqual(actions,[],kind+' installed guard must not retain actions');
  window.__tulaiCaptcha.setManual(true);
  vm.runInNewContext(enabledSource,context);
  assert.equal(window.__tulaiCaptcha.status().autoSolve,true,'new Attach must reset manual using explicit user configuration');
 }else{
  assert.equal(calls.some(name=>name==='captchaShot'||name==='captchaSolve'),false,kind+' disabled must not enter solver');
  assert.deepEqual(actions,[],kind+' disabled must not capture, drag or submit');
  assert.equal(logs.some(msg=>/求解失败|未找到滑块把手|重试|换题/.test(msg)),false,kind+' disabled must not retry');
 }
}
(async()=>{
 for(const kind of ['slider','grid']){
  for(const enabled of [undefined,false,true])await check(kind,enabled);
  await check(kind,true,kind==='slider'?'captchaShot':'captchaSolve');
 }
})().catch(err=>{console.error(err);process.exitCode=1});
`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("隔离验证码开关 fixture: %v\n%s", err, out)
	}
}

type preparePageFixture struct {
	ctx         context.Context
	ego         bool
	installed   bool
	revision    string
	blocked     bool
	autoSolve   bool
	manualCalls int
	afterRead   func()
	evaluations int
	renewals    int
}

func (s *preparePageFixture) IsEgo() bool { return s.ego }

func (s *preparePageFixture) Ctx() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

func (s *preparePageFixture) EgoEvaluate(expression string, out any) error {
	s.evaluations++
	if strings.Contains(expression, "tulai captcha:manual") {
		s.manualCalls++
		return nil
	}
	if s.afterRead != nil {
		s.afterRead()
	}
	data, err := json.Marshal(map[string]any{"installed": s.installed, "revision": s.revision, "blocked": s.blocked, "autoSolve": s.autoSolve})
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func (s *preparePageFixture) RenewPage() error {
	s.renewals++
	return nil
}

func TestPreparePageRenewsOnlyOldEgoCaptchaDocument(t *testing.T) {
	for _, tc := range []struct {
		name      string
		page      preparePageFixture
		wantEval  int
		wantRenew int
	}{
		{"old-without-revision", preparePageFixture{ego: true, installed: true}, 1, 1},
		{"old-revision", preparePageFixture{ego: true, installed: true, revision: "old"}, 1, 1},
		{"current", preparePageFixture{ego: true, installed: true, revision: injectRevision}, 1, 0},
		{"first-page", preparePageFixture{ego: true}, 1, 0},
		{"chrome", preparePageFixture{installed: true}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := PreparePage(&tc.page); err != nil {
				t.Fatal(err)
			}
			if tc.page.evaluations != tc.wantEval || tc.page.renewals != tc.wantRenew {
				t.Fatalf("page preparation = %+v, want evaluations=%d, renewals=%d", tc.page, tc.wantEval, tc.wantRenew)
			}
		})
	}
}
