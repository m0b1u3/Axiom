<script setup lang="ts">
import {nextTick,onMounted,ref} from 'vue';
import {useRoute} from 'vue-router';
import {api} from '../api';
import type {QueueOverview,QueueSettings} from '../types';
import type {StorageSnapshot} from '../contracts';
import {absoluteTime} from '../utils/time';
import ExecutionNodes from './ExecutionNodes.vue';
import SDKIdentityDiagnostics from './SDKIdentityDiagnostics.vue';
import DiagnosticSnapshot from './DiagnosticSnapshot.vue';
import Button from './ui/Button.vue';
const route=useRoute(),overview=ref<QueueOverview|null>(null),history=ref<QueueSettings[]>([]),storage=ref<StorageSnapshot|null>(null),loading=ref(false),nodeError=ref(''),historyError=ref(''),storageError=ref('');
async function load(){if(loading.value)return;loading.value=true;nodeError.value='';historyError.value='';storageError.value='';try{const [nodes,settings,capacity]=await Promise.allSettled([api.queueOverview(),api.executionSettings(),api.storage()]);if(nodes.status==='fulfilled')overview.value=nodes.value;else nodeError.value='执行节点诊断读取失败：'+nodes.reason.message;if(settings.status==='fulfilled')history.value=settings.value.history;else historyError.value='配置变更读取失败：'+settings.reason.message;if(capacity.status==='fulfilled')storage.value=capacity.value;else storageError.value='存储诊断不可用：'+capacity.reason.message}finally{loading.value=false}}
onMounted(async()=>{await load();if(route.hash){await nextTick();document.getElementById(route.hash.slice(1))?.scrollIntoView({block:'start'})}});
</script>

<template>
 <section class="advanced-diagnostics" aria-labelledby="advanced-diagnostics-title">
  <header><div><h2 id="advanced-diagnostics-title">高级诊断</h2><p>排查执行位置、组件兼容性与授权绑定。此页不会启动扫描或登录。</p></div><Button variant="outline" size="sm" :loading="loading" @click="load">刷新运行诊断</Button></header>
  <section class="diagnostic-section" aria-labelledby="structured-diagnostic-title"><h3 id="structured-diagnostic-title">结构化输出对照</h3><p class="muted">固定小样例与三种格式的请求预览、离线回放和脱敏记录。不会调用模型或启动审计。</p><RouterLink to="/settings/structured-output">打开结构化输出对照诊断</RouterLink></section>
  <section class="diagnostic-section" aria-labelledby="node-diagnostics-title">
   <div class="section-heading"><h3 id="node-diagnostics-title">执行节点与组件兼容性</h3><span v-if="overview" class="snapshot-time">Server 快照 · {{absoluteTime(overview.serverTime)}}</span></div>
   <p v-if="nodeError" role="alert">{{nodeError}}</p><p v-if="nodeError&&overview" role="status">以下保留上次成功的节点快照，请重新获取后确认当前状态。</p><p v-if="loading&&!overview" role="status">正在读取执行节点…</p>
   <ExecutionNodes v-if="overview" :agents="overview.agents"/>
   <p class="muted node-policy">节点每 10 秒上报，超过 30 秒未上报视为离线。兼容性以实际合同能力为准；预检被阻止的节点不能开始新的执行。</p>
   <DiagnosticSnapshot v-if="overview" title="调度原始快照" :value="overview" filename="dispatch-snapshot.json"/>
  </section>
  <section class="diagnostic-section"><SDKIdentityDiagnostics/></section>
  <section class="diagnostic-section" aria-labelledby="settings-history-title"><h3 id="settings-history-title">旧版并发配置记录</h3><p v-if="overview?.dispatchMode==='unbounded'" class="muted">这些记录仅供历史追溯；平台并发配额已停用，不再参与当前调度。</p><p v-else class="muted">当前 Server 尚未确认无配额调度，请同步更新配套组件；以下保留旧版配置记录。</p><p v-if="historyError" role="alert">{{historyError}}</p><p v-if="!loading&&!historyError&&!history.length" class="muted">尚无配置变更记录。</p><ol><li v-for="item in history" :key="item.revision"><strong>版本 {{item.revision}}</strong><span>全局 {{item.global}} · 模型 {{item.perModel}} · 项目 {{item.perProject}}</span><span>{{item.updatedBy}} · {{absoluteTime(item.updatedAt)}}</span></li></ol></section>
  <section id="storage-diagnostics" class="diagnostic-section" aria-labelledby="storage-diagnostics-title"><div class="section-heading"><h3 id="storage-diagnostics-title">存储诊断</h3><span v-if="storage" class="snapshot-time">采集时间 · {{absoluteTime(storage.observedAt)}}</span></div><p v-if="storageError" role="alert">{{storageError}}</p><p v-if="storageError&&storage" role="status">以下保留上次成功的存储快照，请重新获取后确认当前状态。</p><p v-if="loading&&!storage" role="status">正在读取存储诊断…</p><DiagnosticSnapshot v-if="storage" title="原始存储快照" :value="storage" filename="storage-snapshot.json"/><p class="muted">存储策略与清理操作在<RouterLink to="/settings?section=storage">存储与清理</RouterLink>管理。</p></section>
  <section class="diagnostic-section"><h3>运行架构</h3><dl><dt>部署组件</dt><dd>Server + Agent · PostgreSQL 业务数据库</dd><dt>扫描引擎</dt><dd>Codex Security · 独立扫描工作目录</dd></dl></section>
 </section>
</template>

<style scoped>
.advanced-diagnostics{min-width:0}header{display:flex;align-items:start;justify-content:space-between;gap:var(--space-3);flex-wrap:wrap}h2{margin:0;font-size:var(--font-size-lg);font-weight:var(--font-weight-semibold)}h3{margin:0 0 var(--space-3);font-size:var(--font-size-md);font-weight:var(--font-weight-semibold)}header p,.muted,p,dl,li{font-size:var(--font-size-sm);line-height:var(--line-height-body)}header p{margin:var(--space-2) 0 0;max-width:75ch;color:var(--text-secondary)}.diagnostic-section{margin-top:var(--space-6);padding-top:var(--space-5);border-top:1px solid var(--border-default);min-width:0;scroll-margin-top:var(--space-4)}.muted,dt{color:var(--text-secondary)}.muted{max-width:75ch}.section-heading{display:flex;align-items:baseline;justify-content:space-between;gap:var(--space-2) var(--space-4);flex-wrap:wrap;margin-bottom:var(--space-3)}.section-heading h3{margin:0}.snapshot-time{font-size:var(--font-size-xs);color:var(--text-secondary);font-variant-numeric:tabular-nums}.node-policy{margin:var(--space-3) 0}ol{padding:0;margin:var(--space-3) 0 0;list-style:none}li{display:grid;grid-template-columns:auto minmax(0,1fr);gap:var(--space-1) var(--space-4);padding:var(--space-3) 0;border-bottom:1px solid var(--border-subtle)}li strong{font-weight:var(--font-weight-medium)}li span:last-child{grid-column:2;color:var(--text-secondary);overflow-wrap:anywhere}dl{display:grid;grid-template-columns:100px minmax(0,1fr);gap:var(--space-2) var(--space-3)}dd{margin:0;overflow-wrap:anywhere}[role=alert]{color:var(--state-danger)}
a{color:var(--accent-primary)}
</style>
