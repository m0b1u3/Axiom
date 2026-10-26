<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch} from 'vue';
import {useRoute,useRouter} from 'vue-router';
import {api} from '../api';
import type {SDKIdentitySnapshot,SDKIdentityOperation} from '../contracts';
import {checkedIdentities,checkedIdentityOperations,identityState,identityAction,identityOperationState} from '../utils/sdk-identities';
import {absoluteTime} from '../utils/time';
import {useAuth} from '../composables/auth';
import Button from './ui/Button.vue';
import Input from './ui/Input.vue';
import Select from './ui/Select.vue';
import DiagnosticSnapshot from './DiagnosticSnapshot.vue';
const route=useRoute(),router=useRouter(),{canAdmin}=useAuth();
const items=ref<SDKIdentitySnapshot[]>([]),loading=ref(false),error=ref(''),agent=ref('');
const selected=computed(()=>items.value.find(item=>item.identity.id===route.query.identity)??items.value[0]);
const options=computed(()=>items.value.map(item=>({value:item.identity.id,label:item.identity.name})));
const operations=ref<SDKIdentityOperation[]>([]),operationError=ref('');let generation=0;
async function loadOperations(){const id=selected.value?.identity.id,n=++generation;if(!id||!canAdmin.value){operations.value=[];return}operationError.value='';try{const records=checkedIdentityOperations(await api.sdkIdentityOperations(id));if(n===generation)operations.value=records}catch(e){if(n===generation)operationError.value='授权操作诊断读取失败：'+(e as Error).message}}
watch(()=>selected.value?.identity.id,()=>{operations.value=[];void loadOperations()});onUnmounted(()=>generation++);
async function load(){if(loading.value)return;loading.value=true;error.value='';try{items.value=checkedIdentities(await api.sdkIdentities())}catch(e){error.value='授权诊断读取失败：'+(e as Error).message}finally{loading.value=false}}
function select(value:string){void router.replace({query:{...route.query,identity:value}})}
function bind(){if(!/^[A-Za-z0-9_-]{1,128}$/.test(agent.value))return;void router.push({path:'/models',query:{tab:'accounts',agent:agent.value}})}
onMounted(load);
</script>

<template>
 <section class="authorization-diagnostics" aria-labelledby="authorization-diagnostics-title">
  <header><h3 id="authorization-diagnostics-title">授权运行诊断</h3><Button variant="outline" size="sm" :loading="loading" @click="load">刷新授权诊断</Button></header>
  <p v-if="error" role="alert">{{error}}</p><p v-if="error&&items.length" role="status">以下保留上次成功的授权快照。</p><p v-if="loading&&!items.length" role="status">正在读取授权诊断…</p>
  <p v-if="!loading&&!error&&!items.length" class="muted">尚无授权账户。</p>
  <template v-if="selected">
   <label for="diagnostic-identity">授权账户</label><Select id="diagnostic-identity" aria-label="查看授权诊断" :model-value="selected.identity.id" :options="options" @update:model-value="select"/>
   <dl><dt>身份标识</dt><dd>{{selected.identity.id}}</dd><dt>Agent ID</dt><dd>{{selected.identity.agentId}}</dd><dt>凭据版本</dt><dd>{{selected.identity.generation}}</dd><dt>记录状态</dt><dd>{{identityState[selected.state]}}</dd><dt>SDK 最近检查</dt><dd>{{selected.checkedAt?absoluteTime(selected.checkedAt):'尚未检查'}}</dd><dt>凭据清理</dt><dd>{{selected.cleanupPending?'等待绑定节点确认；已禁止新任务使用':'无待完成清理'}}</dd></dl>
   <RouterLink :to="{path:'/models',query:{tab:'accounts',identity:selected.identity.id}}">返回此授权账户</RouterLink>
   <DiagnosticSnapshot title="授权原始快照" :value="selected" filename="authorization-snapshot.json"/>
   <details v-if="canAdmin"><summary>授权操作与凭据版本</summary><p v-if="operationError" role="alert">{{operationError}}<Button variant="ghost" size="sm" @click="loadOperations">重读授权操作</Button></p><div class="operation-table"><table aria-label="授权操作版本记录"><thead><tr><th>操作</th><th>凭据版本</th><th>结果</th><th>时间</th></tr></thead><tbody><tr v-for="op in operations" :key="op.id"><td>{{identityAction[op.action]}}<small v-if="op.error">{{op.error}}</small></td><td>{{op.identity.generation}}</td><td>{{identityOperationState[op.status]}}</td><td>{{absoluteTime(op.createdAt)}}</td></tr><tr v-if="!operations.length&&!operationError"><td colspan="4">尚无授权操作。</td></tr></tbody></table></div></details>
  </template>
  <details v-if="canAdmin" :open="route.query.binding==='manual'"><summary>手动指定执行位置</summary><p class="muted">执行位置尚未上报时，可指定 Agent ID，再前往添加授权账户。账户仍绑定该节点；此操作不会创建账户或启动登录。</p><form @submit.prevent="bind"><label for="manual-authorization-agent">Agent ID<Input id="manual-authorization-agent" v-model="agent" maxlength="128" pattern="[A-Za-z0-9_-]+" required autocomplete="off"/></label><Button type="submit" variant="outline" size="sm" :disabled="!/^[A-Za-z0-9_-]{1,128}$/.test(agent)">使用此执行位置添加授权</Button></form></details>
 </section>
</template>

<style scoped>
.authorization-diagnostics{min-width:0}header{display:flex;align-items:center;justify-content:space-between;gap:var(--space-3);flex-wrap:wrap}h3{margin:0;font-size:var(--font-size-md);font-weight:var(--font-weight-semibold)}label,.muted,p,dl,summary{font-size:var(--font-size-sm);line-height:var(--line-height-body)}label{display:block;margin:var(--space-3) 0 var(--space-1)}.muted,dt{color:var(--text-secondary)}.muted{max-width:75ch}dl{display:grid;grid-template-columns:minmax(100px,140px) minmax(0,1fr);gap:var(--space-2) var(--space-3);margin:var(--space-4) 0}dd{margin:0;overflow-wrap:anywhere}a{font-size:var(--font-size-sm)}.diagnostic-snapshot{margin-top:var(--space-4)}details:not(.diagnostic-snapshot){margin-top:var(--space-3);border-top:1px solid var(--border-default)}summary{cursor:pointer;padding:var(--space-3) 0;font-weight:var(--font-weight-medium)}summary:focus-visible{outline:2px solid var(--border-focus);outline-offset:2px}details>p{margin:0 0 var(--space-3)}form{display:flex;align-items:end;gap:var(--space-3);flex-wrap:wrap}form label{flex:1;min-width:0}form button{flex-shrink:0}[role=alert]{color:var(--state-danger)}
.operation-table{overflow-x:auto;margin-top:var(--space-3)}table{width:100%;border-collapse:collapse;text-align:left;font-size:var(--font-size-xs)}th,td{padding:var(--space-2);border-bottom:1px solid var(--border-default);vertical-align:top}th{font-weight:var(--font-weight-medium)}small{display:block;overflow-wrap:anywhere}
a{color:var(--accent-primary)}
</style>
