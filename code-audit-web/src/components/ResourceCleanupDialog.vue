<script setup lang="ts">
import {ref,shallowRef,watch} from 'vue';
import {api} from '../api';
import type {CleanupJob} from '../contracts';
import Dialog from './ui/Dialog.vue';
import Button from './ui/Button.vue';
import CleanupPlan from './CleanupPlan.vue';
const props=defineProps<{open:boolean;kind:'scan'|'snapshot';resourceId:number}>();
const emit=defineEmits<{'update:open':[boolean];completed:[]}>();
const data=shallowRef<CleanupJob|null>(null),error=ref(''),loading=ref(false);let generation=0;
watch(()=>[props.open,props.kind,props.resourceId],async()=>{const n=++generation;if(!props.open)return;data.value=null;error.value='';loading.value=true;try{const plan=await api.previewCleanup(props.kind,props.resourceId);if(n===generation)data.value=plan;}catch(e){if(n===generation)error.value=(e as Error).message}finally{if(n===generation)loading.value=false}},{immediate:true});
function completed(){emit('update:open',false);emit('completed')}
</script>
<template><Dialog :open="open" title="清理与删除预览" width="760px" @update:open="emit('update:open',$event)"><p v-if="loading" role="status">正在核对任务引用、费用状态与文件内容…</p><p v-if="error" role="alert">无法生成删除计划：{{error}}。数据已保留。若已有未完成清理，可在系统设置的“存储与清理”继续。</p><CleanupPlan v-if="data" :job="data" @change="data=$event" @completed="completed" /><template #footer><Button variant="secondary" @click="emit('update:open',false)">关闭</Button></template></Dialog></template>
