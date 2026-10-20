<script setup lang="ts">
import {computed} from 'vue';
import type { QueueOverview } from '../types';
const props=defineProps<{overview:QueueOverview;hideManage?:boolean;simple?:boolean}>();
const online=computed(()=>props.overview.agents.filter(a=>a.online));
const ready=computed(()=>props.overview.readyAgents??online.value.filter(a=>a.runtime?.state!=='blocked').length);
const oldAgents=computed(()=>online.value.some(a=>a.dispatchMode!=='unbounded'));
</script>
<template>
 <section class="queue-capacity" aria-label="扫描执行状态">
  <div><span>执行中</span><strong>{{overview.active}}</strong><span>项执行</span></div>
  <div><span>排队</span><strong>{{overview.pending}}</strong><span>项任务</span></div>
  <div><span>执行机</span><strong>{{online.length}}</strong><span>台在线</span></div>
  <RouterLink v-if="!hideManage" to="/settings?section=execution">查看执行状态</RouterLink>
  <p v-if="overview.dispatchMode==='unbounded'" class="policy">平台不设置全局、模型或项目并发配额。</p>
  <p v-else role="alert">当前 Server 未声明无配额调度能力，请同步更新配套组件后确认执行方式。</p>
  <p v-if="!online.length" role="status">当前没有在线执行机。排队任务将在执行服务恢复后继续领取。</p>
  <p v-else-if="ready<online.length" role="status">部分执行节点尚未就绪，可在高级诊断中查看原因。</p>
  <p v-if="oldAgents" role="status">部分执行服务仍按旧版固定并发方式运行，请更新配套执行服务。</p>
 </section>
</template>
<style scoped>
.queue-capacity{display:flex;align-items:center;gap:var(--space-6);flex-wrap:wrap;border-block:1px solid var(--border-default);padding:var(--space-4) 0;margin-bottom:var(--space-5);font-size:var(--font-size-xs);color:var(--text-secondary)}
.queue-capacity>div{display:flex;align-items:baseline;gap:var(--space-2)}strong{font-size:var(--font-size-md);font-weight:var(--font-weight-semibold);color:var(--text-primary);font-variant-numeric:tabular-nums}a{margin-left:auto;color:var(--accent-primary)}p{flex-basis:100%;margin:0;line-height:var(--line-height-body);color:var(--state-warning)}.policy{color:var(--text-secondary)}
@media(max-width:600px){.queue-capacity{gap:var(--space-3)}a{margin-left:0}}
</style>
