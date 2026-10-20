<script setup lang="ts">
import {onActivated,onMounted,ref} from 'vue';
import {api} from '../api';
import type {QueueOverview} from '../types';
import Button from './ui/Button.vue';
import QueueCapacity from './QueueCapacity.vue';
const overview=ref<QueueOverview|null>(null),loading=ref(false),error=ref('');
async function load(){if(loading.value)return;loading.value=true;error.value='';try{overview.value=await api.queueOverview()}catch(e){error.value='扫描执行状态读取失败：'+(e as Error).message}finally{loading.value=false}}
onMounted(load);
onActivated(()=>{if(overview.value)void load()});
</script>
<template>
 <section class="execution-settings" aria-labelledby="execution-settings-heading">
  <div class="section-heading"><h2 id="execution-settings-heading">扫描执行</h2><Button variant="ghost" size="sm" :loading="loading" @click="load">重新加载</Button></div>
  <p class="muted">查看任务排队与执行状态。单次扫描的 Worker、预算和超时在新建扫描中配置。</p>
  <p v-if="error" role="alert" class="request-error">{{error}}</p>
  <p v-if="error&&overview" role="status" class="muted">以下保留上次成功的执行快照，请重新加载后确认当前状态。</p>
  <p v-if="loading&&!overview" role="status">正在读取扫描执行状态…</p>
  <QueueCapacity v-if="overview" :overview="overview" hide-manage simple />
  <RouterLink class="diagnostics-link" to="/settings?section=diagnostics">查看执行诊断与历史记录</RouterLink>
 </section>
</template>
<style scoped>
.execution-settings{padding:var(--space-1) 0}
.section-heading{display:flex;justify-content:space-between;align-items:center;gap:var(--space-3)}
h2{margin:0;font-size:var(--font-size-md);font-weight:var(--font-weight-semibold)}
.muted,.diagnostics-link{font-size:var(--font-size-xs);line-height:var(--line-height-body)}
.muted{margin:var(--space-2) 0 var(--space-4);color:var(--text-secondary);max-width:80ch}
.diagnostics-link{display:inline-block;color:var(--accent-primary)}
</style>
