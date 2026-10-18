<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref} from 'vue';
import {api} from '../api';import type {SDKIdentitySnapshot,SDKIdentityOperation} from '../contracts';
import {checkedIdentities,checkedIdentityOperations,identityState,identityOperationState,identityAction} from '../utils/sdk-identities';
import {useAuth} from '../composables/auth';import {absoluteTime} from '../utils/time';
import Button from './ui/Button.vue';import Input from './ui/Input.vue';import Select from './ui/Select.vue';import {confirm} from './ui/confirm';
const {canAdmin}=useAuth();const items=ref<SDKIdentitySnapshot[]>([]),operations=ref<SDKIdentityOperation[]>([]),selected=ref(''),name=ref(''),agent=ref(''),kind=ref('chatgpt'),key=ref(''),loading=ref(false),busy=ref(false),error=ref(''),operationError=ref('');
const current=computed(()=>items.value.find(v=>v.identity.id===selected.value));const active=computed(()=>operations.value.find(v=>['queued','running','waiting'].includes(v.status)));
let timer:ReturnType<typeof setTimeout>|undefined,disposed=false,generation=0;
async function load(){if(loading.value||disposed)return;loading.value=true;error.value='';try{items.value=checkedIdentities(await api.sdkIdentities());if(!selected.value&&items.value[0])selected.value=items.value[0].identity.id;if(selected.value&&canAdmin.value)await readOperations(selected.value)}catch(e){error.value=(e as Error).message}finally{loading.value=false}}
async function readOperations(id:string){const n=++generation;operationError.value='';try{const value=checkedIdentityOperations(await api.sdkIdentityOperations(id));if(n===generation&&!disposed)operations.value=value}catch(e){if(n===generation)operationError.value='身份操作读取失败：'+(e as Error).message}}
async function select(id:string){selected.value=id;key.value='';operations.value=[];operationError.value='';if(canAdmin.value)await readOperations(id)}
async function create(){busy.value=true;error.value='';try{const v=await api.createSDKIdentity({name:name.value.trim(),agentId:agent.value.trim(),kind:kind.value});selected.value=v.identity.id;key.value='';operations.value=[];name.value='';await load()}catch(e){error.value='身份创建失败：'+(e as Error).message}finally{busy.value=false}}
async function action(value:SDKIdentityOperation['action']){const v=current.value;if(!v||busy.value)return;if(value==='logout'&&!await confirm({title:'撤销 SDK 执行身份',body:'立即禁止新任务领取，并请求取消使用此身份的运行任务。绑定节点完成本地退出和凭据清理后显示清理完成；历史结果继续保留。'}))return;
 busy.value=true;error.value='';try{await api.startSDKIdentityOperation(v.identity.id,{generation:v.identity.generation,action:value,...(value==='login_api_key'?{apiKey:key.value}: {})});key.value='';await load()}catch(e){error.value='SDK 身份操作未创建：'+(e as Error).message}finally{busy.value=false}}
