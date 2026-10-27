<script setup lang="ts">
import {computed} from 'vue';
import type {ModelPricing} from '../contracts';
import {emptyModelPricing} from '../utils/model-pricing';
import Input from './ui/Input.vue';
import Select from './ui/Select.vue';
const props=defineProps<{disabled?:boolean;revision?:number}>();
const model=defineModel<ModelPricing|null>({default:null});
const modes=[{value:'input_only',label:'全部输入按同一单价'},{value:'cache_read',label:'缓存读取单独计价，写入按普通输入'},{value:'cache_read_write',label:'缓存读取、写入分别计价'}];
const enabled=computed({get:()=>model.value!==null,set:value=>{model.value=value?emptyModelPricing():null}});
const currency=computed({get:()=>model.value?.currency??'USD',set:value=>{if(model.value)model.value={...model.value,currency:value.trim().toUpperCase()}}});
const mode=computed({get:()=>model.value?.cacheMode??'cache_read',set:value=>{if(model.value)model.value={...model.value,cacheMode:value as ModelPricing['cacheMode']}}});
const rate=(key:'inputPerMillion'|'outputPerMillion'|'cacheReadPerMillion'|'cacheWritePerMillion')=>computed({get:()=>model.value?.[key]??'',set:value=>{if(model.value)model.value={...model.value,[key]:value.trim()||null}}});
const input=rate('inputPerMillion'),output=rate('outputPerMillion'),cacheRead=rate('cacheReadPerMillion'),cacheWrite=rate('cacheWritePerMillion');
const note=computed({get:()=>model.value?.note??'',set:value=>{if(model.value)model.value={...model.value,note:value}}});
</script>
<template>
 <fieldset class="pricing-fields" :disabled="props.disabled">
  <legend>用量计价 <span v-if="revision">单价版本 {{revision}}</span></legend>
  <label class="pricing-toggle"><input v-model="enabled" type="checkbox"/>按配置单价自动估算费用</label>
  <p>执行开始时保存单价快照；改价只影响之后开始的执行。估算不代表供应商账单。</p>
  <template v-if="enabled">
   <div class="pricing-grid">
    <label for="pricing-currency"><span>计价币种</span><Input id="pricing-currency" v-model="currency" maxlength="3" placeholder="USD / CNY" autocomplete="off" /></label>
    <label for="pricing-cache-mode"><span>缓存计价规则</span><Select id="pricing-cache-mode" v-model="mode" :options="modes" aria-label="缓存计价规则" /></label>
    <label for="pricing-input"><span>输入单价 / 百万 Token</span><Input id="pricing-input" v-model="input" inputmode="decimal" placeholder="未配置" autocomplete="off" /></label>
    <label for="pricing-output"><span>输出单价 / 百万 Token</span><Input id="pricing-output" v-model="output" inputmode="decimal" placeholder="未配置" autocomplete="off" /></label>
    <label v-if="mode!=='input_only'" for="pricing-cache-read"><span>缓存读取单价 / 百万 Token</span><Input id="pricing-cache-read" v-model="cacheRead" inputmode="decimal" placeholder="未配置" autocomplete="off" /></label>
    <label v-if="mode==='cache_read_write'" for="pricing-cache-write"><span>缓存写入单价 / 百万 Token</span><Input id="pricing-cache-write" v-model="cacheWrite" inputmode="decimal" placeholder="未配置" autocomplete="off" /></label>
    <label class="pricing-basis" for="pricing-note"><span>价格依据（可选）</span><Input id="pricing-note" v-model="note" maxlength="512" placeholder="例如合同价、折扣或适用时段；请勿填写凭据。" /></label>
   </div>
   <p>单价最多六位小数；填 0 表示该项免费，留空表示未知。缺少所需单价或用量时不计算金额，不自动换算币种。</p>
  </template>
 </fieldset>
</template>
<style scoped>
.pricing-fields{min-width:0;border:0;border-top:1px solid var(--border-default);padding:var(--space-3) 0 0;margin:0}.pricing-fields legend{font-size:var(--font-size-sm);font-weight:var(--font-weight-medium);padding-right:var(--space-2)}legend span,p{font-size:var(--font-size-xs);font-weight:var(--font-weight-normal);color:var(--text-secondary)}p{line-height:var(--line-height-body);margin:var(--space-2) 0;overflow-wrap:anywhere}.pricing-toggle{display:flex;align-items:center;gap:var(--space-2);font-size:var(--font-size-sm)}.pricing-grid{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:var(--space-3);margin-top:var(--space-3)}.pricing-grid label{min-width:0;display:flex;flex-direction:column;gap:var(--space-1);font-size:var(--font-size-xs)}.pricing-basis{grid-column:1/-1}@media(max-width:600px){.pricing-grid{grid-template-columns:minmax(0,1fr)}}
</style>
