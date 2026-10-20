<script setup lang="ts">
import {ref,watch} from 'vue';
import {api} from '../api';
import {useAuth} from '../composables/auth';
import Button from './ui/Button.vue';
const props=defineProps<{scanId:number;attempt:number;findingId:number}>();
const emit=defineEmits<{created:[]}>();const {canOperate}=useAuth();
const acknowledged=ref(false),busy=ref(false),error=ref(''),created=ref<number>();let generation=0;
watch(()=>[props.scanId,props.findingId,props.attempt],()=>{generation++;acknowledged.value=false;busy.value=false;error.value='';created.value=undefined;});
async function validate(){if(!acknowledged.value||busy.value||!canOperate.value)return;const gen=generation;busy.value=true;error.value='';try{const task=await api.createSDKOperation(props.scanId,{kind:'validate',findingId:String(props.findingId),originAttempt:props.attempt,acknowledgeUnknownCost:true});if(gen!==generation)return;created.value=task.id;emit('created');}catch(e){if(gen===generation)error.value=(e as Error).message;}finally{if(gen===generation)busy.value=false;}}
</script>
<template>
 <section class="sdk-validation-action" aria-label="独立漏洞复核">
  <h3>独立漏洞复核</h3><p>冻结当前漏洞证据、源码和模型配置。SDK 独立复核只读完整源码快照，不执行应用、测试或 PoC；本入口不传扫描范围、知识文档、扫描提示词或费用上限。</p>
  <p>费用未知。此操作不纳入来源扫描的累计预算，也不会自动修改人工漏洞状态。</p>
  <template v-if="canOperate"><label><input v-model="acknowledged" type="checkbox" :disabled="busy"/>我已知悉本次复核费用未知且无法设置费用上限</label><Button size="sm" variant="outline" :loading="busy" :disabled="!acknowledged||busy" @click="validate">发起 SDK 独立复核</Button></template><p v-else>当前账户为只读用户，无法发起复核。</p>
  <p v-if="error" role="alert" class="request-error">复核操作创建失败：{{error}}</p><p v-if="created" role="status">已创建独立操作 <RouterLink :to="'/scans/'+created">#{{created}} · 查看复核执行</RouterLink>。当前证据与人工状态已保留。</p>
 </section>
</template>
<style scoped>
.sdk-validation-action{border-block:1px solid var(--border-default);padding:var(--space-3) 0;margin:var(--space-3) 0;font-size:var(--font-size-xs);min-width:0}h3{font-size:var(--font-size-sm);font-weight:var(--font-weight-medium);margin:0}p{margin:var(--space-2) 0;color:var(--text-secondary);line-height:var(--line-height-body);overflow-wrap:anywhere}label{display:flex;gap:var(--space-2);align-items:flex-start;margin:var(--space-2) 0;line-height:var(--line-height-body)}input{accent-color:var(--accent-primary);margin-top:var(--space-1)}a{color:var(--accent-primary)}
</style>
