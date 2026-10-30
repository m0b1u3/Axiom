<script setup lang="ts">
import {computed,onMounted,ref,watch} from 'vue';
import {api} from '../api';import type {SDKIdentitySnapshot} from '../contracts';
import {checkedIdentities,usableIdentity,identitySelectionError} from '../utils/sdk-identities';
import Select from './ui/Select.vue';import Button from './ui/Button.vue';
const props=withDefaults(defineProps<{modelValue:string;native?:boolean;required?:boolean;disabled?:boolean}>(),{native:true,required:false,disabled:false});
const emit=defineEmits<{'update:modelValue':[string];validity:[boolean]}>();
const items=ref<SDKIdentitySnapshot[]>([]),loading=ref(true),error=ref('');
const options=computed(()=>[...(!props.required?[{value:'model-config',label:'使用模型配置的 API Key'}]:[]),...items.value.filter(usableIdentity).map(v=>({value:v.identity.id,label:`${v.identity.name} · ${v.identity.kind==='chatgpt'?'ChatGPT':'API Key'} · ${v.identity.agentId}`}))]);
const selected=computed(()=>items.value.find(v=>v.identity.id===props.modelValue));
const selectionError=computed(()=>identitySelectionError({id:props.modelValue,native:props.native,required:props.required,items:items.value,error:error.value}));
const valid=computed(()=>!(loading.value&&(props.required||!!props.modelValue))&&!selectionError.value);
watch(valid,value=>emit('validity',value),{immediate:true});
let pending:Promise<void>|undefined;
async function load():Promise<void>{
 if(pending)return pending;
 loading.value=true;error.value='';
 pending=(async()=>{try{items.value=checkedIdentities(await api.sdkIdentities())}catch(e){items.value=[];error.value=(e as Error).message}finally{loading.value=false;pending=undefined}})();
 return pending;
}
async function validate():Promise<boolean>{
 if(props.modelValue||props.required)await load();
 return valid.value;
}
defineExpose({validate});
onMounted(load);
</script>
<template>
 <section class="identity-picker" aria-label="扫描执行身份">
  <label>SDK 执行身份<Select aria-label="SDK 执行身份" :model-value="modelValue||(required?'':'model-config')" @update:model-value="emit('update:modelValue',$event==='model-config'?'':$event)" :options="options" :disabled="disabled||loading||!native" placeholder="选择已登录的授权账户" :aria-invalid="!loading&&!!selectionError"/></label>
  <p v-if="!native">独立 SDK 身份仅用于原生 OpenAI Responses 模型；其他供应商使用模型配置的凭据。</p>
  <p v-else-if="selected&&usableIdentity(selected)">绑定节点 {{selected.identity.agentId}} · 凭据版本 {{selected.identity.generation}} · {{selected.checkedAt}}。此扫描使用所选账户，模型访问权限尚未验证。</p>
  <p v-else-if="required">此模型未保存 API Key，必须选择已登录的授权账户。账户凭据保留在绑定节点。</p>
  <p v-else>可选择已认证的 API Key 或 ChatGPT 身份。SDK 身份与 Axiom 账户登录分开管理。</p>
  <p v-if="loading&&(modelValue||required)" role="status">正在读取授权账户状态…</p>
  <p v-else-if="selectionError" role="alert" class="request-error">{{selectionError}}</p>
  <p v-if="error" role="alert" class="request-error">{{error}}</p>
  <div class="identity-picker-actions">
   <Button v-if="native" variant="ghost" size="sm" :disabled="disabled||loading" @click="load">刷新账户列表</Button>
   <Button v-if="!native&&modelValue&&!required" variant="outline" size="sm" :disabled="disabled" @click="emit('update:modelValue','')">使用模型配置的 API Key</Button>
   <RouterLink :to="{path:'/models',query:{tab:'accounts',...(modelValue?{identity:modelValue}:{})}}">管理授权账户</RouterLink>
  </div>
 </section>
</template>
<style scoped>
.identity-picker{margin:var(--space-3) 0;min-width:0}.identity-picker label{display:block;font-size:var(--font-size-sm)}.identity-picker p,.identity-picker a{font-size:var(--font-size-xs);line-height:var(--line-height-body);overflow-wrap:anywhere}.identity-picker p{color:var(--text-secondary);margin:var(--space-2) 0}
.identity-picker .request-error{color:var(--state-danger)}.identity-picker-actions{display:flex;gap:var(--space-3);align-items:center;flex-wrap:wrap}
</style>
