<script setup lang="ts">
import SDKIdentityPicker from './SDKIdentityPicker.vue';
import {computed,ref} from 'vue';
import {useRouter} from 'vue-router';
import {api,type ModelConfig} from '../api';
import type {ComponentPlanVersion} from '../contracts';
import {useAuth} from '../composables/auth';
import Button from './ui/Button.vue';import Input from './ui/Input.vue';import Select from './ui/Select.vue';
const props=defineProps<{version:ComponentPlanVersion;models:ModelConfig[]}>();
const {canOperate}=useAuth(),router=useRouter(),model=ref(''),workers=ref('2'),single=ref('10'),total=ref('50'),component=ref('2'),minutes=ref('60'),ack=ref(false),busy=ref(false),error=ref('');
const sdkIdentityId=ref('');
const selectedModel=computed(()=>props.models.find(v=>String(v.id)===model.value));
const prefix=computed(()=>`component-launch-${props.version.planId}-${props.version.version}`);
const planned=computed(()=>{const n=props.version.document.components.length,grant=Math.min(Number(single.value),Number(total.value)),each=Math.min(Number(component.value),Math.floor(grant*1e6/n)/1e6);return Number.isFinite(each)&&each>0?`${n} 个组件，每组件最多预留 $${each.toFixed(6)}；实际分配在领取时按剩余额度冻结。`:'填写正数预算后可查看预留方案。'});
async function launch(){if(!canOperate.value||busy.value||!ack.value)return;busy.value=true;error.value='';try{const result=await api.createComponentScan(props.version.planId,{...(sdkIdentityId.value?{sdkIdentityId:sdkIdentityId.value}:{}),version:props.version.version,versionSha256:props.version.sha256,modelConfigId:Number(model.value),workers:Number(workers.value),timeoutMinutes:Number(minutes.value),maxCostUsd:Number(single.value),maxTotalCostUsd:Number(total.value),componentCostUsd:Number(component.value),acknowledgeMatchingCost:ack.value});await router.push('/scans/'+result.id)}catch(e){error.value='组件审计创建失败：'+(e as Error).message}finally{busy.value=false}}
</script>
<template>
 <section class="component-launch" aria-label="从冻结计划启动组件审计">
  <h4>从 v{{version.version}} 启动组件审计</h4>
  <p>使用此冻结版本的完整源码和组件范围，执行 Standard 扫描。以后编辑草稿不会改变此任务。</p>
  <form v-if="canOperate" @submit.prevent="launch">
   <label :for="prefix+'-model'">审计模型<Select :id="prefix+'-model'" aria-label="组件审计模型" v-model="model" :options="models.map(m=>({value:String(m.id),label:m.name+' · '+m.model}))" placeholder="选择审计模型" :disabled="busy"/></label>
   <SDKIdentityPicker v-model="sdkIdentityId" :native="!!selectedModel&&selectedModel.provider==='openai'&&(!selectedModel.baseUrl||selectedModel.baseUrl==='https://api.openai.com/v1')&&selectedModel.wireApi!=='chat'" :disabled="busy"/>
   <div class="component-fields">
    <label :for="prefix+'-workers'">并发组件数<Input :id="prefix+'-workers'" v-model="workers" type="number" min="1" max="8" step="1" required :disabled="busy"/></label>
    <label :for="prefix+'-time'">本轮时限 · 分钟<Input :id="prefix+'-time'" v-model="minutes" type="number" min="1" max="600" required :disabled="busy"/></label>
    <label :for="prefix+'-component'">每组件上限 · USD<Input :id="prefix+'-component'" v-model="component" type="number" min="0.000001" step="0.000001" required :disabled="busy"/></label>
    <label :for="prefix+'-single'">单轮上限 · USD<Input :id="prefix+'-single'" v-model="single" type="number" min="0.000001" step="0.000001" required :disabled="busy"/></label>
    <label :for="prefix+'-total'">任务累计预算 · USD<Input :id="prefix+'-total'" v-model="total" type="number" min="0.000001" step="0.000001" required :disabled="busy"/></label>
   </div>
   <p>{{planned}}</p><p>费用为引擎估算。SDK 原生跨组件匹配没有费用回调或上限，可能产生额外未知费用；这些预算不能保证包含匹配费用。未知费用须先在 Web 核对才能重试。</p>
   <label class="component-ack"><input v-model="ack" type="checkbox" :disabled="busy"/>我知悉这会调用审计模型，且跨组件匹配费用未知、不受上述扫描预算限制</label>
   <p v-if="error" role="alert" class="request-error">{{error}}</p>
   <Button type="submit" :disabled="busy||!model||!ack" :loading="busy">启动 Standard 组件审计</Button>
  </form>
  <p v-else>当前账户可查看冻结计划；启动组件审计需要操作权限。</p>
 </section>
</template>
<style scoped>
.component-launch{border-top:1px solid var(--border-default);margin-top:var(--space-4);padding-top:var(--space-3);min-width:0}h4{font-size:var(--font-size-sm);font-weight:var(--font-weight-semibold);margin:0 0 var(--space-2)}p,label{font-size:var(--font-size-xs);line-height:var(--line-height-body)}p{color:var(--text-secondary);margin:var(--space-2) 0;overflow-wrap:anywhere}label{display:block;color:var(--text-primary)}label :deep(input),label :deep(button){margin-top:var(--space-1)}.component-fields{display:grid;grid-template-columns:repeat(auto-fit,minmax(140px,1fr));gap:var(--space-3);margin:var(--space-3) 0}.component-ack{display:flex;align-items:flex-start;gap:var(--space-2);margin:var(--space-3) 0}.component-ack input{accent-color:var(--accent-primary);flex:none}.component-ack input:focus-visible{outline:2px solid var(--border-focus);outline-offset:2px}
</style>
