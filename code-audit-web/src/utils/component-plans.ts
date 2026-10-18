import {validate} from '../../../contracts/ts/src/index';
import type {ComponentPlanDocument,ComponentPlanSnapshot,ComponentPlanVersion,ComponentPlanningResult,ComponentSource} from '../contracts';
export type {ComponentPlanView} from '../contracts';
import type {ComponentPlanView} from '../contracts';
export interface ComponentVersionItem {version:number;revision:number;sha256:string;confirmedAt:string;confirmedBy:string}
export function sameComponentSource(a:ComponentSource,b:ComponentSource):boolean{return a.snapshotId===b.snapshotId&&a.contentHash===b.contentHash&&a.headCommit===b.headCommit}
export function componentDocument(text:string):ComponentPlanDocument{let value:unknown;try{value=JSON.parse(text)}catch{throw new Error('计划 JSON 无法解析，请检查逗号和引号。')}try{validate('ComponentPlanDocument',value)}catch{throw new Error('计划需要 1–128 个组件，每个组件包含名称及 1–64 个规范相对路径。根目录使用 .；不能包含绝对路径或上级路径。')}return value as ComponentPlanDocument}
export function componentView(value:ComponentPlanView):ComponentPlanView{
 validate('ComponentPlanView',value);
 if(value.result){validate('ComponentPlanningResult',value.result);if(value.result.execution.taskId!==value.operationTaskId||value.result.operation.planId!==value.plan.id||!sameComponentSource(value.result.operation.source,value.plan.source))throw new Error('组件规划结果与当前计划或源码快照不匹配，请重新读取。');}
 return value;
}
export function canFreezeComponentPlan(v:ComponentPlanView):boolean{return !!v.result&&v.operationStatus==='completed'&&v.result.operation.kind==='normalize'&&v.result.operation.revision===v.plan.revision&&v.result.error===null&&v.result.coverage?.valid===true&&!!v.resultSha256&&/^[a-f0-9]{64}$/.test(v.resultSha256)}
export function checkedComponentVersion(v:ComponentPlanVersion,plan:ComponentPlanSnapshot):ComponentPlanVersion{validate('ComponentPlanVersion',v);if(v.planId!==plan.id||!sameComponentSource(v.source,plan.source))throw new Error('冻结版本与当前计划的源码绑定不匹配。');return v}
export const componentOperationState=(status:string)=>({pending:'等待执行节点',claimed:'正在准备源码副本',scanning:'正在规划或校验',cancel_requested:'正在取消',completed:'操作已完成',failed:'操作失败',canceled:'已取消',timeout:'操作超时',partial:'操作未完整结束'}[status]??'尚未执行');
