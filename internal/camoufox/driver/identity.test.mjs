import assert from 'node:assert/strict';
import {execFile} from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import {promisify} from 'node:util';
import {createIdentity, prepareIdentity, prepareRendering, preserveVoices} from './identity.mjs';

const device={'navigator.userAgent':'Mozilla/5.0 (Macintosh; rv:150.0) Gecko/20100101 Firefox/150.0','navigator.platform':'MacIntel','navigator.oscpu':'Intel Mac OS X 10.15','navigator.hardwareConcurrency':8,'canvas:seed':42,'screen.width':1920,'screen.height':1080,'screen.availWidth':1920,'screen.availHeight':1004,'screen.colorDepth':24};

test('reuse host rendering only when it is compatible with the saved GPU',()=>{
 const saved={...device,'webGl:vendor':'Apple','webGl:renderer':'Apple M1'};
 assert.deepEqual(prepareRendering(saved,true,['Apple','Apple M1']),{config:saved});
 assert.deepEqual(prepareRendering(saved,false,['Other','GPU']),{config:saved});
 assert.deepEqual(prepareRendering(saved,false,null),{config:saved,canvasNoise:false});
 assert.deepEqual(prepareRendering(saved,false,['Apple','Apple M1']),{config:device});
 assert.equal(saved['webGl:renderer'],'Apple M1');
});

test('restarts retain the device; upgrades track the real Firefox version',()=>{
 const saved={...device};
 assert.deepEqual(prepareIdentity(saved,'150',false),saved);
 const upgraded=prepareIdentity(saved,'158',false);
 assert.match(upgraded['navigator.userAgent'],/rv:158\.0.*Firefox\/158\.0/);
 assert.equal(upgraded['canvas:seed'],42);
 assert.match(saved['navigator.userAgent'],/Firefox\/150\.0/);
});
test('addon paths are configured afresh instead of persisted as device identity',()=>{
 const saved={...device,addons:['/old/download-helper']};
 assert.deepEqual(prepareIdentity(saved,'150',false),device);
 assert.deepEqual(saved.addons,['/old/download-helper']);
});
test('changing or clearing a proxy cannot retain the previous exit location',()=>{
 const saved={...device,'webrtc:ipv4':'1.1.1.1','webrtc:ipv6':'2001:db8::1','network:ipv4Only':true,
  'timezone':'old','locale:language':'old','locale:region':'old','locale:script':'old',
  'geolocation:latitude':10,'geolocation:longitude':20,'navigator.language':'old','navigator.languages':['old'],
  'voices':['old'],'headers.Accept-Language':'old'};
 for (const proxy of [true,false]) assert.deepEqual(prepareIdentity(saved,'150',proxy),{...device,voices:['old']});
});
test('voices stay stable for the same locale and refresh when it changes',async()=>{
 const saved={...device,'locale:language':'en','locale:region':'US',voices:['original']};
 const config=prepareIdentity(saved,'150',true);
 let calls=0;
 const generate=async input=>({identity:{...input,'locale:language':'en','locale:region':'US',voices:input.voices||['fresh']},calls:++calls});
 assert.deepEqual((await preserveVoices(saved,config,generate)).identity.voices,['original']);
 assert.equal(calls,1);
 calls=0;
 const changed=async input=>({identity:{...input,'locale:language':'zh','locale:region':'CN',voices:input.voices||['fresh']},calls:++calls});
 assert.deepEqual((await preserveVoices(saved,config,changed)).identity.voices,['fresh']);
 assert.equal(calls,2);
});
test('incomplete persisted identities cannot silently generate a new device',()=>{
 assert.equal(prepareIdentity(undefined,'158',false),undefined);
 for (const saved of [null,false,'invalid',[],{}, {...device,'screen.width':0}]) assert.throws(()=>prepareIdentity(saved,'158',false));
});

test('new environments retry repeated device fingerprints, even when unrelated fields differ',async t=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'tulai-identity-'));
 t.after(()=>fs.rmSync(root,{recursive:true,force:true}));
 const first=path.join(root,'account-1'), second=path.join(root,'account-2');
 fs.mkdirSync(first); fs.mkdirSync(second);
 await createIdentity(first,async()=>({identity:device}));
 let attempts=0;
 await createIdentity(second,async()=>({identity:++attempts===1
  ? {...device,'canvas:seed':99}
  : {...device,'screen.width':1600,'screen.availWidth':1600}}));
 assert.equal(attempts,2);
 assert.deepEqual(JSON.parse(fs.readFileSync(path.join(first,'.tulai-identity.json'))),device);
 assert.equal(JSON.parse(fs.readFileSync(path.join(second,'.tulai-identity.json')))['screen.width'],1600);
});

