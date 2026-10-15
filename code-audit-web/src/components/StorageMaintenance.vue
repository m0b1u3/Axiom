<script setup lang="ts">
import {onMounted,ref,shallowRef} from 'vue';
import {api,ApiError} from '../api';
import type {StorageSnapshot,CleanupJob} from '../contracts';
import {useAuth} from '../composables/auth';
import {absoluteTime} from '../utils/time';
import Button from './ui/Button.vue';
import Input from './ui/Input.vue';
import {formatStorageBytes as bytes} from '../utils/storage';
import CleanupPlan from './CleanupPlan.vue';
const {canAdmin}=useAuth();
const data=shallowRef<StorageSnapshot|null>(null),jobs=ref<CleanupJob[]>([]),selected=shallowRef<CleanupJob|null>(null);
const loading=ref(false),saving=ref(false),previewing=ref(false),error=ref(''),jobsError=ref(''),policyError=ref(''),unsupported=ref(false),free=ref('1024'),grace=ref('24');

const states={ready:'容量满足领取阈值',low:'空间不足 · 暂停新领取',unknown:'容量未知 · 暂停新领取'};
async function loadJobs(){if(!canAdmin.value)return;try{jobs.value=await api.cleanupJobs();jobsError.value='';if(selected.value){selected.value=jobs.value.find(j=>j.id===selected.value!.id)??selected.value}}catch(e){jobsError.value=(e as Error).message}}
async function load(){if(loading.value)return;loading.value=true;error.value='';unsupported.value=false;try{data.value=await api.storage();free.value=String(data.value.policy.minFreeBytes/1024/1024);grace.value=String(data.value.policy.orphanGraceHours);}catch(e){if(e instanceof ApiError&&[404,405].includes(e.status))unsupported.value=true;else error.value=(e as Error).message}finally{loading.value=false}await loadJobs()}
async function save(){if(!data.value||saving.value)return;const min=Number(free.value)*1024*1024,hours=Number(grace.value);if(!String(free.value).trim()||!String(grace.value).trim()||!Number.isSafeInteger(min)||min<0||!Number.isInteger(hours)||hours<1||hours>8760){policyError.value='请填写有效空间阈值，宽限期须为 1–8760 小时。';return}saving.value=true;policyError.value='';try{data.value=await api.updateStoragePolicy({...data.value.policy,minFreeBytes:min,orphanGraceHours:hours});}catch(e){policyError.value=(e as Error).message}finally{saving.value=false}}
async function preview(){if(previewing.value)return;previewing.value=true;jobsError.value='';try{selected.value=await api.previewCleanup('orphans');await loadJobs()}catch(e){jobsError.value=(e as Error).message}finally{previewing.value=false}}
function changed(job:CleanupJob){selected.value=job;const i=jobs.value.findIndex(v=>v.id===job.id);if(i>=0)jobs.value[i]=job}
onMounted(load);
</script>
<template>
 <section class="storage-maintenance" aria-label="存储容量与清理">
  <header><h2>存储容量</h2><span v-if="data">{{error?'容量状态待刷新':states[data.state]}}</span><Button variant="outline" size="sm" :loading="loading" @click="load">刷新存储状态</Button></header>
  <p v-if="unsupported" role="status">当前 Server 尚未提供存储治理接口，已有任务与历史报告仍可读取。</p>
  <p v-if="error" role="alert" class="request-error">存储状态读取失败：{{error}}。当前容量不能确认，请刷新重试。</p>
  <p v-if="loading&&!data" role="status">正在检查受管理目录与卷容量…</p>
  <template v-if="data">
   <p v-if="error" role="status">以下保留上次成功的快照，不能代表当前容量。</p>
   <p class="secondary">{{absoluteTime(data.observedAt)}} · 仅影响新领取；运行中的心跳、取消和结果提交继续处理。</p>
   <div class="table-scroll" tabindex="0" aria-label="存储目录容量，可横向滚动"><table><caption class="sr-only">受管理目录与所在卷容量</caption><thead><tr><th>管理目录</th><th>文件逻辑占用</th><th>卷剩余空间</th><th>卷总容量</th><th>状态</th></tr></thead><tbody><tr v-for="root in data.roots" :key="root.kind"><td>{{root.kind==='scans'?'扫描产物':'源码快照'}}</td><td>{{bytes(root.usedBytes)}}</td><td>{{bytes(root.availableBytes)}}</td><td>{{bytes(root.volumeBytes)}}</td><td>{{root.state==='ready'?'可用':root.state==='low'?'低于领取阈值':'探测未知'}}<small v-if="root.message">{{root.message}}</small></td></tr><tr v-if="!data.roots.length"><td colspan="5">未能确认管理目录，容量未知。</td></tr></tbody></table></div>
   <p class="secondary">两个目录可能共用同一卷，卷容量不能相加。逻辑文件量不等于实际占用块数；无法完成的检查显示未知。</p>
   <details class="raw-view"><summary>原始存储快照</summary><pre>{{JSON.stringify(data,null,2)}}</pre></details>
   <form v-if="canAdmin" class="storage-policy" @submit.prevent="save"><h3>领取与保留策略</h3><p v-if="policyError" role="alert" class="request-error">{{policyError}}</p><div class="policy-fields"><div><label for="storage-free">领取前保留空间（MiB）</label><Input id="storage-free" v-model="free" type="number" min="0" step="1" /><small>默认 1024 MiB。0 仅关闭空间阈值，容量探测失败仍阻止领取。</small></div><div><label for="storage-grace">遗留目录宽限期（小时）</label><Input id="storage-grace" v-model="grace" type="number" min="1" max="8760" step="1" /><small>宽限期过后仍需预览并明确执行清理。</small></div></div><Button type="submit" variant="outline" size="sm" :loading="saving">保存存储策略</Button></form>
   <p class="retention">历史任务、报告与费用证据默认一直保留，不会自动删除。运行中任务、费用未知的证据及已登记快照不参与遗留目录清理。</p>
   <section v-if="canAdmin" class="cleanup-workspace" aria-label="清理工作区"><header><h2>清理记录</h2><Button variant="outline" size="sm" :disabled="!data.cleanupSupported" :loading="previewing" @click="preview">预览遗留目录</Button></header><p v-if="!data.cleanupSupported" role="status">当前平台尚未认证隔离清理，保留文件。</p><p v-if="jobsError" role="alert" class="request-error">清理记录读取或预览失败：{{jobsError}}</p><p v-if="!jobs.length&&!jobsError" class="secondary">尚无清理记录。预览不会删除数据。</p><ul class="job-list"><li v-for="job in jobs" :key="job.id"><button type="button" :aria-label="`查看清理记录 ${job.id}`" :aria-pressed="selected?.id===job.id" @click="selected=job"><span>{{absoluteTime(job.updatedAt)}} · {{job.kind==='orphans'?'遗留目录':job.kind==='scan'?'扫描删除':'快照删除'}}</span><span>{{job.state==='completed'?'已完成':job.state==='canceled'?'已放弃':job.state==='preview'?'待确认':'未完成 · 可继续'}}</span></button></li></ul><CleanupPlan v-if="selected" :job="selected" @change="changed" @completed="load" /></section>
  </template>
 </section>
