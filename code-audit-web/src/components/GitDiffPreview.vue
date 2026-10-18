<script setup lang="ts">
import {onUnmounted,ref,shallowRef,watch} from 'vue';
import {api} from '../api';
import type {GitDiffTarget,GitDiffPreview,GitDiffContent} from '../contracts';
import {sameGitTarget} from '../utils/git-diff';
import Button from './ui/Button.vue';
import CatalogPagination from './CatalogPagination.vue';
const props=defineProps<{snapshotId:number;sourceHash?:string;target?:GitDiffTarget}>();
const emit=defineEmits<{state:[value:{busy:boolean;error:string}]}>();
const preview=shallowRef<GitDiffPreview|null>(null),content=shallowRef<GitDiffContent|null>(null),busy=ref(false),fileBusy=ref(false),error=ref(''),fileError=ref(''),selected=ref(-1),raw=ref(false);
let generation=0,fileGeneration=0;
const labels={added:'新增',deleted:'删除',modified:'修改',renamed:'重命名',type_changed:'类型变化'};
async function inspect(index:number){
 const g=++fileGeneration,identity=generation;selected.value=index;fileBusy.value=true;fileError.value='';content.value=null;
 try{const v=await api.gitDiffContent(props.snapshotId,index);if(g!==fileGeneration||identity!==generation)return;if(v.schemaVersion!==1||v.snapshotId!==String(props.snapshotId)||v.contentHash!==preview.value?.contentHash||!sameGitTarget(v.target,preview.value?.target)||v.file.index!==index)throw new Error('文件差异与当前冻结版本不匹配，请重新读取');content.value=v;}
 catch(e){if(g===fileGeneration&&identity===generation)fileError.value=(e as Error).message;}
 finally{if(g===fileGeneration&&identity===generation)fileBusy.value=false;}
}
async function load(page=1){
 const g=++generation;fileGeneration++;busy.value=true;error.value='';preview.value=null;content.value=null;fileError.value='';selected.value=-1;emit('state',{busy:true,error:''});
 try{const v=await api.gitDiffPreview(props.snapshotId,page);if(g!==generation)return;if(v.schemaVersion!==1||v.snapshotId!==String(props.snapshotId)||v.page!==page||!sameGitTarget(v.target,v.target)||(props.sourceHash&&v.contentHash!==props.sourceHash)||(props.target&&!sameGitTarget(v.target,props.target)))throw new Error('Git 差异与当前快照或任务冻结版本不匹配');preview.value=v;if(v.files[0])void inspect(v.files[0].index);}
 catch(e){if(g===generation){const status=(e as {status?:number}).status;error.value=status===404?'当前 Server 没有此 Git 差异或尚未支持该能力；未回退为普通扫描。':(e as Error).message;}}
 finally{if(g===generation){busy.value=false;emit('state',{busy:false,error:error.value});}}
}
function move(e:KeyboardEvent,index:number){if(!['ArrowUp','ArrowDown','Home','End'].includes(e.key)||!preview.value)return;e.preventDefault();const rows=preview.value.files,i=rows.findIndex(f=>f.index===index),next=e.key==='Home'?0:e.key==='End'?rows.length-1:Math.max(0,Math.min(rows.length-1,i+(e.key==='ArrowDown'?1:-1)));const item=rows[next];if(item){void inspect(item.index);(e.currentTarget as HTMLElement).closest('ul')?.querySelector<HTMLButtonElement>(`[data-git-index="${item.index}"]`)?.focus();}}
watch(()=>[props.snapshotId,props.sourceHash,JSON.stringify(props.target)],()=>void load(),{immediate:true});onUnmounted(()=>{generation++;fileGeneration++});
</script>
<template>
 <section class="git-preview" aria-label="Git 提交差异预览">
  <header><h3>Git 提交差异</h3><Button variant="ghost" size="sm" :disabled="busy" @click="load()">重新读取差异</Button></header>
  <p v-if="busy" role="status">正在核对冻结提交与源码完整性…</p>
  <p v-else-if="error" class="git-error" role="alert">{{error}}</p>
  <template v-else-if="preview">
   <dl><dt>Base 提交</dt><dd>{{preview.target.baseCommit}}</dd><dt>HEAD 提交</dt><dd>{{preview.target.headCommit}}</dd><dt>差异 SHA-256</dt><dd>{{preview.target.diffSha256}}</dd></dl>
   <p>已冻结 {{preview.total}} 个文件的提交差异。扫描使用 Standard；代码观察统一对照 HEAD，删除文件与旧行只作为 base 差异展示。</p>
   <p v-if="!preview.total">两个提交的源码树没有文件变化，未补造差异内容。</p>
   <div v-else class="diff-workspace">
    <div class="diff-files"><ul aria-label="差异文件列表"><li v-for="f in preview.files" :key="f.index"><button type="button" :aria-pressed="selected===f.index" :data-git-index="f.index" @click="inspect(f.index)" @keydown="move($event,f.index)"><span>{{labels[f.status]}}</span><code>{{f.oldPath&&f.newPath&&f.oldPath!==f.newPath?f.oldPath+' → ':''}}{{f.newPath??f.oldPath}}</code></button></li></ul><CatalogPagination label="差异文件分页" :page="preview.page" :page-size="preview.pageSize" :total="preview.total" :loading="busy" @change="load" /></div>
    <section class="diff-inspector" aria-label="选中文件差异">
     <p v-if="fileBusy" role="status">正在读取选定文件的差异…</p>
     <div v-else-if="fileError" role="alert"><p class="git-error">{{fileError}}</p><Button variant="outline" size="sm" @click="inspect(selected)">重读文件差异</Button></div>
     <template v-else-if="content"><h4>{{content.file.newPath??content.file.oldPath}}</h4><p>− Base · {{preview.target.baseCommit.slice(0,12)}} / ＋ HEAD · {{preview.target.headCommit.slice(0,12)}}</p><p v-if="content.reason">{{content.reason}}</p><pre tabindex="0" aria-label="原始 Git 文件补丁">{{content.patch}}</pre></template>
     <p v-else>选择文件查看对应提交的原始差异。</p>
    </section>
   </div>
   <details @toggle="raw=($event.currentTarget as HTMLDetailsElement).open"><summary>原始 Git 目标合同</summary><pre v-if="raw">{{JSON.stringify(preview.target,null,2)}}</pre></details>
  </template>
 </section>
