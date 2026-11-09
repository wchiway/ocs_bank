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
test('OCS image requests use POST JSON and retain original question fields', () => {
  assert.equal(new URL(wrapper.url).pathname,'/ocs/query');
  assert.equal(wrapper.type,'GM_xmlhttpRequest');
  assert.equal(wrapper.method,'post');
  assert.equal(wrapper.headers['Content-Type'],'application/json');
  assert.equal(wrapper.data.title,'${title}');
  assert.equal(wrapper.data.options,'${options}');
  assert.equal(wrapper.data.type,'${type}');
  assert.equal(wrapper.data.suggestion_title,'${suggestion_title}');
  assert.equal(wrapper.data.suggestion_options,'${suggestion_options}');
});
test('OCS field parser preserves image arrays and their order', () => {
  const images = ['data:image/png;base64,first','data:image/jpeg;base64,second'];
  const parse = new Function(wrapper.data.images.handler)();
  assert.deepEqual(parse({images}),images);
  for (const env of [{}, {images:null}, {images:'not an array'}]) assert.deepEqual(parse(env),[]);
});
test('OCS image errors produce safe actionable messages', () => {
  assert.throws(() => handle({success:false,error_code:'image_invalid',error:'private image data'}), /图片数据无效/);
  assert.throws(() => handle({success:false,error_code:'request_too_large',error:'private image data'}), /请求过大/);
});
