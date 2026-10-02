import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseTag, compareTags, previousTag, validateChannel, replaceSummary, responseText, summarize } from '../.github/scripts/release-lib.mjs';
const completed = text => ({status:'completed',output:[{type:'reasoning',content:[{text:'private thinking'}]},{type:'message',role:'assistant',status:'completed',content:[{type:'output_text',text}]}]});
test('stable and prerelease routing uses strict SemVer', () => {
  assert.equal(validateChannel('v1.2.3','release'),false);
  for(const tag of ['v1.2.3-rc.1','v1.2.3-beta.2','v1.2.3-alpha.1']) assert.equal(validateChannel(tag,'prerelease'),true);
  for(const tag of ['v01.2.3','v1.2','v1.2.3-rc.01','v1.2.3;echo','main','v1.2.3+build']) assert.throws(()=>parseTag(tag));
  assert.throws(()=>validateChannel('v1.2.3','prerelease'));
  assert.throws(()=>validateChannel('v1.2.3-rc.1','release'));
});
test('stable release range includes prior prerelease work',()=>{
  const tags=['v1.0.0','v1.1.0-rc.1','v1.1.0-rc.2','v1.1.0','invalid','v2.0.0'];
  assert.equal(previousTag(tags,'v1.1.0','release'),'v1.0.0');
  assert.equal(previousTag(tags,'v1.1.0-rc.3','prerelease'),'v1.1.0-rc.2');
  assert.equal(previousTag([], 'v1.0.0','release'),null);
  assert(compareTags('v1.0.0-rc.10','v1.0.0-rc.2')>0);
  assert(compareTags('v1.0.0','v1.0.0-rc.10')>0);
});
test('summary retries replace their own section and preserve manual notes',()=>{
  const body=replaceSummary('# Notes\nmanual text','first');
  const changed=replaceSummary(body+'\nmanual footer','second');
  assert(changed.includes('manual text') && changed.includes('manual footer'));
  assert(!changed.includes('first'));assert.equal(changed.split('<!-- deepseek-summary:start -->').length,2);
  assert.throws(()=>replaceSummary('<!-- deepseek-summary:start -->','x'));
});
test('Responses parser accepts only completed assistant text, never reasoning',()=>{
  assert.equal(responseText(completed('发布总结')),'发布总结');
  for(const body of [{status:'incomplete',output:[]},completed(''),{status:'completed',output:[{type:'function_call'}]}, {status:'completed',output:[{type:'message',role:'assistant',status:'completed',content:[{type:'refusal',refusal:'no'}]}]}]) assert.throws(()=>responseText(body));
});
test('DeepSeek request uses the actual Responses endpoint with no file input',async()=>{
  let calls=0;
  const value=await summarize('v1.0.0\n- fix: issue',{apiKey:'fake-secret',fetchImpl:async(url,init)=>{
    calls++;assert.equal(url,'https://api.deepseek.com/responses');assert.equal(init.headers.Authorization,'Bearer fake-secret');assert.equal(init.redirect,'error');
    const body=JSON.parse(init.body);assert.equal(body.stream,false);assert.equal(body.model,'deepseek-v4-flash');assert(body.input.includes('fix: issue'));assert.equal(body.messages,undefined);
    return new Response(JSON.stringify(completed('修复问题')),{status:200});
  }});
  assert.equal(value,'修复问题');assert.equal(calls,1);
});
test('missing secret fails before any network request',async()=>{
  await assert.rejects(summarize('notes',{apiKey:'',fetchImpl:()=>assert.fail('network called')}),/DEEPSEEK_API_KEY/);
});
test('authentication errors redact upstream body and do not retry',async()=>{
  let calls=0;
  await assert.rejects(summarize('notes',{apiKey:'fake-secret',fetchImpl:async()=>{calls++;return new Response('fake-secret and private data',{status:401});}}),error=>error.message.includes('401') && !error.message.includes('fake-secret'));
  assert.equal(calls,1);
});
test('429 retries, truncated output fails, and network messages are redacted',async()=>{
  let calls=0;const waits=[];
  const result=await summarize('notes',{apiKey:'fake',sleep:async(ms)=>waits.push(ms),fetchImpl:async()=>++calls===1?new Response('',{status:429,headers:{'retry-after':'1'}}):new Response(JSON.stringify(completed('done')))});
  assert.equal(result,'done');assert.deepEqual(waits,[1000]);
  await assert.rejects(summarize('notes',{apiKey:'fake',fetchImpl:async()=>new Response(JSON.stringify({status:'incomplete',output:[]}))}),/incomplete/);
  await assert.rejects(summarize('notes',{apiKey:'fake',fetchImpl:async()=>{throw new Error('secret raw transport message');}}),error=>!error.message.includes('secret raw'));
});

