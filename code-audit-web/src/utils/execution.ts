export const queueReasons: Record<string,string> = {
 cost_unknown:"费用未知，需先核对估算", storage_low:"存储空间不足，等待释放空间",storage_unknown:"存储容量未知，暂停新领取",storage_cleanup:"数据清理中",budget_exhausted:"累计预算已用尽",
 matrix_incompatible:"执行组件的兼容矩阵不匹配",scanner_incompatible:"执行节点的 Scanner 预检被阻止",control_incompatible:"等待支持当前控制接口的执行节点",task_contract_unsupported:"任务合同版本不受支持",
 observation_incompatible:"等待支持进度与事件合同的执行节点",result_incompatible:"等待支持完整结果合同的执行节点",input_incompatible:'等待支持当前模型配置和预算的执行节点', agent_offline:'执行机离线，等待恢复', global_limit:'全局并发已满', model_limit:'该模型配置的并发已满', project_limit:'该项目的并发已满', agent_capacity:'执行机槽位已满', awaiting_dispatch:'等待调度领取', lease_expired:'执行心跳已过期', cancel_requested:'正在停止扫描',
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
 lease_expired:['执行机心跳中断','检查执行机在线状态和运行日志。自动恢复次数由部署配置决定；可在任务记录查看，恢复后再手动重试。'],
 invalid_result:['扫描结果格式异常','查看活动流中的错误与原始结果，检查扫描引擎和模型兼容性。'],
 canceled:['扫描已取消','需要继续时可重新扫描，将创建新的执行记录。'],
 };
 const v=map[code] ?? (status==='partial'?['扫描部分完成','已返回结果可供检查，请结合日志确认未完成的阶段；需要时重新扫描。']:['扫描执行失败','展开原始错误并检查活动流。修复原因后可手动重试。']);
 return {title:v[0]!,action:v[1]!};
}
