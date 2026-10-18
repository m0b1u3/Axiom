import {validate} from '../../../contracts/ts/src/index';
import type {ComponentScanRequest,ComponentScanSnapshot,ComponentScanReceipt,ComponentFindingBinding,ComponentScanResult} from '../contracts';
export type ComponentAttempt={request:ComponentScanRequest;snapshot:ComponentScanSnapshot|null;status:string};
export interface ComponentExecutionsView {request:ComponentScanRequest;attempts:ComponentAttempt[]}
const same=(a:unknown,b:unknown)=>JSON.stringify(a)===JSON.stringify(b);
export function checkedComponentExecutions(value:unknown,taskId:number):ComponentExecutionsView{
 const v=value as ComponentExecutionsView;
 validate('ComponentScanRequest',v.request);if(v.request.execution.taskId!==String(taskId)||!Array.isArray(v.attempts))throw new Error('组件父任务身份不匹配');
 for(const a of v.attempts){validate('ComponentScanRequest',a.request);if(a.request.execution.taskId!==String(taskId)||a.request.planVersion.sha256!==v.request.planVersion.sha256||!same(a.request.planVersion.source,v.request.planVersion.source))throw new Error('组件历史版本身份不匹配');if(a.snapshot){validate('ComponentScanSnapshot',a.snapshot,a.request.execution);if(a.snapshot.versionSha256!==a.request.planVersion.sha256||a.snapshot.planId!==a.request.planVersion.planId||!same(a.snapshot.source,a.request.planVersion.source))throw new Error('组件进度源码身份不匹配')}}
 return v;
}
export function componentRows(v:ComponentExecutionsView,attempt?:number):Array<{index:number;receipt:ComponentScanReceipt|null;attempt:number|null}>{
 const latest=new Map<number,{receipt:ComponentScanReceipt;attempt:number}>();
 for(const a of [...v.attempts].sort((a,b)=>b.request.execution.attempt-a.request.execution.attempt)){
  if(attempt!==undefined&&a.request.execution.attempt!==attempt)continue;
  for(const receipt of a.snapshot?.components??[])if(!latest.has(receipt.index))latest.set(receipt.index,{receipt,attempt:a.request.execution.attempt});
 }
 return v.request.planVersion.document.components.map((_,index)=>({index,receipt:latest.get(index)?.receipt??null,attempt:latest.get(index)?.attempt??null}));
}
export const componentStatus=(status?:string)=>({pending:'等待执行',running:'运行中',completed:'已完成',partial:'部分完成',failed:'失败',canceled:'已取消'}[status??'']??'尚未执行');
export const componentMoney=(micros:number|null|undefined)=>micros==null?'未知':(micros/1e6).toFixed(6).replace(/\.?0+$/,'')||'0';
export function componentBinding(value:unknown):ComponentFindingBinding|null{try{validate('ComponentFindingBinding',value);return value}catch{return null}}
export function componentResult(value:unknown):ComponentScanResult|null{try{validate('ComponentScanResult',value);return value}catch{return null}}
