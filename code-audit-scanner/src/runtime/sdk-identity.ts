import {lstat,mkdir,readFile,realpath,writeFile,rm,rename,mkdtemp} from 'node:fs/promises';
import path from 'node:path';
import {decode,validate,type SDKIdentityClaim,type SDKIdentityReport,type SDKTaskIdentity,type SDKIdentityRef} from '@axiom/contracts';
import type {ScanTask} from '../contract/scan-task.js';

const credentialName=(n:string)=>n.toUpperCase().endsWith('_API_KEY')||['CODEX_HOME','CHATGPT_ACCESS_TOKEN','CHATGPT_ACCOUNT_ID'].includes(n.toUpperCase());
const sameIdentity=(a:SDKIdentityRef,b:SDKIdentityRef)=>(['id','name','agentId','generation','kind'] as const).every(key=>a[key]===b[key]);
export async function loadTaskIdentity(file:string,task:ScanTask):Promise<void>{
 const binding=decode('SDKTaskIdentity',await readFile(file,'utf8'),{taskId:String(task.taskId),attempt:task.executionAttempt!});
 if(task.provider!=='openai'||task.connectionType!=='openai'||task.wireApi!=='responses'||(task.baseUrl&&task.baseUrl!=='https://api.openai.com/v1'))throw new Error('SDK identity requires native OpenAI');
 const home=process.env.AXIOM_SDK_IDENTITY_HOME;if(!home||await realpath(home)!==path.resolve(home))throw new Error('SDK identity directory unavailable');
 const marker=decode('SDKIdentityRef',await readFile(path.join(home,'session.json'),'utf8'));
 if(!sameIdentity(marker,binding.identity))throw new Error('SDK identity session differs from task binding');
 const expectedHome=path.join(home,'sdk/codex-home');if(process.env.CODEX_HOME!==expectedHome)throw new Error('SDK identity credential source differs');
 const root=process.env.CODEX_SECURITY_STATE_DIR;if(!root||!path.resolve(root).startsWith(path.join(home,'executions')+path.sep))throw new Error('SDK identity execution state is not private');
 await mkdir(root,{recursive:true,mode:0o700});if(await realpath(root)!==path.resolve(root))throw new Error('SDK identity state must not be a symlink');
 if(binding.identity.kind==='chatgpt'&&Object.keys(process.env).some(n=>credentialName(n)&&n!=='CODEX_HOME'))throw new Error('ChatGPT identity cannot inherit API credentials');
 if(binding.identity.kind==='api_key'&&!process.env.OPENAI_API_KEY)throw new Error('Selected API identity is unavailable');
 task.sdkIdentity=binding;
}
export function selectedSDKAuth(task:ScanTask):'api-key'|'chatgpt'{return task.sdkIdentity?.identity.kind==='chatgpt'?'chatgpt':'api-key'}

