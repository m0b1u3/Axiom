import {test} from 'node:test';
import assert from 'node:assert/strict';
import {SseParser} from '../src/proxy/sse.js';
test('Responses SSE framing preserves CRLF, multiline data, comments, UTF-8 and final tails',()=>{
 const raw=': comment\r\nevent: response.output_text.delta\r\ndata: {"value":\r\ndata: "中文 🧪"}\r\n\r\ndata: {"done":true}';
 for(let split=0;split<=raw.length;split++){
  const frames:Array<[string,string]>=[],parser=new SseParser((event,payload)=>frames.push([event,payload]));
  parser.push(raw.slice(0,split));parser.push(raw.slice(split));parser.end();
  assert.deepEqual(frames,[['response.output_text.delta','{"value":\n"中文 🧪"}'],['message','{"done":true}']]);
 }
});
test('SSE framing rejects an oversized unterminated frame',()=>{
 const parser=new SseParser(()=>{});assert.throws(()=>parser.push('x'.repeat(8*1024*1024+1)),/too large/);
});
