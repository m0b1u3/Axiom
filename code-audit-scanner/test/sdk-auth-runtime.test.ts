// Locked SDK, synthetic Codex executable. No provider authentication or network.
import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,mkdir,writeFile,readFile,rm,realpath} from 'node:fs/promises';
import {spawn} from 'node:child_process';
import path from 'node:path';import os from 'node:os';
import {fileURLToPath} from 'node:url';
import {decode,type SDKIdentityClaim,type SDKIdentityRef,type SDKIdentityReport} from '@axiom/contracts';

const scannerRoot=fileURLToPath(new URL('../',import.meta.url));
test('SDK 0.5.0 auth APIs use isolated credential state through a synthetic executable',{skip:process.platform==='win32'},async()=>{
 const temporary=await mkdtemp(path.join(os.tmpdir(),'axiom-native-auth-fixture-')),base=await realpath(temporary);
 const executable=path.join(base,'fixture-codex.cjs');
 const script=`#!/usr/bin/env node
const fs=require('node:fs'),path=require('node:path');
const args=process.argv.slice(2),home=process.env.CODEX_HOME;
if(!home||!home.startsWith(${JSON.stringify(base)}+path.sep))process.exit(90);
const file=path.join(home,'auth.json');fs.mkdirSync(home,{recursive:true,mode:0o700});
const save=kind=>fs.writeFileSync(file,JSON.stringify({fixtureOnly:true,kind}),{mode:0o600});
if(args[0]==='login'&&args[1]==='--with-api-key'){let input='';process.stdin.on('data',b=>input+=b);process.stdin.on('end',()=>{if(!input.trim())process.exit(91);save('api_key');console.log('Fixture login succeeded')})}
else if(args[0]==='login'&&args[1]==='status'){if(fs.existsSync(file))console.log('Logged in using Fixture credentials');else{console.log('Not logged in');process.exitCode=1}}
else if(args[0]==='login'&&args[1]==='--device-auth'){console.log('Open https://auth.openai.com/codex/device and enter MOCK-1234');if(process.env.AXIOM_FAKE_DEVICE_HOLD==='1'){process.on('SIGTERM',()=>process.exit(1));setInterval(()=>{},1000)}else setTimeout(()=>save('chatgpt'),100)}
else if(args[0]==='logout'){fs.rmSync(file,{force:true});console.log('Fixture logout succeeded')}
else process.exit(92);
`;
 await writeFile(executable,script,{mode:0o700});let operation=0;
 const homeFor=async(id:string)=>{const p=path.join(base,id);await mkdir(p,{mode:0o700});return p};
 const apiHome=await homeFor('api'),chatHome=await homeFor('chat');
 async function run(home:string,kind:SDKIdentityRef['kind'],action:SDKIdentityClaim['operation']['action'],cancelDevice=false):Promise<SDKIdentityReport[]>{
  const identity:SDKIdentityRef={id:kind==='api_key'?'81':'82',name:'Fixtures / Mock native SDK',agentId:'fixture-agent',generation:2,kind};
  const stamp=new Date().toISOString();const claim:SDKIdentityClaim={schemaVersion:1,operation:{schemaVersion:1,id:String(++operation),identity,action,status:'running',createdAt:stamp,updatedAt:stamp,deadline:new Date(Date.now()+20000).toISOString(),instructions:null,error:null},leaseToken:'a'.repeat(64),leaseSeconds:15,...(action==='login_api_key'?{apiKey:'fixture-native-key-not-real'}:{})};
  const input=path.join(home,'operation-'+operation+'.json');await writeFile(input,JSON.stringify(claim),{mode:0o600});
  const env={...process.env};for(const key of Object.keys(env))if(key.endsWith('_API_KEY')||['CODEX_HOME','CHATGPT_ACCESS_TOKEN','CHATGPT_ACCOUNT_ID','NODE_OPTIONS'].includes(key))delete env[key];
  Object.assign(env,{CODEX_CLI_PATH:executable,CODEX_SECURITY_STATE_DIR:path.join(home,'sdk'),CODEX_HOME:path.join(home,'ambient-empty'),AXIOM_SDK_IDENTITY_HOME:home,HOME:path.join(base,'empty-user-home'),AXIOM_FAKE_DEVICE_HOLD:cancelDevice?'1':'0'});
  const child=spawn(process.execPath,['--import','tsx','src/main.ts','--identity-operation',input],{cwd:scannerRoot,env});
  const reports:SDKIdentityReport[]=[];let buffer='',stderr='';
  child.stdout.setEncoding('utf8');child.stdout.on('data',(chunk:string)=>{buffer+=chunk;let pos;while((pos=buffer.indexOf('\n'))!==-1){const line=buffer.slice(0,pos);buffer=buffer.slice(pos+1);if(!line.trim())continue;const report=decode('SDKIdentityReport',line);reports.push(report);if(cancelDevice&&report.status==='waiting')child.kill('SIGTERM')}});
  child.stderr.on('data',b=>stderr+=b);const timer=setTimeout(()=>child.kill('SIGKILL'),25000);
  try{const code=await new Promise<number|null>((resolve,reject)=>{child.once('error',reject);child.once('close',resolve)});assert.equal(code,cancelDevice?10:0,stderr);assert.ok(reports.length>=2);assert.equal(reports.at(-1)!.status,cancelDevice?'canceled':'completed');assert.ok(!JSON.stringify(reports).includes('fixture-native-key-not-real'));return reports}finally{clearTimeout(timer)}
 }
 try{
  await run(apiHome,'api_key','login_api_key');assert.equal((await run(apiHome,'api_key','account')).at(-1)!.authenticated,true);await run(apiHome,'api_key','logout');assert.equal((await run(apiHome,'api_key','account')).at(-1)!.authenticated,false);
  const reports=await run(chatHome,'chatgpt','login_device_code');assert.equal(reports.find(v=>v.status==='waiting')!.instructions!.userCode,'MOCK-1234');assert.equal((await run(chatHome,'chatgpt','account')).at(-1)!.authenticated,true);await run(chatHome,'chatgpt','logout');
  const canceled=await run(chatHome,'chatgpt','login_device_code',true);assert.equal(canceled.at(-1)!.instructions,null);await assert.rejects(()=>readFile(path.join(chatHome,'session.json')));
 }finally{await rm(base,{recursive:true,force:true})}
});
