import type {QueueOverview,QueueTask,ScanTask} from '../types';

// Detail and overview requests can settle on opposite sides of a retry. Keep
// an old queue observation out of the new execution's current-state display.
export function queueForExecution(task:Pick<ScanTask,'id'|'status'|'startedAt'>,overview:QueueOverview|null):QueueTask|undefined {
 const queued=overview?.tasks.find(row=>row.taskId===task.id);
 if(!queued||queued.status!==task.status)return;
 if(task.status==='pending'&&(!Number.isSafeInteger(queued.position)||queued.position<1))return;
 if(task.status!=='pending'&&task.startedAt&&queued.startedAt){
  const current=Date.parse(task.startedAt),observed=Date.parse(queued.startedAt);
  if(Number.isFinite(current)&&Number.isFinite(observed)&&current!==observed)return;
 }
 return queued;
}

export const queueReasons: Record<string,string> = {
 sdk_concurrency_incompatible:"等待支持 SDK 原生并发参数的配套执行节点，请同步更新 Agent 与 Scanner",
 token_budget_incompatible:"等待支持配置单价预算控制的配套执行节点",
 sdk_parameters_incompatible:"等待支持推理强度与组件知识、指令的配套执行节点",
 component_scan_incompatible:'等待支持冻结组件审计的配套执行节点',
 git_diff_incompatible:'等待支持冻结 Git 提交差异的配套执行节点',
 cost_unknown:"费用未知，需先核对估算", storage_low:"存储空间不足，等待释放空间",storage_unknown:"存储容量未知，暂停新领取",storage_cleanup:"数据清理中",budget_exhausted:"累计预算已用尽",
 matrix_incompatible:"执行组件的兼容矩阵不匹配",scanner_incompatible:"执行节点的 Scanner 预检被阻止",control_incompatible:"等待支持当前控制接口的执行节点",task_contract_unsupported:"任务合同版本不受支持",
 sdk_operation_incompatible:'等待支持 SDK 预检与独立复核的配套执行节点', scan_input_incompatible:'等待支持高级输入与知识文档的执行节点', observation_incompatible:"等待支持进度与事件合同的执行节点",result_incompatible:"等待支持完整结果合同的执行节点",input_incompatible:'等待支持当前模型配置和预算的执行节点', agent_offline:'执行机离线，等待恢复', global_limit:'全局并发已满', model_limit:'该模型配置的并发已满', project_limit:'该项目的并发已满', agent_capacity:'执行机槽位已满', awaiting_dispatch:'等待调度领取', lease_expired:'执行心跳已过期', cancel_requested:'正在停止扫描',
};
export function elapsed(start: string | null | undefined,end?: string): string {
 if(!start)return '—';const a=Date.parse(start),b=end?Date.parse(end):Date.now();if(!Number.isFinite(a)||!Number.isFinite(b))return '—';const s=Math.max(0,Math.floor((b-a)/1000));return s<60?`${s} 秒`:s<3600?`${Math.floor(s/60)} 分 ${s%60} 秒`:`${Math.floor(s/3600)} 小时 ${Math.floor(s%3600/60)} 分`;
}
export function failureHelp(code: string, status: string): {title:string;action:string} | null {
 if(!code && !['failed','timeout','partial','abandoned'].includes(status))return null;
 const map:Record<string,[string,string]>={
 quota_exhausted:['模型额度已耗尽','在供应商控制台补充额度或关闭仅使用免费额度；恢复后再手动重试。'],
 rate_limit:['模型请求受限','检查供应商额度，等待限流解除后重试；可降低该模型的任务并发。'],
 authentication_error:['模型认证失败','在模型配置中检查 API Key 和访问权限，测试连接成功后重试。'],
 provider_unavailable:['模型服务连接失败','检查接口地址、网络和供应商服务状态，恢复后手动重试。'],
 timeout:['扫描超时','可缩小扫描范围，或新建扫描并调整超时时间。重试会沿用本任务参数。'],
 protocol_version_unsupported:['执行组件版本不兼容','升级配套的 Server、执行节点和 Scanner。任务参数保持原快照，组件兼容后再重试。'],
 configuration_error:['模型配置不可用','检查模型配置是否存在及密钥是否可用，修复后重试。'],
 lease_expired:['执行机心跳中断','先查看「扫描中断与恢复」。有完整提交结果时可恢复入库；否则需确认重新执行，可能重复分析和产生费用。部署中的自动恢复同样属于重新执行。'],
 invalid_result:['扫描结果格式异常','查看活动流中的错误与原始结果，检查扫描引擎和模型兼容性。'],
 canceled:['扫描已取消','取消不能当作暂停续扫。可恢复已提交结果，或确认重新执行并创建新的执行记录。'],
 };
 const v=map[code] ?? (status==='partial'?['扫描部分完成','已返回结果可供检查，请结合日志确认未完成的阶段；需要时重新扫描。']:['扫描执行失败','展开原始错误并检查活动流。修复原因后可手动重试。']);
 return {title:v[0]!,action:v[1]!};
}
