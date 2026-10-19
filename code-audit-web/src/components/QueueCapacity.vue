<script setup lang="ts">
import type { QueueOverview } from '../types';
defineProps<{overview:QueueOverview; hideManage?:boolean;simple?:boolean}>();
</script>
<template>
 <section class="queue-capacity" aria-label="执行容量">
  <div><span>执行中</span><strong>{{overview.active}} / {{overview.effectiveCapacity}}</strong><span>有效槽位</span></div>
  <div><span>排队</span><strong>{{overview.pending}}</strong><span>项任务</span></div>
  <div><span>执行机</span><strong>{{overview.agents.filter(a=>a.online).length}}</strong><span>台在线 · {{overview.onlineSlots}} 个槽位</span></div>
  <RouterLink v-if="!hideManage" to="/settings?section=execution">管理并发</RouterLink>
  <p v-if="overview.onlineSlots===0" role="status">当前没有在线执行机。排队任务将在执行机恢复后继续调度。</p>
  <p v-else-if="overview.readySlots!==undefined && overview.readySlots<overview.onlineSlots"><template v-if="simple">部分执行位置暂不可用，当前可用 {{overview.readySlots}} 个槽位。可在高级诊断中查看原因。</template><template v-else>部分在线节点预检被阻止或组件不匹配，当前可用 {{overview.readySlots}} 个槽位；任务还需匹配对应合同能力。</template></p>
  <p v-else-if="overview.settings.global>overview.onlineSlots">全局上限 {{overview.settings.global}}，当前执行机提供 {{overview.onlineSlots}} 个槽位；增加执行机槽位后才能达到配置上限。</p>
 </section>
</template>
<style scoped>
.queue-capacity{display:flex;align-items:center;gap:var(--space-6);flex-wrap:wrap;border-block:1px solid var(--border-default);padding:var(--space-4) 0;margin-bottom:var(--space-5);font-size:var(--font-size-xs);color:var(--text-secondary)}
.queue-capacity>div{display:flex;align-items:baseline;gap:var(--space-2)}strong{font-size:var(--font-size-md);font-weight:600;color:var(--text-primary);font-variant-numeric:tabular-nums}a{margin-left:auto;color:var(--accent-primary)}p{flex-basis:100%;margin:0;line-height:var(--line-height-body);color:var(--state-warning)}
@media(max-width:600px){.queue-capacity{gap:var(--space-3)}a{margin-left:0}}
</style>
