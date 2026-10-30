<script setup lang="ts">
import {computed,onMounted,ref} from 'vue';
import {useRouter} from 'vue-router';
import {api,type ModelConfig} from '../api';
import type {SDKIdentitySnapshot} from '../contracts';
import {useAuth} from '../composables/auth';
import {usableIdentity} from '../utils/sdk-identities';
import Button from './ui/Button.vue';
import Select from './ui/Select.vue';

const props=defineProps<{identity:SDKIdentitySnapshot;disabled?:boolean}>();
const {canAdmin,canOperate}=useAuth();
const router=useRouter();
const models=ref<ModelConfig[]>([]),modelId=ref(''),loading=ref(true),error=ref('');
const options=computed(()=>models.value.filter(model=>model.supportsSdkIdentity===true).map(model=>({value:String(model.id),label:`${model.name} · ${model.model}`})));
const ready=computed(()=>canOperate.value&&usableIdentity(props.identity)&&!props.disabled&&!loading.value&&!error.value);
async function load(){
 loading.value=true;error.value='';
 try{
  models.value=await api.listModelConfigs();
  if(!modelId.value&&options.value.length===1)modelId.value=options.value[0]!.value;
 }catch(e){error.value='扫描模型读取失败：'+(e as Error).message}finally{loading.value=false}
}
function configureScan(){
 if(!ready.value||!options.value.some(model=>model.value===modelId.value))return;
 void router.push({name:'new-scan',query:{modelConfigId:modelId.value,sdkIdentityId:props.identity.identity.id}});
}
onMounted(load);
</script>
<template>
 <section class="account-scan" aria-label="使用授权账户扫描">
  <h4>使用此账户扫描</h4>
  <p>选择模型后进入新建扫描，再选择源码。模型访问权限以实际执行结果为准。</p>
  <p v-if="loading" role="status">正在读取扫描模型…</p>
  <p v-if="error" role="alert">{{error}}<Button variant="ghost" size="sm" :disabled="loading" @click="load">重新读取模型</Button></p>
  <template v-if="options.length">
   <label>扫描模型<Select v-model="modelId" :options="options" aria-label="授权账户扫描模型" placeholder="选择已配置的 OpenAI 模型" :disabled="!ready"/></label>
   <Button :disabled="!ready||!options.some(model=>model.value===modelId)" @click="configureScan">使用此账户新建扫描</Button>
  </template>
  <p v-else-if="!loading&&!error">还没有可搭配此账户的 OpenAI 模型配置。</p>
  <RouterLink v-if="canAdmin&&ready" :to="{path:'/models',query:{tab:'models',setup:'authorized',identity:identity.identity.id}}">添加授权账户模型</RouterLink>
  <p v-else-if="!canAdmin&&!loading&&!options.length">请联系管理员添加 OpenAI 模型配置。</p>
 </section>
</template>
<style scoped>
.account-scan{border-block:1px solid var(--border-default);padding:var(--space-3) 0;margin:var(--space-3) 0;display:flex;flex-direction:column;align-items:start;gap:var(--space-2);min-width:0}.account-scan h4{font-size:var(--font-size-sm);margin:0}.account-scan p,.account-scan a{font-size:var(--font-size-xs);line-height:var(--line-height-body);margin:0;overflow-wrap:anywhere}.account-scan p{color:var(--text-secondary)}.account-scan [role=alert]{color:var(--state-danger)}.account-scan label{display:block;width:100%;min-width:0;font-size:var(--font-size-sm)}
</style>
