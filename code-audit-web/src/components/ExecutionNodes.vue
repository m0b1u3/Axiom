<script setup lang="ts">
import type {AgentPresence,TaskCompatibility} from '../types';
import {absoluteTime} from '../utils/time';
const props=defineProps<{agents:AgentPresence[];candidates?:TaskCompatibility['candidates']}>();
const labels:Record<string,string>={git_diff:'冻结 Git 提交差异',sdk_operation:'SDK 独立操作',scan_input:'高级输入与知识文档',input:'模型与范围输入',budget:'费用预算',result:'完整结果',observation:'进度与事件',control_api:'控制接口',matrix:'配套组件矩阵',scanner_preflight:'Scanner 预检',task_contract_unsupported:'任务版本'};
function candidate(id:string){return props.candidates?.find(c=>c.agentId===id)}
function state(a:AgentPresence){if(!a.online)return '离线';if(a.runtime?.state==='blocked')return '预检被阻止';if(candidate(a.agentId)?.ready===false)return '能力不匹配';return '在线'}
function missing(id:string){return candidate(id)?.missing.map(k=>labels[k]??k).join('、')||'—'}
</script>
<template>
 <div class="execution-nodes">
  <p v-if="!agents.length" class="node-note" role="status">尚未收到执行节点上报。请确认执行服务已启动并连接到 Server。</p>
  <p v-if="agents.length" class="node-scroll-hint">窄屏可横向滚动查看合同与诊断。</p>
  <div v-if="agents.length" class="node-table" tabindex="0" role="region" aria-label="执行节点数据表，可横向滚动"><table aria-label="执行节点兼容状态"><thead><tr><th>执行节点</th><th>当前状态</th><th>合同能力</th><th v-if="candidates">缺少能力</th><th>最近上报</th></tr></thead><tbody>
   <tr v-for="a in agents" :key="a.agentId"><td><strong>{{a.agentId}}</strong><span>{{a.slots}} 个槽位</span></td><td><span>{{state(a)}}</span><p v-if="a.runtime?.diagnostic">{{a.runtime.diagnostic.message}}</p></td><td><span>输入 {{a.inputCapabilities?.assignmentVersions?.join('/')||'未声明'}} · 结果 {{a.resultCapabilities?.resultVersions?.join('/')||'未声明'}} · 观察 {{a.observationCapabilities?.heartbeatVersions?.join('/')||'未声明'}}</span><span v-if="a.runtime">控制接口 {{a.runtime.controlAPI}} · Scanner 描述 {{a.runtime.scannerDescriptionVersion??'未获得'}}</span><span v-if="a.runtime">Agent {{a.runtime.agentBuild}} · Scanner {{a.runtime.scannerBuild??'未获得'}}</span><span v-else>组件版本未上报</span></td><td v-if="candidates">{{missing(a.agentId)}}</td><td>{{absoluteTime(a.lastSeen)}}</td></tr>
  </tbody></table></div>
  <details v-if="agents.length" class="node-raw"><summary>节点原始上报</summary><pre>{{JSON.stringify(agents,null,2)}}</pre></details>
 </div>
</template>
<style scoped>
.execution-nodes{min-width:0;max-width:100%;font-size:var(--font-size-xs);color:var(--text-secondary)}.node-note{line-height:var(--line-height-body);margin:var(--space-3) 0}.node-table{overflow-x:auto;max-width:100%;margin:var(--space-3) 0}table{width:100%;min-width:96ch;border-collapse:collapse;text-align:left}th,td{padding:var(--space-2) var(--space-3);border-bottom:1px solid var(--border-default);vertical-align:top;overflow-wrap:anywhere;min-width:12ch}th{font-weight:var(--font-weight-medium);background:var(--surface-secondary);white-space:nowrap}td span{display:block;margin-top:var(--space-1)}td p{max-width:48ch;line-height:var(--line-height-body);margin:var(--space-1) 0}strong{color:var(--text-primary);font-weight:var(--font-weight-medium)}summary{cursor:pointer;color:var(--text-secondary)}pre{max-height:var(--inspector-width-md);overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere;padding:var(--space-3);background:var(--surface-secondary);border:1px solid var(--border-default)}
.node-scroll-hint{display:none;margin:var(--space-2) 0;line-height:var(--line-height-body)}
@media(max-width:600px){.node-scroll-hint{display:block}}
</style>
