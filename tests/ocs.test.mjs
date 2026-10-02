import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';
const [wrapper] = JSON.parse(readFileSync(new URL('../examples/ocs.json', import.meta.url)));
const handle = new Function(wrapper.handler)();
test('OCS wrapper returns answers from nested business response', () => {
  assert.deepEqual(handle({success:true,data:{code:1,data:'a###b'}}),[undefined,'a###b']);
});
test('OCS wrapper rejects missing answers and error responses', () => {
  for(const value of [null,{}, {success:false,error:'failed'}, {success:true,data:{code:0,data:null}}, {success:true,data:{code:1,data:''}}]) assert.equal(handle(value),undefined);
});
test('OCS request retains the three expected placeholders', () => {
  assert.equal(wrapper.type,'GM_xmlhttpRequest');
  assert.equal(wrapper.method,'get');
  assert.deepEqual(wrapper.data,{title:'${title}',options:'${options}',type:'${type}'});
});
