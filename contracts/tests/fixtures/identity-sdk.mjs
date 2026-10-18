// Fixtures / Mock only. No real authentication, account, audit, or network calls.
import {mkdir,readFile,writeFile,appendFile,rm} from 'node:fs/promises';
import path from 'node:path';
import {CodexSecurity as InputSDK} from './input-sdk.mjs';
import {importAmbientAuth} from '../../../code-audit-scanner/node_modules/@openai/codex-security/dist/index.js';
export {importAmbientAuth};
export * from './input-sdk.mjs';
const enabled=()=>{if(process.env.AXIOM_IDENTITY_TEST_FIXTURE!=='1'||!process.env.AXIOM_INPUT_TEST_CAPTURE)throw new Error('Explicit identity fixture required')};
const state=()=>path.join(process.env.CODEX_SECURITY_STATE_DIR,'codex-home','auth.json');
async function record(action,extra={}){enabled();const file=process.env.AXIOM_INPUT_TEST_CAPTURE;await mkdir(path.dirname(file),{recursive:true});const v={fixtureOnly:true,action,leakedServerCredentials:['AXIOM_DATABASE_URL','AXIOM_AGENT_TOKEN','AXIOM_MASTER_KEY','AXIOM_JWT_SECRET','AXIOM_ADMIN_PASSWORD'].some(k=>process.env[k]!==undefined),ambientKeyCount:Object.keys(process.env).filter(k=>k.endsWith('_API_KEY')).length,...extra};await appendFile(file+'.auth',JSON.stringify(v)+'\n')}
async function persist(kind){await mkdir(path.dirname(state()),{recursive:true,mode:0o700});await writeFile(state(),JSON.stringify({fixtureOnly:true,kind}),{mode:0o600})}
export class CodexSecurity extends InputSDK{
 async loginApiKey(key){await record('login_api_key',{nonempty:!!key});if(process.env.AXIOM_IDENTITY_TEST_SCENARIO==='api-failure')throw new Error('Bearer fixture-secret-never-log');await persist('api_key')}
 async loginChatGPTDeviceCode(){await record('login_device_code');let canceled=false;const scenario=process.env.AXIOM_IDENTITY_TEST_SCENARIO;const handle={verificationUrl:scenario==='bad-url'?'https://evil.invalid/steal':'https://auth.openai.com/codex/device',userCode:'MOCK-1234',cancel(){canceled=true},async wait(){if(scenario==='hold'){while(!canceled){try{await readFile(process.env.AXIOM_INPUT_TEST_CAPTURE+'.release');break}catch{}await new Promise(r=>setTimeout(r,50))}}else await new Promise(r=>setTimeout(r,80));if(canceled||scenario==='reject')return {success:false,exitCode:1,stdout:'fixture-private',stderr:'fixture-private'};await persist('chatgpt');return {success:true,exitCode:0,stdout:'fixture-private',stderr:''}}};this.identityHandle=handle;return handle}
 async account(){await record('account');if(process.env.AXIOM_IDENTITY_TEST_SCENARIO==='account-failure')throw new Error('fixture-account-sensitive-details');const yes=!!(await readFile(state()).catch(()=>null));return {authenticated:yes,details:'fixture-private-account-details-do-not-publish'}}
 async logout(){await record('logout');if(process.env.AXIOM_IDENTITY_TEST_SCENARIO==='logout-failure')throw new Error('fixture-logout-sensitive-details');await rm(state(),{force:true})}
 async run(repository,options){if(process.env.AXIOM_SDK_IDENTITY_HOME&&!process.env.OPENAI_API_KEY){await importAmbientAuth(process.env.CODEX_HOME,path.dirname(state()));await writeFile(state(),JSON.stringify({fixtureOnly:true,kind:'chatgpt',refreshedByFixture:true}),{mode:0o600})}await record('run',{auth:options?.auth,credentialPresent:!!process.env.OPENAI_API_KEY,selectedIdentity:!!process.env.AXIOM_SDK_IDENTITY_HOME});return super.run(repository,options)}
 async preflight(repository,options){await record('preflight',{auth:options?.auth});return super.preflight(repository,options)}
 async validate(options){await record('validate',{auth:options?.auth});return super.validate(options)}
 async close(){this.identityHandle?.cancel();return super.close()}
}
