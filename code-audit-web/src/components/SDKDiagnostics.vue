<script setup lang="ts">
import {computed} from 'vue';
import type {ExecutionRef} from '../contracts';
import {sdkDiagnosticsView,diagnosticLabel} from '../utils/sdk-native';
import {absoluteTime} from '../utils/time';
const props=defineProps<{value:unknown;execution:ExecutionRef}>();
const view=computed(()=>sdkDiagnosticsView(props.value,props.execution));
const entries=computed(()=>[...(view.value.value?.entries??[])].reverse());
</script>
<template>
 <section v-if="view.value||view.invalid" class="sdk-diagnostics" aria-label="SDK 结构化诊断">
  <p v-if="view.invalid" role="alert">SDK 诊断快照版本或身份不受支持；其他进度与原始数据仍可查看。</p>
  <template v-else-if="view.value">
   <h3>SDK 结构化诊断</h3><p>警告 {{view.value.warningCount}} · 观察器错误 {{view.value.observerErrorCount}}<template v-if="view.value.omittedCount"> · 仅显示最近 {{view.value.entries.length}} 条，另 {{view.value.omittedCount}} 条见完整诊断日志</template></p>
   <p v-if="view.value.observerErrorCount" class="diagnostic-note">观察器错误可能影响进度或日志的完整性，扫描结论以归档结果为准。</p>
   <p v-if="!view.value.entries.length">本次没有记录 SDK 警告或观察器错误。</p>
   <ol v-else tabindex="0" aria-label="最近 SDK 诊断，可滚动展开"><li v-for="entry in entries" :key="entry.sequence"><details><summary><time :datetime="entry.occurredAt">{{absoluteTime(entry.occurredAt)}}</time><strong>{{diagnosticLabel(entry)}}</strong><span>{{entry.message}}</span></summary><dl><dt>来源 / 类型</dt><dd>{{entry.source}} / {{entry.kind}}</dd><template v-if="entry.observer"><dt>观察器</dt><dd>{{entry.observer}}</dd></template><template v-if="entry.error"><dt>错误</dt><dd>{{entry.error.name}} · {{entry.error.code??'无代码'}} · {{entry.error.message}}</dd></template><dt>执行 / 序号</dt><dd>{{entry.execution.taskId}} / {{entry.execution.attempt}} · #{{entry.sequence}}</dd></dl><p v-if="entry.truncated">文本已在源头脱敏或截断。</p><details><summary>原始诊断记录</summary><pre>{{JSON.stringify(entry,null,2)}}</pre></details></details></li></ol>
  </template>
 </section>
</template>
<style scoped>
.sdk-diagnostics{font-size:var(--font-size-xs);padding:var(--space-3) 0;border-top:1px solid var(--border-subtle)}h3{font-size:var(--font-size-sm);margin:0 0 var(--space-2);font-weight:var(--font-weight-medium)}p{color:var(--text-secondary);margin:var(--space-2) 0}.diagnostic-note{color:var(--state-warning)}ol{list-style:none;padding:0;margin:0;max-height:calc(var(--height-table-row)*8);overflow:auto;overscroll-behavior:contain}li{border-top:1px solid var(--border-subtle);padding:var(--space-2) 0}summary{cursor:pointer;overflow-wrap:anywhere;line-height:var(--line-height-body)}summary time{color:var(--text-tertiary);margin-right:var(--space-3)}summary strong{font-weight:var(--font-weight-medium);margin-right:var(--space-2)}summary span{color:var(--text-secondary)}dl{display:grid;grid-template-columns:max-content minmax(0,1fr);gap:var(--space-2);margin:var(--space-3) 0}dt{color:var(--text-tertiary)}dd{margin:0;overflow-wrap:anywhere}pre{font:var(--font-size-xs)/var(--line-height-body) var(--font-mono);max-height:calc(var(--height-table-row)*7);overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere;background:var(--surface-secondary);padding:var(--space-2)}
</style>