async function cancel(){if(!active.value||busy.value)return;busy.value=true;try{await api.cancelSDKIdentityOperation(active.value.id);await load()}catch(e){error.value=(e as Error).message}finally{busy.value=false}}
function move(event:KeyboardEvent,index:number){const next=index+(['ArrowDown','j'].includes(event.key)?1:-1);if(!['ArrowDown','ArrowUp','j','k'].includes(event.key)||!items.value[next])return;event.preventDefault();void select(items.value[next]!.identity.id);(event.currentTarget as HTMLElement).closest('table')?.querySelectorAll<HTMLButtonElement>('tbody button')[next]?.focus()}
async function tick(){await load();if(!disposed)timer=setTimeout(tick,2500)}onMounted(tick);onUnmounted(()=>{disposed=true;generation++;clearTimeout(timer);key.value=''});
</script>
<template>
 <section class="security-section" aria-labelledby="sdk-identities-title">
  <div class="security-toolbar"><h2 id="sdk-identities-title">SDK 执行身份</h2><Button variant="outline" size="sm" :loading="loading" @click="load">刷新身份</Button></div>
  <p class="security-note">与 Axiom 本地登录分开。每个身份绑定一个 Agent，任务保存凭据版本。SDK 认证状态不代表 Trusted Access 或模型访问已验证。</p>
  <p v-if="error" role="alert" class="security-error">{{error}}</p>
  <form v-if="canAdmin" class="identity-create" aria-label="创建 SDK 执行身份" @submit.prevent="create">
   <label for="sdk-name">身份名称<Input id="sdk-name" v-model="name" maxlength="120" required :disabled="busy" autocomplete="off"/></label>
   <label for="sdk-agent">绑定 Agent ID<Input id="sdk-agent" v-model="agent" maxlength="128" pattern="[A-Za-z0-9_-]+" required :disabled="busy" placeholder="执行节点中的 Agent ID" autocomplete="off"/></label>
   <label>身份类型<Select aria-label="SDK 身份类型" v-model="kind" :options="[{value:'chatgpt',label:'ChatGPT 设备码'},{value:'api_key',label:'API Key'}]" :disabled="busy"/></label>
   <Button type="submit" :loading="busy" :disabled="busy||!name.trim()||!agent.trim()">创建身份</Button>
  </form>
  <p v-if="loading&&!items.length" role="status">正在读取 SDK 执行身份…</p>
  <div class="identity-workspace">
   <div class="security-table-wrap"><table class="security-table"><caption class="sr-only">SDK 身份及绑定节点</caption><thead><tr><th>身份</th><th>状态</th><th>执行边界</th></tr></thead><tbody>
    <tr v-for="(item,index) in items" :key="item.identity.id" :aria-selected="selected===item.identity.id"><td><button type="button" class="identity-name" :aria-pressed="selected===item.identity.id" @click="select(item.identity.id)" @keydown="move($event,index)">{{item.identity.name}}</button><small>{{item.identity.kind==='chatgpt'?'ChatGPT':'API Key'}}</small></td><td>{{identityState[item.state]}}<small v-if="item.cleanupPending">等待节点清理</small></td><td><span class="mono">{{item.identity.agentId}}</span><small>凭据版本 {{item.identity.generation}}</small></td></tr>
    <tr v-if="!items.length&&!loading"><td colspan="3">尚无 SDK 执行身份。现有模型配置与历史扫描继续使用原凭据规则。</td></tr>
   </tbody></table></div>
   <aside v-if="current" class="identity-inspector" aria-label="SDK 身份详情">
    <h3>{{current.identity.name}}</h3><p>{{identityState[current.state]}} · {{current.identity.agentId}}</p>
    <dl><dt>身份 / 凭据版本</dt><dd>#{{current.identity.id}} / {{current.identity.generation}}</dd><dt>SDK 最近检查</dt><dd>{{current.checkedAt?absoluteTime(current.checkedAt):'尚未检查'}}</dd><dt>本地凭据清理</dt><dd>{{current.cleanupPending?'等待绑定节点确认；身份已禁止新任务使用':'无待完成清理'}}</dd></dl>
    <template v-if="canAdmin">
     <form v-if="current.identity.kind==='api_key'&&current.state!=='revoked'" @submit.prevent="action('login_api_key')"><label for="sdk-key">此身份的 API Key<Input id="sdk-key" v-model="key" type="password" autocomplete="new-password" :disabled="busy||!!active" maxlength="8192"/></label><Button type="submit" :disabled="busy||!!active||!key">保存并登录 API Key</Button></form>
     <div class="identity-actions"><Button v-if="current.identity.kind==='chatgpt'&&current.state!=='revoked'" :disabled="busy||!!active" @click="action('login_device_code')">开始 ChatGPT 设备码登录</Button><Button variant="outline" :disabled="busy||!!active||current.state==='revoked'" @click="action('account')">查询 SDK 账户状态</Button><Button variant="outline" :disabled="busy||(active?.action==='logout')||(current.state==='revoked'&&!current.cleanupPending)" @click="action('logout')">{{current.state==='revoked'?'重试退出清理':'撤销并退出'}}</Button></div>
     <p v-if="active" role="status">{{identityOperationState[active.status]}} · {{identityAction[active.action]}}<Button v-if="active.action!=='logout'" variant="ghost" size="sm" :disabled="busy" @click="cancel">取消身份操作</Button></p>
     <section v-if="active?.instructions" class="device-instructions" aria-label="ChatGPT 设备码授权"><h4>在浏览器中完成授权</h4><p>设备码：<strong class="mono">{{active.instructions.userCode}}</strong></p><a :href="active.instructions.verificationUrl" target="_blank" rel="noopener noreferrer">打开 OpenAI 设备授权页面</a><p>本地等待截止：{{absoluteTime(active.instructions.expiresAt)}}；此时间不代表提供方承诺的有效期。无需在 Axiom 填写 ChatGPT 密码。</p></section>
     <p v-if="operationError" role="alert" class="security-error">{{operationError}}<Button variant="ghost" size="sm" @click="readOperations(selected)">重读身份操作</Button></p>
     <h4>最近操作</h4><ol class="identity-history"><li v-for="op in operations" :key="op.id"><span>{{identityAction[op.action]}} · {{identityOperationState[op.status]}}</span><small>v{{op.identity.generation}} · {{absoluteTime(op.createdAt)}}</small><p v-if="op.error" class="security-error">{{op.error}}</p></li></ol>
    </template><p v-else class="security-note">当前账户可查看身份状态；登录、设备码和撤销操作仅向管理员开放。</p>
   </aside>
  </div>
 </section>