test('simultaneous launcher processes do not retain the same device',async t=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'tulai-identity-'));
 t.after(()=>fs.rmSync(root,{recursive:true,force:true}));
 const profiles=[1,2].map(i=>path.join(root,`account-${i}`));
 profiles.forEach(profile=>fs.mkdirSync(profile));
 const script=`
  import fs from 'node:fs';
  import path from 'node:path';
  import {createIdentity} from ${JSON.stringify(new URL('./identity.mjs',import.meta.url).href)};
  const [root,index]=process.argv.slice(1), device=${JSON.stringify(device)};
  let attempts=0;
  await createIdentity(path.join(root,'account-'+index),async()=>{
   if (++attempts===1) {
    fs.writeFileSync(path.join(root,'ready-'+index),'');
    while (!fs.existsSync(path.join(root,'ready-'+(index==='1'?'2':'1')))) await new Promise(r=>setTimeout(r,5));
    return {identity:device};
   }
   return {identity:{...device,'screen.width':1400+Number(index),'screen.availWidth':1400+Number(index)}};
  });`;
 const run=promisify(execFile);
 await Promise.all([1,2].map(index=>run(process.execPath,['--input-type=module','-e',script,root,String(index)],{timeout:10000})));
 const widths=profiles.map(profile=>JSON.parse(fs.readFileSync(path.join(profile,'.tulai-identity.json')))['screen.width']);
 assert.equal(new Set(widths).size,profiles.length);
});

test('exhausting the fingerprint pool leaves existing accounts intact',async t=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'tulai-identity-'));
 t.after(()=>fs.rmSync(root,{recursive:true,force:true}));
 const first=path.join(root,'account-1'), second=path.join(root,'account-2');
 fs.mkdirSync(first); fs.mkdirSync(second);
 await createIdentity(first,async()=>({identity:device}));
 await assert.rejects(createIdentity(second,async()=>({identity:device})),/无法生成/);
 assert.deepEqual(JSON.parse(fs.readFileSync(path.join(first,'.tulai-identity.json'))),device);
 assert.equal(fs.existsSync(path.join(second,'.tulai-identity.json')),false);
});

test('a corrupt existing identity is not ignored during collision checks',async t=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'tulai-identity-'));
 t.after(()=>fs.rmSync(root,{recursive:true,force:true}));
 const first=path.join(root,'account-1'), second=path.join(root,'account-2');
 fs.mkdirSync(first); fs.mkdirSync(second);
 fs.writeFileSync(path.join(first,'.tulai-identity.json'),'{}');
 await assert.rejects(createIdentity(second,async()=>({identity:device})),/读取已有环境指纹失败/);
 assert.equal(fs.existsSync(path.join(second,'.tulai-identity.json')),false);
});

test('missing optional fields in a legacy identity cannot hide a collision',async t=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'tulai-identity-'));
 t.after(()=>fs.rmSync(root,{recursive:true,force:true}));
 const first=path.join(root,'account-1'), second=path.join(root,'account-2');
 fs.mkdirSync(first); fs.mkdirSync(second);
 const legacy={...device}; delete legacy['navigator.oscpu']; delete legacy['navigator.hardwareConcurrency'];
 fs.writeFileSync(path.join(first,'.tulai-identity.json'),JSON.stringify(legacy));
 let attempts=0;
 await createIdentity(second,async()=>({identity:++attempts===1
  ? {...device,'navigator.hardwareConcurrency':16}
  : {...device,'screen.width':1600,'screen.availWidth':1600}}));
 assert.equal(attempts,2);
});

test('an incomplete generated fingerprint is not saved',async t=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'tulai-identity-'));
 t.after(()=>fs.rmSync(root,{recursive:true,force:true}));
 const profile=path.join(root,'account-1'); fs.mkdirSync(profile);
 const incomplete={...device}; delete incomplete['navigator.oscpu'];
 await assert.rejects(createIdentity(profile,async()=>({identity:incomplete})),/指纹不完整/);
 assert.equal(fs.existsSync(path.join(profile,'.tulai-identity.json')),false);
});

test('a concurrent identity file is never overwritten',async t=>{
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'tulai-identity-'));
 t.after(()=>fs.rmSync(root,{recursive:true,force:true}));
 const profile=path.join(root,'account-1'); fs.mkdirSync(profile);
 const file=path.join(profile,'.tulai-identity.json');
 fs.writeFileSync(file,JSON.stringify(device));
 await assert.rejects(createIdentity(profile,async()=>({identity:{...device,'screen.width':1600,'screen.availWidth':1600}})),{code:'EEXIST'});
 assert.deepEqual(JSON.parse(fs.readFileSync(file)),device);
 assert.deepEqual(fs.readdirSync(profile),['.tulai-identity.json']);
});