</template>
<style scoped>
.git-preview{container-type:inline-size;min-width:0;border-top:1px solid var(--border-default);padding-top:var(--space-3);margin-top:var(--space-3);font-size:var(--font-size-sm)}header{display:flex;justify-content:space-between;align-items:center;gap:var(--space-2)}h3,h4{font-size:var(--font-size-sm);font-weight:var(--font-weight-medium);margin:0;overflow-wrap:anywhere}p{font-size:var(--font-size-xs);color:var(--text-secondary);line-height:var(--line-height-body);margin:var(--space-2) 0}dl{display:grid;grid-template-columns:max-content minmax(0,1fr);gap:var(--space-1) var(--space-3);margin:var(--space-2) 0;font-size:var(--font-size-xs)}dt{color:var(--text-secondary)}dd{margin:0;font-family:var(--font-mono);overflow-wrap:anywhere}.diff-workspace{display:grid;grid-template-columns:minmax(0,var(--sidebar-width)) minmax(0,1fr);border-top:1px solid var(--border-default);margin-top:var(--space-3)}ul{list-style:none;margin:0;padding:0;max-height:calc(var(--height-table-row)*8);overflow:auto}li{border-bottom:1px solid var(--border-subtle)}li button{display:flex;gap:var(--space-2);width:100%;border:0;text-align:left;padding:var(--space-2);background:transparent;color:var(--text-primary);font-size:var(--font-size-xs);cursor:pointer}li button:hover{background:var(--surface-hover)}li button[aria-pressed=true]{background:var(--surface-selected)}li button:focus-visible,pre:focus-visible{outline:2px solid var(--border-focus);outline-offset:-2px}li span{color:var(--text-secondary);flex-shrink:0}code{font-family:var(--font-mono);overflow-wrap:anywhere;min-width:0}.diff-files{min-width:0}.diff-files :deep(.page-controls){flex-wrap:wrap;min-width:0}.diff-files :deep(.page-jump){order:1}.diff-inspector{min-width:0;padding:var(--space-2) var(--space-3);border-left:1px solid var(--border-default)}pre{font:var(--font-size-xs)/var(--line-height-body) var(--font-mono);white-space:pre;overflow:auto;max-height:calc(var(--height-table-row)*14);margin:var(--space-2) 0;padding:var(--space-2);background:var(--surface-secondary)}details{padding:var(--space-2) 0}summary{cursor:pointer;font-size:var(--font-size-xs)}.git-error{color:var(--state-danger);overflow-wrap:anywhere}@container(max-width:640px){.diff-workspace{grid-template-columns:minmax(0,1fr)}.diff-inspector{border-left:0;border-top:1px solid var(--border-default);padding:var(--space-2) 0}dl{grid-template-columns:minmax(0,1fr)}dd{margin-bottom:var(--space-1)}}@media(max-width:760px){.diff-workspace{grid-template-columns:minmax(0,1fr)}.diff-inspector{border-left:0;border-top:1px solid var(--border-default);padding:var(--space-2) 0}dl{grid-template-columns:minmax(0,1fr)}dd{margin-bottom:var(--space-1)}}
</style>
