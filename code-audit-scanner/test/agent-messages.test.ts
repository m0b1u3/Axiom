import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {mkdtempSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {ResponsesAgentMessages,AgentMessageCompatibilityError,deepSeekAgentMessageCompatibility} from '../src/proxy/agent-messages.js';
import {startReliableUpstream} from '../src/proxy/reliable-upstream.js';

const bytes=(value:unknown)=>Buffer.from(JSON.stringify(value));
const dispatch=(message:string,name='spawn_agent',namespace='collaboration')=>({type:'function_call',namespace,name,arguments:JSON.stringify({task_name:'fixture',fork_turns:'none',message})});
const envelope=(content:unknown[],id='amsg-fixture')=>({type:'agent_message',id,author:'/root',recipient:'/root/fixture',content});
const header={type:'input_text',text:'Message Type: NEW_TASK\nTask name: /root/fixture\nSender: /root\nPayload:\n'};
const hidden=(text:string)=>({type:'encrypted_content',encrypted_content:text});
const format={type:'json_schema',name:'fixture',strict:true,schema:{type:'object',properties:{token:{type:'string'}},required:['token'],additionalProperties:false}};

test('DeepSeek message compatibility is scoped to the exact connection and native V2',()=>{
 assert.ok(deepSeekAgentMessageCompatibility('deepseek','https://api.deepseek.com/','official-v2'));
 for(const [provider,url,runtime] of [['openai','https://api.openai.com/v1','official-v2'],['custom','https://api.deepseek.com','official-v2'],['deepseek','https://other.invalid','official-v2'],['deepseek','https://api.deepseek.com/v1','official-v2'],['deepseek','https://api.deepseek.com','legacy-v1']] as const)assert.equal(deepSeekAgentMessageCompatibility(provider,url,runtime),false);
});

for(const tool of ['spawn_agent','followup_task','send_message'])test(`native ${tool} plaintext reaches the exact public user content without changing other input`,()=>{
 const bridge=new ResponsesAgentMessages(),message='Fixtures / Mock token\n  indentation 😀';
 bridge.observe({type:'response.output_item.done',item:dispatch(message,tool)});
 const opaque={type:'reasoning',id:'rs-server',encrypted_content:'gAAAA-opaque-do-not-decrypt'};
 const source={input:[envelope([header,hidden(message)]),opaque,{type:'function_call_output',call_id:'call-fixture',output:'source\nunchanged'}],text:{format},tools:[{type:'function',name:'fixture'}],reasoning:{effort:'high'},store:false};
 const saved=structuredClone(source),actual=JSON.parse(bridge.prepare(bytes(source)).toString());
 assert.deepEqual(source,saved);assert.deepEqual(actual.input[0],{type:'message',role:'user',content:[header,{type:'input_text',text:message}]});
 assert.deepEqual(actual.input.slice(1),source.input.slice(1));for(const key of ['text','tools','reasoning','store'])assert.deepEqual(actual[key],(source as any)[key]);
 const normalized=bridge.prepare(bytes(source));assert.strictEqual(bridge.prepare(normalized),normalized);
});

test('plaintext child replies remain readable by the parent, including exact attribution',()=>{
 const bridge=new ResponsesAgentMessages(),reply={type:'input_text',text:'Message Type: MESSAGE\nTask name: /root\nSender: /root/fixture\nPayload:\nActual child reply\n'};
 const actual=JSON.parse(bridge.prepare(bytes({input:[{...envelope([reply]),author:'/root/fixture',recipient:'/root'}]})).toString());
 assert.deepEqual(actual.input[0].content,[reply]);assert.equal(actual.input[0].role,'user');
});

test('unknown or opaque hidden content is refused without leaking it in an error',()=>{
 for(const observed of [undefined,dispatch('another message'),dispatch('gAAAA-secret-fixture','read_file'),dispatch('gAAAA-secret-fixture','spawn_agent','functions')]){
  const bridge=new ResponsesAgentMessages();if(observed)bridge.observe({output:[observed]});
  assert.throws(()=>bridge.prepare(bytes({input:[envelope([header,hidden('gAAAA-secret-fixture')])]})),(e:unknown)=>e instanceof AgentMessageCompatibilityError&&e.code==='agent_message_payload_unverified'&&!e.message.includes('gAAAA'));
 }
});

test('dispatch history can prove a saved plaintext payload; references and unknown content never disappear',()=>{
 const bridge=new ResponsesAgentMessages(),message='Fixture persisted dispatch';
 const body={input:[dispatch(message),envelope([header,hidden(message)])]};
 const actual=JSON.parse(bridge.prepare(bytes(body)).toString());assert.deepEqual(actual.input[0],body.input[0]);assert.equal(actual.input[1].content[1].text,message);
 for(const item of [envelope([{type:'input_image',image_url:'data:fixture'}]),{...envelope([header]),author:null},envelope([])])assert.throws(()=>bridge.prepare(bytes({input:[item]})),AgentMessageCompatibilityError);
 assert.throws(()=>bridge.prepare(bytes({input:[envelope([header]),{type:'item_reference',id:'amsg-fixture'}]})),(e:unknown)=>e instanceof AgentMessageCompatibilityError&&e.code==='agent_message_reference_unsupported');
});

test('bounded provenance never evicts an already admitted payload or exposes a new one',()=>{
 const bridge=new ResponsesAgentMessages(1);
 bridge.observe({type:'response.completed',response:{output:[dispatch('first')]}});bridge.observe({output:[dispatch('second')]});
 assert.equal(JSON.parse(bridge.prepare(bytes({input:[envelope([hidden('first')])]})).toString()).input[0].content[0].text,'first');
 assert.throws(()=>bridge.prepare(bytes({input:[envelope([hidden('second')])]})),AgentMessageCompatibilityError);
});

test('wire round trip observes restored namespace calls before dispatch, preserves Schema and rejects unverified bodies before sending',async()=>{
 const folder=mkdtempSync(path.join(tmpdir(),'axiom-agent-message-'));let calls=0;const received:any[]=[];
 const up=createServer(async(req,res)=>{
  let raw='';for await(const chunk of req)raw+=chunk;const body=JSON.parse(raw);received.push(body);calls++;
  const output=calls===1?[{type:'function_call',name:body.tools[0].name,call_id:'call-fixture',arguments:JSON.stringify({task_name:'fixture',fork_turns:'none',message:'Fixture wire token'})}]:[{type:'message',role:'assistant',content:[{type:'output_text',text:'Fixture wire token'}]}];
  res.writeHead(200,{'content-type':'text/event-stream'});res.end(`data: ${JSON.stringify({type:'response.completed',response:{status:'completed',output}})}\n\n`);
 });
 await new Promise<void>(resolve=>up.listen(0,'127.0.0.1',resolve));
 const proxy=await startReliableUpstream(`http://127.0.0.1:${(up.address() as any).port}`,path.join(folder,'gate.sqlite'),()=>{},undefined,'native_schema',false,undefined,{agentMessages:true});
 try{
  const response=await fetch(proxy.baseUrl+'/responses',{method:'POST',body:JSON.stringify({input:'Fixtures / Mock only',tools:[{type:'namespace',name:'collaboration',tools:[{type:'function',name:'spawn_agent',parameters:{type:'object'}}]}],text:{format}})});const text=await response.text();assert.match(text,/"namespace":"collaboration"/);
  const body={input:[envelope([header,hidden('Fixture wire token')])],text:{format}};
  const child=await fetch(proxy.baseUrl+'/responses',{method:'POST',body:JSON.stringify(body)});assert.equal(child.status,200);await child.text();
  assert.deepEqual(received[1].text,body.text);assert.deepEqual(received[1].input[0],{type:'message',role:'user',content:[header,{type:'input_text',text:'Fixture wire token'}]});
  const rejected=await fetch(proxy.baseUrl+'/responses',{method:'POST',body:JSON.stringify({input:[envelope([hidden('private-unproved-fixture')])]})});assert.equal(rejected.status,400);assert.equal((await rejected.json() as any).error.code,'agent_message_payload_unverified');assert.equal(calls,2);
 }finally{await proxy.close();up.closeAllConnections();await new Promise<void>(resolve=>up.close(()=>resolve()));rmSync(folder,{recursive:true,force:true});}
});

test('ordinary messages and opaque server items are returned byte-for-byte without conversion',()=>{
 const bridge=new ResponsesAgentMessages();
 for(const value of [{input:'Fixture text'},{input:[{type:'message',role:'user',content:'Fixture text'}]},{input:[{type:'reasoning',encrypted_content:'opaque'}]}]){
  const raw=bytes(value);assert.strictEqual(bridge.prepare(raw),raw);
 }
});
