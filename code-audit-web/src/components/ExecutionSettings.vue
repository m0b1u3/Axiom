<script setup lang="ts">
import {computed,onActivated,onMounted,ref} from 'vue';
import {api} from '../api';
import type {QueueSettings,QueueOverview} from '../types';
import Button from './ui/Button.vue';
import Input from './ui/Input.vue';
import QueueCapacity from './QueueCapacity.vue';
import {absoluteTime} from '../utils/time';
const settings=ref<QueueSettings|null>(null),overview=ref<QueueOverview|null>(null);
const global=ref(''),perModel=ref(''),perProject=ref('');
const canEdit=ref(false),loading=ref(false),saving=ref(false),error=ref(''),notice=ref('');
const fields=[{key:'global',label:'全局任务并发',help:'所有项目同时执行的扫描任务上限。'},{key:'model',label:'每个模型配置的并发',help:'共用同一模型配置的任务上限；不同配置分别计数。'},{key:'project',label:'每个项目的并发',help:'同一项目的任务上限；未分配项目的任务归为一组。'}];
const values=computed(()=>[global.value,perModel.value,perProject.value]);
const valid=computed(()=>values.value.every(v=>/^\d+$/.test(v)&&Number(v)>=1&&Number(v)<=64));
const dirty=computed(()=>settings.value&&[settings.value.global,settings.value.perModel,settings.value.perProject].some((v,i)=>v!==Number(values.value[i])));
function assign(v:QueueSettings){settings.value=v;global.value=String(v.global);perModel.value=String(v.perModel);perProject.value=String(v.perProject)}
async function load(){if(loading.value||saving.value)return;loading.value=true;error.value='';try{const [config,queue]=await Promise.all([api.executionSettings(),api.queueOverview()]);assign(config.settings);canEdit.value=config.canEdit;overview.value=queue}catch(e){error.value=(e as Error).message}finally{loading.value=false}}
async function save(){if(!valid.value||!settings.value||saving.value)return;saving.value=true;error.value='';notice.value='';try{const v=await api.saveExecutionSettings({...settings.value,global:Number(global.value),perModel:Number(perModel.value),perProject:Number(perProject.value)});assign(v);notice.value='已保存，后续任务领取立即使用新上限。运行中的任务继续执行。';overview.value=await api.queueOverview()}catch(e){error.value=(e as Error).message}finally{saving.value=false}}
onMounted(load);
onActivated(()=>{if(settings.value&&!dirty.value)void load()});
</script>
<template>
 <section class="execution-settings" aria-labelledby="execution-settings-heading">
  <div class="section-heading"><h2 id="execution-settings-heading">扫描任务调度</h2><Button variant="ghost" size="sm" :disabled="saving" :loading="loading" @click="load">重新加载</Button></div>
  <p class="muted">设置同时运行的扫描任务数量。单次深度扫描的 Worker 数在新建扫描中配置。</p>
  <div v-if="error" class="request-error" role="alert">{{error}}</div>
  <p v-if="loading && !settings" role="status">正在读取调度配置…</p>
  <template v-if="settings">
   <QueueCapacity v-if="overview" :overview="overview" hide-manage simple />
   <form @submit.prevent="save">
    <div class="limits-grid">
    <div v-for="(field,i) in fields" :key="field.key" class="limit-row">
     <div><label :for="`limit-${field.key}`">{{field.label}}</label><p :id="`help-${field.key}`">{{field.help}}</p></div>
     <Input v-if="i===0" id="limit-global" v-model="global" type="number" min="1" max="64" step="1" required :disabled="!canEdit||saving||loading" aria-describedby="help-global" />
     <Input v-else-if="i===1" id="limit-model" v-model="perModel" type="number" min="1" max="64" step="1" required :disabled="!canEdit||saving||loading" aria-describedby="help-model" />
     <Input v-else id="limit-project" v-model="perProject" type="number" min="1" max="64" step="1" required :disabled="!canEdit||saving||loading" aria-describedby="help-project" />
    </div>
    </div>
    <p class="muted">取值 1–64。实际并发同时受上述上限和在线执行机槽位限制。降低上限后，已有扫描结束释放槽位，才会继续领取新任务。</p>
    <p v-if="!canEdit" class="muted">仅系统管理员可修改调度配置。</p>
    <div class="save-row"><Button v-if="canEdit" type="submit" :disabled="!valid||!dirty||saving||loading" :loading="saving">保存并发配置</Button><span>最近更新 {{absoluteTime(settings.updatedAt)}}</span></div>
    <p v-if="notice" role="status" class="notice">{{notice}}</p>
   </form>
   <RouterLink class="diagnostics-link" to="/settings?section=diagnostics">查看高级诊断与配置变更记录</RouterLink>
  </template>
 </section>
</template>
<style scoped>
.execution-settings{padding:var(--space-1) 0}
.section-heading{display:flex;justify-content:space-between;align-items:center;gap:var(--space-3)}
.section-heading :deep(button){min-width:88px}
h2{margin:0;font-size:var(--font-size-md);font-weight:600}
.muted,.limit-row p,.save-row span{font-size:var(--font-size-xs);color:var(--text-secondary);line-height:var(--line-height-body)}
.muted{margin:var(--space-2) 0 var(--space-4);max-width:80ch}
.limits-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:var(--space-5);padding:var(--space-5) 0 var(--space-3)}
.limit-row{display:flex;flex-direction:column;gap:var(--space-3);min-width:0}
.limit-row>div{flex:1}label{font-size:var(--font-size-sm);font-weight:500}.limit-row p{margin:var(--space-1) 0 0;min-height:2.8em}
.limit-row :deep(input){width:100%;appearance:textfield;-moz-appearance:textfield}
.limit-row :deep(input::-webkit-inner-spin-button),.limit-row :deep(input::-webkit-outer-spin-button){-webkit-appearance:none;margin:0}
.save-row{display:flex;align-items:center;gap:var(--space-3);flex-wrap:wrap;margin:var(--space-5) 0}
.notice{font-size:var(--font-size-xs);color:var(--state-success)}
.diagnostics-link{display:inline-block;margin-top:var(--space-4);font-size:var(--font-size-sm)}
@media(max-width:700px){.limits-grid{grid-template-columns:1fr;gap:var(--space-4)}.limit-row{display:grid;grid-template-columns:minmax(0,1fr) 88px;align-items:center}.limit-row p{min-height:0}.save-row{align-items:flex-start;flex-direction:column}}
</style>
