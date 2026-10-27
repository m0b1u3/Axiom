<script setup lang="ts">
import {computed} from 'vue';
import {absoluteTime} from '../utils/time';
import {tokenEstimateView,pricingMoney,pricingReasons} from '../utils/model-pricing';
const props=defineProps<{value:unknown;taskId:number;attempt:number}>();
const estimate=computed(()=>tokenEstimateView(props.value,props.taskId,props.attempt));
const price=computed(()=>estimate.value?.pricingSnapshot.pricing);
const modes={input_only:'全部输入同价',cache_read:'缓存读取单独计价',cache_read_write:'缓存读写分别计价'};
</script>
<template>
 <details v-if="estimate" class="token-cost">
  <summary>第 {{attempt}} 次 · 按用量估算：{{pricingMoney(estimate.amountMicros,estimate.currency)}}<span v-if="estimate.status==='partial'"> · 已知下界，完整费用未知</span><span v-else-if="estimate.status==='estimated'"> · 估算</span></summary>
  <p v-if="estimate.reason">{{pricingReasons[estimate.reason]??estimate.reason}}</p>
  <p v-if="estimate.currency&&estimate.currency!=='USD'">按 {{estimate.currency}} 单价估算，未换算为 USD；USD 预算记账单独保留。</p>
  <dl>
   <div><dt>单价快照</dt><dd>{{estimate.pricingSnapshot.provider}} · {{estimate.pricingSnapshot.model}} · 版本 {{estimate.pricingSnapshot.revision}} · {{absoluteTime(estimate.pricingSnapshot.capturedAt)}}</dd></div>
   <template v-if="price">
    <div><dt>计价规则</dt><dd>{{price.currency}} / 百万 Token · {{modes[price.cacheMode]}}</dd></div>
    <div><dt>输入 / 输出单价</dt><dd>{{price.inputPerMillion??'未知'}} / {{price.outputPerMillion??'未知'}}</dd></div>
    <div v-if="price.cacheMode!=='input_only'"><dt>缓存读取 / 写入单价</dt><dd>{{price.cacheReadPerMillion??'未知'}} / {{price.cacheMode==='cache_read_write'?(price.cacheWritePerMillion??'未知'):'按普通输入'}}</dd></div>
    <div v-if="price.note"><dt>价格依据</dt><dd>{{price.note}}</dd></div>
   </template>
   <div v-if="estimate.usage"><dt>实际 Token</dt><dd>输入 {{estimate.usage.inputTokens??'未知'}} · 输出 {{estimate.usage.outputTokens??'未知'}} · 缓存读取 {{estimate.usage.cachedInputTokens??'未知'}} · 缓存写入 {{estimate.usage.cacheWriteInputTokens??'未知'}}</dd></div>
   <div v-if="estimate.usage"><dt>用量来源</dt><dd>{{estimate.usage.source==='sdk_on_usage'?'SDK 累计用量回调':'Responses 请求回执'}} · {{estimate.usage.complete?'完整用量已归档':'部分观察'}}<template v-if="estimate.usage.requestCount!==null"> · {{estimate.usage.requestCount}} 次请求</template></dd></div>
  </dl>
  <p>按本次冻结单价与已上报用量计算；推理 Token 不重复计入输出费用。完整价格、用量和计算结果可在下方原始费用快照中查看。</p>
 </details>
 <p v-else class="token-cost-invalid" role="alert">用量估算的版本或执行身份不匹配，金额未知；原始费用快照仍可查看。</p>
</template>
<style scoped>
.token-cost{border-top:1px solid var(--border-subtle);padding-block:var(--space-2);font-size:var(--font-size-xs)}summary{cursor:pointer;color:var(--text-primary);font-variant-numeric:tabular-nums;overflow-wrap:anywhere}summary:focus-visible{outline:2px solid var(--border-focus);outline-offset:var(--space-1)}dl{margin-block:var(--space-2)}dl>div{display:grid;grid-template-columns:140px minmax(0,1fr);gap:var(--space-2);padding-block:var(--space-1)}dt,p{color:var(--text-secondary)}dd{margin:0;overflow-wrap:anywhere}p{line-height:var(--line-height-body);margin-block:var(--space-2);overflow-wrap:anywhere}.token-cost-invalid{font-size:var(--font-size-xs);color:var(--state-danger)}@media(max-width:600px){dl>div{grid-template-columns:minmax(0,1fr);gap:var(--space-1)}}
</style>