</template>
<style scoped>
.identity-create{display:grid;grid-template-columns:repeat(3,minmax(0,1fr)) auto;gap:var(--space-3);align-items:end;margin:var(--space-4) 0}.identity-create label,.identity-inspector label{display:block;font-size:var(--font-size-sm)}.identity-workspace{display:grid;grid-template-columns:minmax(0,1fr) minmax(260px,320px);gap:var(--space-4);align-items:start}.identity-workspace:has(> :only-child){grid-template-columns:minmax(0,1fr)}.identity-inspector{border-left:1px solid var(--border-default);padding-left:var(--space-4);min-width:0}.identity-inspector h3{font-size:var(--font-size-md);font-weight:var(--font-weight-semibold);margin:0 0 var(--space-2)}.identity-inspector h4{font-size:var(--font-size-sm);margin:var(--space-4) 0 var(--space-2)}.identity-inspector p,.identity-inspector dl,.identity-history{font-size:var(--font-size-xs);line-height:var(--line-height-body)}.identity-inspector p,.identity-inspector dd{overflow-wrap:anywhere}.identity-inspector dt{color:var(--text-secondary);margin-top:var(--space-2)}.identity-inspector dd{margin:var(--space-1) 0}.identity-name{font:inherit;color:var(--text-primary);background:none;border:0;padding:0;text-align:left;cursor:pointer;overflow-wrap:anywhere}.identity-name:focus-visible{outline:2px solid var(--border-focus);outline-offset:2px}tr[aria-selected=true]{background:var(--surface-selected)}.identity-actions{display:flex;flex-wrap:wrap;gap:var(--space-2);margin:var(--space-3) 0}.identity-history{list-style:none;padding:0;margin:0}.identity-history li{padding:var(--space-2) 0;border-bottom:1px solid var(--border-subtle)}.identity-history small{display:block;color:var(--text-secondary)}.device-instructions{border-block:1px solid var(--border-default);padding:var(--space-2) 0}.device-instructions a{font-size:var(--font-size-sm);overflow-wrap:anywhere}
@media(max-width:1050px){.identity-workspace{grid-template-columns:minmax(0,1fr)}.identity-inspector{border-left:0;border-top:1px solid var(--border-default);padding:var(--space-3) 0}.identity-create{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:550px){.identity-create{grid-template-columns:minmax(0,1fr)}}
</style>
