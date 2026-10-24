package cdp

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestEgoUploadMenuNodeFixture(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("需要普通 Node.js；不调用 ego-browser")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	source := egoSDKJS + `
const assert = await import('node:assert/strict');
async function run(mode,alreadyOpen=false){
 const presses=[],triggers=[],clicks=[],files=[];
 let resolveChooser,waiting=false;
 const menu={contains:node=>node===menu||items.includes(node)};
 const outside={closest:()=>null};
 function item(label,options={}){
  const node={innerText:label,disabled:false,
   getBoundingClientRect:()=>({width:options.hidden?0:100,height:20}),
   getAttribute:name=>name==='aria-disabled'&&options.disabled?'true':null,
   hasAttribute:()=>false,
   closest:query=>query==='[role="menu"]'?menu:null,
   contains:other=>other===node,
   querySelectorAll:()=>[{innerText:label.split('\n')[0]}],
  };return node;
 }
 const target=item('上传图片\n最多上传10张');
 const items=[item('上传文档'),target,item('上传图片',{hidden:true}),item('上传图片',{disabled:true})];
 if(mode==='duplicate')items.push(item('上传图片'));
 if(mode==='cycle')items.push(item('其他菜单项'));
 if(mode==='missing')items.splice(1,1);
 globalThis.document={activeElement:alreadyOpen?menu:outside,querySelectorAll:()=>items};
 globalThis.getComputedStyle=()=>({display:'block',visibility:'visible'});
 const page={
  evaluate:async(fn,arg)=>{if(mode==='expired')await new Promise(resolve=>setTimeout(resolve,25));return fn(arg)},
  click:async selector=>{clicks.push(selector);assert.equal(selector,'entry');document.activeElement=mode==='outside'?outside:menu},
  press:async(selector,key,options)=>{
   assert.equal(waiting,true,'chooser must precede focused trigger');
   assert.equal(selector,'entry');assert.equal(key,'ArrowDown');assert.ok(options.timeout>0);
   triggers.push([selector,key]);
   if(mode==='trigger-failed')throw new Error('trigger unavailable');
   document.activeElement=mode==='outside'?outside:items[0];
  },
  keyboard:{press:async key=>{
   presses.push(key);
   if(key==='Enter'){assert.equal(document.activeElement,target);resolveChooser();return}
   assert.equal(key,'ArrowDown');
   if(mode==='stuck')return;
   if(mode==='leave'){document.activeElement=outside;return}
   const enabled=items.filter(item=>item.getBoundingClientRect().width>0&&item.getAttribute('aria-disabled')!=='true');
   const next=enabled[(enabled.indexOf(document.activeElement)+1)%enabled.length];
   document.activeElement=mode==='loop'?enabled[0]:mode==='cycle'?(document.activeElement===enabled[0]?enabled.at(-1):enabled[0]):next;
  }},
  waitForFileChooser:({timeout})=>{
   assert.equal(waiting,false);waiting=true;
   return new Promise((resolve,reject)=>{
    const timer=setTimeout(()=>{waiting=false;reject(new Error('chooser timeout'))},timeout);
    resolveChooser=()=>{clearTimeout(timer);waiting=false;resolve({setFiles:async value=>files.push(value)})};
   });
  },
  cdp:()=>{throw new Error('raw CDP is forbidden')},
 };
 if(mode==='at-target')document.activeElement=target;
 const request={selectors:alreadyOpen?[]:['entry'],menuLabel:'上传图片',files:['中文.png'],timeout:20};
 let error;
 try{await egoSDKAction(page,'sdkUpload',request)}catch(value){error=value}
 assert.equal(waiting,false,'chooser must be consumed after errors');
 return {presses,triggers,clicks,files,error};
}
const normal=await run('normal');
assert.equal(normal.error,undefined);
assert.deepEqual(normal.clicks,[]);
assert.deepEqual(normal.triggers,[['entry','ArrowDown']]);
assert.deepEqual(normal.presses,['ArrowDown','Enter']);
assert.deepEqual(normal.files,[['中文.png']]);
const opened=await run('normal',true);
assert.equal(opened.error,undefined);assert.deepEqual(opened.clicks,[]);assert.deepEqual(opened.triggers,[]);assert.deepEqual(opened.presses,['ArrowDown','ArrowDown','Enter']);
const focused=await run('at-target',true);
assert.equal(focused.error,undefined);assert.deepEqual(focused.presses,['Enter']);
for(const [mode,pattern]of [['duplicate',/不存在或不唯一/],['missing',/不存在或不唯一/],['outside',/焦点已离开/],['leave',/焦点已离开/],['stuck',/导航无效/],['loop',/导航无效/],['cycle',/未到达目标/],['expired',/选择器已结束/],['trigger-failed',/trigger unavailable/]]){
 const result=await run(mode);
 assert.match(result.error.message,pattern);
 assert.equal(result.presses.includes('Enter'),false,mode+' must never confirm');
 assert.deepEqual(result.files,[]);
 assert.ok(result.presses.length<=5,'bounded navigation');
}
console.log('keyboard upload menu fixture passed');
`
	output, err := exec.CommandContext(ctx, node, "--input-type=module", "-e", source).CombinedOutput()
	if err != nil || !strings.Contains(string(output), "keyboard upload menu fixture passed") {
		t.Fatalf("pure Node keyboard menu fixture: %v: %s", err, output)
	}
}
