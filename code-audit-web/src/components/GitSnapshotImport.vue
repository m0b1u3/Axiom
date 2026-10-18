<script setup lang="ts">
import {ref} from 'vue';
import {api,type SnapshotInfo} from '../api';
import type {GitDiffTarget} from '../contracts';
import Button from './ui/Button.vue';
import Input from './ui/Input.vue';
const props=defineProps<{projectId?:number;disabled?:boolean}>();
const emit=defineEmits<{imported:[value:{snapshot:SnapshotInfo;target:GitDiffTarget}];busy:[value:boolean]}>();
const file=ref<File|null>(null),base=ref('HEAD~1'),head=ref('HEAD'),busy=ref(false),error=ref('');
async function freeze(){
 if(!file.value||busy.value||props.disabled)return;
 busy.value=true;emit('busy',true);error.value='';
 try{const value=await api.uploadGitSnapshot(file.value,base.value.trim(),head.value.trim(),props.projectId);emit('imported',value);}
 catch(e){error.value=(e as Error).message;}
 finally{busy.value=false;emit('busy',false);}
}
</script>
<template>
 <details class="git-import" aria-label="导入 Git 提交差异">
  <summary>导入 Git 提交差异</summary>
  <p>上传含完整 .git 历史的 ZIP / TAR.GZ。按选定提交重建干净快照，忽略未提交工作区，不保留原仓库配置、凭据或 hooks。本操作只生成差异预览，不调用模型。</p>
  <label for="git-archive">Git 仓库归档</label><input id="git-archive" type="file" accept=".zip,.tar.gz,.tgz" :disabled="busy||disabled" @change="file=($event.target as HTMLInputElement).files?.[0]??null;error=''" />
  <div class="git-revisions"><div><label for="git-base">基准提交 base</label><Input id="git-base" v-model="base" maxlength="256" :disabled="busy||disabled" placeholder="提交 SHA、分支或标签" /></div><div><label for="git-head">目标提交 head</label><Input id="git-head" v-model="head" maxlength="256" :disabled="busy||disabled" placeholder="提交 SHA、分支或标签" /></div></div>
  <p>首版仅接受 SHA-1 完整仓库及已提交差异；浅克隆、子模块、符号链接、外部 worktree 和工作区差异暂不可用。</p>
  <p v-if="error" class="git-error" role="alert">{{error}}</p>
  <Button variant="secondary" :loading="busy" :disabled="disabled||!file||!base.trim()||!head.trim()" @click="freeze">导入并冻结差异</Button>
 </details>
</template>
<style scoped>
.git-import{border-top:1px solid var(--border-default);margin-top:var(--space-4);padding-top:var(--space-3);font-size:var(--font-size-sm)}summary{cursor:pointer;font-weight:var(--font-weight-medium)}p{font-size:var(--font-size-xs);line-height:var(--line-height-body);color:var(--text-secondary);margin:var(--space-2) 0}label{display:block;font-size:var(--font-size-xs);margin:var(--space-2) 0 var(--space-1)}input[type=file]{max-width:100%;font-size:var(--font-size-xs);color:var(--text-primary)}.git-revisions{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:var(--space-3)}.git-error{color:var(--state-danger);overflow-wrap:anywhere}@media(max-width:600px){.git-revisions{grid-template-columns:minmax(0,1fr)}}
</style>
