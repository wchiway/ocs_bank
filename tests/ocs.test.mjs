import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';
const [wrapper] = JSON.parse(readFileSync(new URL('../examples/ocs.json', import.meta.url)));
const handle = new Function(wrapper.handler)();
test('OCS wrapper returns answers from nested business response', () => {
  assert.deepEqual(handle({success:true,data:{code:1,data:'a###b'}}),[undefined,'a###b']);
});
test('OCS wrapper rejects missing answers and error responses', () => {
  for(const value of [null,{}, {success:true,data:{code:0,data:null}}, {success:true,data:{code:1,data:''}}]) assert.equal(handle(value),undefined);
});
test('OCS business errors throw safe messages instead of becoming answers', () => {
  for (const [code, pattern] of [['output_limit', /输出达到上限/], ['answer_format', /JSON/], ['answer_invalid', /校验/], ['unauthorized', /令牌/], ['timeout', /超时/], ['busy', /繁忙/]]) {
    assert.throws(() => handle({success:false,error_code:code,error:'private upstream body'}), pattern);
  }
  assert.throws(() => handle({success:false,error_code:'unknown',error:'private upstream body'}), error => error instanceof Error && !error.message.includes('private'));
});
test('OCS GM transport can deliver business failures to the handler', () => {
  const request = (status, body) => status === 200 ? Promise.resolve(JSON.parse(body)) : Promise.reject(body);
  return request(200, JSON.stringify({success:false,error_code:'output_limit'}))
    .then(handle)
    .then(() => assert.fail('failure became an answer'), error => assert.match(error.message, /输出达到上限/));
});
test('OCS request retains the three expected placeholders', () => {
  assert.equal(new URL(wrapper.url).pathname,'/ocs/query');
  assert.equal(wrapper.type,'GM_xmlhttpRequest');
  assert.equal(wrapper.method,'get');
  assert.deepEqual(wrapper.data,{title:'${title}',options:'${options}',type:'${type}'});
});
