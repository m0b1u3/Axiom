<script setup lang="ts">
import {computed,onMounted,ref,watch} from 'vue';
import {api} from '../api';import type {SDKIdentitySnapshot} from '../contracts';
import {checkedIdentities,usableIdentity} from '../utils/sdk-identities';
import Select from './ui/Select.vue';import Button from './ui/Button.vue';
const props=withDefaults(defineProps<{modelValue:string;native?:boolean;disabled?:boolean}>(),{native:true,disabled:false});const emit=defineEmits<{'update:modelValue':[string]}>();
const items=ref<SDKIdentitySnapshot[]>([]),loading=ref(false),error=ref('');
const options=computed(()=>[{value:'model-config',label:'使用模型配置的 API Key'},...items.value.filter(usableIdentity).map(v=>({value:v.identity.id,label:`${v.identity.name} · ${v.identity.kind==='chatgpt'?'ChatGPT':'API Key'} · ${v.identity.agentId}`}))]);
const selected=computed(()=>items.value.find(v=>v.identity.id===props.modelValue));
async function load(){loading.value=true;error.value='';try{items.value=checkedIdentities(await api.sdkIdentities())}catch(e){error.value=(e as Error).message}finally{loading.value=false}}
watch(()=>props.native,value=>{if(!value)emit('update:modelValue','')});onMounted(load);
</script>
<template>
 <section class="identity-picker" aria-label="扫描执行身份">
  <label>SDK 执行身份<Select aria-label="SDK 执行身份" :model-value="modelValue||'model-config'" @update:model-value="emit('update:modelValue',$event==='model-config'?'':$event)" :options="options" :disabled="disabled||loading||!native"/></label>
  <p v-if="!native">独立 SDK 身份仅用于原生 OpenAI Responses 模型；其他供应商继续使用模型配置的凭据。</p>
  <p v-else-if="selected">绑定节点 {{selected.identity.agentId}} · 凭据版本 {{selected.identity.generation}} · {{selected.checkedAt}}。节点不可用、状态未知或身份撤销时不会改用其他凭据。</p>
  <p v-else>可选择已认证的 API Key 或 ChatGPT 身份。SDK 身份与 Axiom 账户登录分开管理。</p>
  <p v-if="modelValue&&(!selected||!usableIdentity(selected))" role="alert">选定身份不可用，请刷新并核对登录状态。</p>
  <p v-if="error" role="alert" class="request-error">{{error}} <Button variant="ghost" size="sm" @click="load">重读执行身份</Button></p>
  <RouterLink to="/models?tab=accounts">管理授权账户</RouterLink>
 </section>
</template>
<style scoped>
.identity-picker{margin:var(--space-3) 0;min-width:0}.identity-picker label{display:block;font-size:var(--font-size-sm)}.identity-picker p,.identity-picker a{font-size:var(--font-size-xs);line-height:var(--line-height-body);overflow-wrap:anywhere}.identity-picker p{color:var(--text-secondary);margin:var(--space-2) 0}
</style>