export async function runIdentityOperation(file:string):Promise<void>{
 const info=await lstat(file);if(!info.isFile()||info.isSymbolicLink()||info.size>64*1024)throw new Error('Invalid identity operation file');
 const claim=decode('SDKIdentityClaim',await readFile(file,'utf8')),op=claim.operation;
 const home=process.env.AXIOM_SDK_IDENTITY_HOME;
 if(!home||await realpath(home)!==path.resolve(home)||path.dirname(await realpath(file))!==home||process.env.CODEX_SECURITY_STATE_DIR!==path.join(home,'sdk'))throw new Error('Invalid identity operation directory');
 if(Object.keys(process.env).some(n=>credentialName(n)&&n!=='CODEX_HOME'))throw new Error('Identity operation inherited unrelated credentials');
 let sequence=0;const report=(status:SDKIdentityReport['status'],authenticated:boolean|null=null,instructions:SDKIdentityReport['instructions']=null,error:string|null=null)=>{
  const v:SDKIdentityReport={schemaVersion:1,operationId:op.id,identityId:op.identity.id,agentId:op.identity.agentId,generation:op.identity.generation,leaseToken:claim.leaseToken,sequence:++sequence,status,authenticated,instructions,error};validate('SDKIdentityReport',v);process.stdout.write(JSON.stringify(v)+'\n');
 };
 const {CodexSecurity}=await import('@openai/codex-security');const security=new CodexSecurity();const controller=new AbortController();let handle:Awaited<ReturnType<typeof security.loginChatGPTDeviceCode>>|undefined;
 const abort=()=>{controller.abort();handle?.cancel();void security.close().catch(()=>{})};process.once('SIGTERM',abort);process.once('SIGINT',abort);
 const timer=setTimeout(abort,Math.max(1,Date.parse(op.deadline)-Date.now()));
 report('running');
 try{
  switch(op.action){
   case 'login_api_key':
    if(op.identity.kind!=='api_key'||!claim.apiKey)throw new Error('API credential missing');
    await security.loginApiKey(claim.apiKey);await writeFile(path.join(home,'selected-api-key'),claim.apiKey,{mode:0o600});break;
   case 'login_device_code':{
    if(op.identity.kind!=='chatgpt'||claim.apiKey!==undefined)throw new Error('Invalid device identity');
    handle=await security.loginChatGPTDeviceCode();
    const instructions={verificationUrl:handle.verificationUrl??'',userCode:handle.userCode??'',expiresAt:op.deadline};validate('SDKDeviceInstructions',instructions);
    report('waiting',null,instructions);
    if(!(await handle.wait()).success)throw new Error('Device login did not complete');break;
   }
   case 'account':break;
   case 'logout':await security.logout();await rm(path.join(home,'selected-api-key'),{force:true});await rm(path.join(home,'session.json'),{force:true});report('completed',false);return;
  }
  if(controller.signal.aborted)throw new Error('Identity operation canceled');
  const status=await security.account();
  if((op.action==='login_api_key'||op.action==='login_device_code')&&!status.authenticated)throw new Error('SDK did not confirm authentication');
  if(status.authenticated)await writeFile(path.join(home,'session.json'),JSON.stringify(op.identity)+'\n',{mode:0o600});
  report('completed',status.authenticated);
 }catch{
  if(op.action==='login_api_key'||op.action==='login_device_code'){await rm(path.join(home,'selected-api-key'),{force:true});await rm(path.join(home,'session.json'),{force:true});}
  report(controller.signal.aborted?'canceled':'failed',null,null,'SDK 身份操作未完成；请在 Web 核对节点与登录状态');process.exitCode=10;
 }finally{clearTimeout(timer);process.off('SIGTERM',abort);process.off('SIGINT',abort);await security.close();}
}

/** Preserve refreshed credentials using the SDK's public opaque copy helper.
 * The Server serializes top-level executions for one identity, so no parallel
 * task can overwrite a newer session. Workbench and source data stay per task.
 */
export async function persistTaskIdentity(task:ScanTask):Promise<void>{
 if(task.sdkIdentity?.identity.kind!=='chatgpt')return;
 const home=process.env.AXIOM_SDK_IDENTITY_HOME,root=process.env.CODEX_SECURITY_STATE_DIR;
 if(!home||!root||await realpath(home)!==path.resolve(home)||!path.resolve(root).startsWith(path.join(home,'executions')+path.sep))throw new Error('SDK session persistence boundary differs');
 const marker=decode('SDKIdentityRef',await readFile(path.join(home,'session.json'),'utf8'));
 if(!sameIdentity(marker,task.sdkIdentity.identity))throw new Error('SDK session generation changed');
 const directory=path.join(home,'sdk');await mkdir(directory,{recursive:true,mode:0o700});
 const next=await mkdtemp(path.join(directory,'.session-next-')),current=path.join(directory,'codex-home'),previous=next+'-previous';let moved=false;
 try{
  const {importAmbientAuth}=await import('@openai/codex-security');
  if(!await importAmbientAuth(path.join(root,'codex-home'),next))return;
  await rename(current,previous);moved=true;
  try{await rename(next,current)}catch(error){await rename(previous,current);moved=false;throw error}
  await rm(previous,{recursive:true,force:true});moved=false;
 }catch{
  // Unknown persistence never silently uses an unrelated or stale credential.
  await rm(path.join(home,'session.json'),{force:true});throw new Error('SDK 会话刷新保存未确认，请在 Web 查询账户状态或重新创建登录');
 }finally{await rm(next,{recursive:true,force:true});if(moved)await rm(previous,{recursive:true,force:true});}
}