import { mkdtempSync, mkdirSync, writeFileSync, chmodSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';
test('prepare resolves annotated tags and rejects unmerged branch releases',()=>{
 const dir=mkdtempSync(join(tmpdir(),'ocs-release-test-'));
 const script=fileURLToPath(new URL('../.github/scripts/release.mjs',import.meta.url));
 const git=(...args)=>{const r=spawnSync('git',args,{cwd:dir,encoding:'utf8'});assert.equal(r.status,0,r.stderr);return r.stdout.trim();};
 try {
  git('init','--initial-branch=main');git('config','user.name','Fixture');git('config','user.email','fixture@example.invalid');
  git('commit','--allow-empty','-m','initial');git('tag','v1.0.0');
  git('commit','--allow-empty','-m','feat: release summary');git('tag','-a','v1.1.0-rc.1','-m','rc');git('tag','v1.1.0');git('update-ref','refs/remotes/origin/main','HEAD');
  const run=(tag,channel)=>spawnSync(process.execPath,[script,'prepare'],{cwd:dir,encoding:'utf8',env:{...process.env,RELEASE_TAG:tag,RELEASE_CHANNEL:channel,GITHUB_REPOSITORY:'fixture/repo'}});
  let result=run('v1.1.0','release');assert.equal(result.status,0,result.stderr);
  const meta=JSON.parse(readFileSync(join(dir,'.release','metadata.json'),'utf8'));assert.equal(meta.previous,'v1.0.0');assert.equal(meta.prerelease,false);
  assert(readFileSync(join(dir,'.release','notes.md'),'utf8').includes('feat: release summary'));
  result=run('v1.1.0-rc.1','prerelease');assert.equal(result.status,0,result.stderr);
  git('switch','-c','feature');git('commit','--allow-empty','-m','unmerged');git('tag','v2.0.0');assert.notEqual(run('v2.0.0','release').status,0);
 } finally {rmSync(dir,{recursive:true,force:true});}
});

test('publish creates correct channel once and preserves notes on retries',()=>{
 const script=fileURLToPath(new URL('../.github/scripts/release.mjs',import.meta.url));
 for(const prerelease of [false,true]) {
  const dir=mkdtempSync(join(tmpdir(),'ocs-publish-test-'));
  try {
   mkdirSync(join(dir,'bin'));mkdirSync(join(dir,'.release'));
   const fake=join(dir,'bin','gh');
   writeFileSync(fake,`#!${process.execPath}
const fs=require('node:fs');
const args=process.argv.slice(2),state=process.env.FAKE_GH_STATE;
if(args[0]==='api' && args[2]==='GET'){
 if(fs.existsSync(state)){process.stdout.write(fs.readFileSync(state));process.exit(0);}
 console.error('HTTP 404');process.exit(1);
}
if(args[0]==='api' && args[2]==='POST'){
 const body=JSON.parse(fs.readFileSync(0,'utf8'));
 const release={...body,id:1,html_url:'https://github.com/fixture/repo/releases/tag/'+body.tag_name};
 fs.writeFileSync(state,JSON.stringify(release));process.stdout.write(JSON.stringify(release));process.exit(0);
}
process.exit(2);
`);chmodSync(fake,0o700);
   const tag=prerelease?'v1.0.0-rc.1':'v1.0.0',marker='<!-- ocs-bank-release commit=abc -->';
   writeFileSync(join(dir,'.release','metadata.json'),JSON.stringify({tag,prerelease,commit:'abc',marker}));
   writeFileSync(join(dir,'.release','notes.md'),marker+'\noriginal notes');
   const state=join(dir,'release.json');
   const run=()=>spawnSync(process.execPath,[script,'publish'],{cwd:dir,encoding:'utf8',env:{...process.env,PATH:join(dir,'bin')+':'+process.env.PATH,FAKE_GH_STATE:state,GITHUB_REPOSITORY:'fixture/repo'}});
   let r=run();assert.equal(r.status,0,r.stderr);
   const release=JSON.parse(readFileSync(state,'utf8'));assert.equal(release.prerelease,prerelease);assert.equal(release.make_latest,prerelease?'false':'legacy');
   release.body+='\nmanual note';writeFileSync(state,JSON.stringify(release));
   r=run();assert.equal(r.status,0,r.stderr);assert(JSON.parse(readFileSync(state,'utf8')).body.includes('manual note'));
   release.body='unowned release';writeFileSync(state,JSON.stringify(release));assert.notEqual(run().status,0);
  } finally {rmSync(dir,{recursive:true,force:true});}
 }
});
