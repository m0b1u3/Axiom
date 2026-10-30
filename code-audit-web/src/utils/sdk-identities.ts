import {validate} from '../../../contracts/ts/src/index';
import type {SDKIdentitySnapshot,SDKIdentityOperation} from '../contracts';
export const identityState:Record<SDKIdentitySnapshot['state'],string>={unconfigured:'未登录',authenticating:'正在登录',authenticated:'已认证',signed_out:'未认证',revoked:'已撤销',unknown:'状态未知'};
export const identityOperationState:Record<SDKIdentityOperation['status'],string>={queued:'等待绑定节点',running:'正在执行',waiting:'等待设备码授权',completed:'已完成',failed:'失败',canceled:'已取消',expired:'已过期或节点失联'};
export const identityAction:Record<SDKIdentityOperation['action'],string>={login_api_key:'API Key 登录',login_device_code:'ChatGPT 设备码登录',account:'查询 SDK 账户',logout:'退出与清理'};
export function checkedIdentities(value:unknown):SDKIdentitySnapshot[]{if(!Array.isArray(value))throw new Error('身份列表合同不受支持，请同步升级 Server、Agent 和 Web');for(const v of value)validate('SDKIdentitySnapshot',v);return value as SDKIdentitySnapshot[]}
export function checkedIdentityOperations(value:unknown):SDKIdentityOperation[]{if(!Array.isArray(value))throw new Error('身份操作合同不受支持');for(const v of value)validate('SDKIdentityOperation',v);return value as SDKIdentityOperation[]}
export const usableIdentity=(v:SDKIdentitySnapshot)=>v.state==='authenticated'&&v.authenticated===true&&!v.cleanupPending;

export function identitySelectionError(options: {id:string; native:boolean; required:boolean; items:SDKIdentitySnapshot[]; error?:string}):string {
 if (!options.id) return options.required ? '此模型使用授权账户，请选择已登录的账户。' : '';
 if (!options.native) return '所选模型不支持此授权账户，请选择原生 OpenAI Responses 模型，或明确改用模型配置的 API Key。';
 if (options.error) return '账户状态读取失败，请刷新账户列表后再提交。';
 const selected=options.items.find(item=>item.identity.id===options.id);
 if (!selected||!usableIdentity(selected)) return '选定账户未登录、已撤销或状态未知，请重新选择或前往授权账户页核对。';
 return '';
}
