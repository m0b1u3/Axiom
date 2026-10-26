<script setup lang="ts">
import {computed} from 'vue';
import type {ScanResult} from '../contracts';
import {sdkOperationView,dispositionLabels} from '../utils/sdk-operations';
const props=defineProps<{result:ScanResult}>();
const operation=computed(()=>sdkOperationView(props.result.extensions?.sdkOperation,props.result.execution));
</script>
<template>
 <section aria-label="SDK 独立操作结果" class="sdk-operation-result">
  <p v-if="!operation" role="alert">SDK 操作结果版本、身份或费用语义不受支持；完整归档仍可下载。</p>
  <template v-else>
   <h3>{{operation.kind==='preflight'?'SDK 原生预检':'SDK 独立漏洞复核'}}</h3>
   <dl><dt>执行 / SDK</dt><dd>{{operation.execution.taskId}} / {{operation.execution.attempt}} · {{operation.sdkVersion}}</dd><dt>操作费用 · USD</dt><dd>{{operation.cost.state==='unknown'?'未知':'0'}}</dd></dl>
   <p>{{operation.cost.reason}}</p>
   <p v-if="operation.error" role="alert" class="request-error">{{operation.error.code}}：{{operation.error.message}}</p>
   <template v-if="operation.preflight">
    <p>预检已解析本地配置。认证尚未验证（verified: false），不能据此确认模型可访问或账户可用。</p>
    <dl><dt>目标 / 模式</dt><dd>{{JSON.stringify(operation.preflight.target)}} / {{operation.preflight.mode}}</dd><dt>模型 / 供应商</dt><dd>{{operation.preflight.model}} / {{operation.preflight.modelProvider??'SDK 默认'}}</dd><dt>认证来源</dt><dd>{{JSON.stringify(operation.preflight.authentication)}}</dd><dt>输出计划</dt><dd>{{operation.preflight.outputDir??'SDK 自动分配'}}</dd><dt>知识文档</dt><dd>{{JSON.stringify(operation.preflight.knowledgeBasePaths??[])}}</dd></dl>
   </template>
   <template v-if="operation.validation"><dl><dt>SDK 原生结论</dt><dd>{{dispositionLabels[operation.validation.disposition]}} · {{operation.validation.disposition}}</dd><dt>原生 Thread ID</dt><dd>{{operation.validation.threadId??'未提供'}}</dd></dl><p>{{operation.error?'本次复核未成功；原始响应和 SDK 标签保留供诊断，不能作为有效复核结论。':'这是 SDK 原生复核结论，人工漏洞处理状态保持独立。'}} 复核边界是冻结的完整源码快照。</p><pre aria-label="SDK 复核报告原文">{{operation.validation.report}}</pre></template>
   <details><summary>查看原始操作结果</summary><pre>{{JSON.stringify(operation,null,2)}}</pre></details>
  </template>
 </section>
</template>
<style scoped>
.sdk-operation-result{border-top:1px solid var(--border-default);margin-top:var(--space-3);padding-top:var(--space-3);font-size:var(--font-size-xs);min-width:0}h3{font-size:var(--font-size-sm);font-weight:var(--font-weight-semibold);margin:0 0 var(--space-2)}dl{display:grid;grid-template-columns:max-content minmax(0,1fr);gap:var(--space-2) var(--space-3)}dt,p{color:var(--text-secondary)}dd{margin:0;overflow-wrap:anywhere}p{line-height:var(--line-height-body);margin:var(--space-2) 0}summary{cursor:pointer}pre{max-height:calc(var(--height-table-row)*10);overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere;background:var(--surface-secondary);padding:var(--space-2);font:var(--font-size-xs)/var(--line-height-body) var(--font-mono)}@media(max-width:650px){dl{grid-template-columns:1fr;gap:var(--space-1)}}
</style>