</template>
<style scoped>
.storage-maintenance{min-width:0;padding:var(--space-4) 0;font-size:var(--font-size-sm)}header{display:flex;gap:var(--space-3);align-items:center;flex-wrap:wrap}h2{font-size:var(--font-size-md);font-weight:var(--font-weight-semibold)}header>span{font-size:var(--font-size-xs);color:var(--text-secondary)}header>button{margin-left:auto}p{margin:var(--space-3) 0;line-height:var(--line-height-body)}.secondary,small{color:var(--text-secondary);font-size:var(--font-size-xs)}.table-scroll{width:100%;overflow:auto;margin:var(--space-3) 0}table{width:100%;border-collapse:collapse;font-size:var(--font-size-xs);white-space:nowrap}th,td{padding:var(--space-2);border-bottom:1px solid var(--border-subtle);text-align:left;font-variant-numeric:tabular-nums}th{font-weight:var(--font-weight-medium);color:var(--text-secondary)}td small{display:block;white-space:normal;max-width:32ch}.storage-policy{border-top:1px solid var(--border-default);padding-top:var(--space-4);margin-top:var(--space-4)}h3{font-size:var(--font-size-md);font-weight:var(--font-weight-medium)}.policy-fields{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:var(--space-4);margin:var(--space-3) 0}.policy-fields>div{min-width:0}label{display:block;margin-bottom:var(--space-2)}small{display:block;margin-top:var(--space-1);line-height:var(--line-height-body)}.retention{border-top:1px solid var(--border-default);padding-top:var(--space-4);margin-top:var(--space-4)}.cleanup-workspace{border-top:1px solid var(--border-default);margin-top:var(--space-4);padding-top:var(--space-4)}.job-list{list-style:none;padding:0;margin:var(--space-3) 0;max-height:240px;overflow:auto}.job-list button{display:flex;width:100%;justify-content:space-between;gap:var(--space-3);text-align:left;padding:var(--space-2);border-bottom:1px solid var(--border-subtle);font-size:var(--font-size-xs)}.job-list button[aria-pressed=true]{background:var(--surface-selected)}.job-list button:hover{background:var(--surface-hover)}.raw-view{font-size:var(--font-size-xs);margin:var(--space-3) 0}.raw-view summary{cursor:pointer;color:var(--text-secondary)}.raw-view pre{max-height:20rem;overflow:auto;padding:var(--space-3);background:var(--surface-secondary);white-space:pre-wrap;overflow-wrap:anywhere}.request-error{color:var(--state-danger)}@media(max-width:600px){.policy-fields{grid-template-columns:minmax(0,1fr)}header>button{margin-left:0}.job-list button{flex-wrap:wrap}}
</style>
