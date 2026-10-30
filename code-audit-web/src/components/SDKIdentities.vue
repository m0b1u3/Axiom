<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch} from 'vue';
import {useRoute,useRouter} from 'vue-router';
import {api} from '../api';import type {SDKIdentitySnapshot,SDKIdentityOperation} from '../contracts';
import {checkedIdentities,checkedIdentityOperations,identityState,identityOperationState,usableIdentity} from '../utils/sdk-identities';
import SDKAccountScanSetup from './SDKAccountScanSetup.vue';
import {useAuth} from '../composables/auth';import {absoluteTime} from '../utils/time';
import type {AgentPresence} from '../types';
import Button from './ui/Button.vue';import Input from './ui/Input.vue';import Select from './ui/Select.vue';import {confirm} from './ui/confirm';
import '../styles/security.css';
const props=defineProps<{identityId?:string;agentId?:string}>();const route=useRoute(),router=useRouter();
const identityAction:Record<SDKIdentityOperation['action'],string>={login_api_key:'API Key 登录',login_device_code:'ChatGPT 设备码登录',account:'查询账户',logout:'退出授权账户'};
const operationState=(status:SDKIdentityOperation['status'])=>status==='queued'?'等待执行位置':identityOperationState[status];
const {canAdmin,canOperate}=useAuth();const items=ref<SDKIdentitySnapshot[]>([]),operations=ref<SDKIdentityOperation[]>([]),selected=ref(''),name=ref(''),agent=ref(/^[A-Za-z0-9_-]{1,128}$/.test(props.agentId??'')?props.agentId!:''),kind=ref('chatgpt'),key=ref(''),loading=ref(false),busy=ref(false),error=ref(''),operationError=ref('');
const locations=ref<AgentPresence[]>([]),locationsLoading=ref(false),locationError=ref('');
const locationOptions=computed(()=>{const options=locations.value.slice().sort((a,b)=>a.agentId.localeCompare(b.agentId)).map(item=>({value:item.agentId,label:item.agentId+' · '+(item.online?'在线':'离线')}));if(agent.value&&!options.some(item=>item.value===agent.value))options.unshift({value:agent.value,label:agent.value+' · 未上报'});return options});
async function loadLocations(){if(locationsLoading.value)return;locationsLoading.value=true;locationError.value='';try{locations.value=(await api.queueOverview()).agents;if(!agent.value&&locations.value.length===1)agent.value=locations.value[0]!.agentId}catch(e){locationError.value='执行位置读取失败：'+(e as Error).message}finally{locationsLoading.value=false}}
watch(()=>props.agentId,value=>{if(/^[A-Za-z0-9_-]{1,128}$/.test(value??''))agent.value=value!});
const current=computed(()=>items.value.find(v=>v.identity.id===selected.value));const active=computed(()=>operations.value.find(v=>['queued','running','waiting'].includes(v.status)));
let timer:ReturnType<typeof setTimeout>|undefined,disposed=false,generation=0;
async function load(){if(loading.value||disposed)return;loading.value=true;error.value='';try{items.value=checkedIdentities(await api.sdkIdentities());if(!selected.value)selected.value=items.value.find(item=>item.identity.id===props.identityId)?.identity.id??items.value[0]?.identity.id??'';if(selected.value&&canAdmin.value)await readOperations(selected.value)}catch(e){error.value=(e as Error).message}finally{loading.value=false}}
async function readOperations(id:string){const n=++generation;operationError.value='';try{const value=checkedIdentityOperations(await api.sdkIdentityOperations(id));if(n===generation&&!disposed)operations.value=value}catch(e){if(n===generation)operationError.value='身份操作读取失败：'+(e as Error).message}}
async function select(id:string){selected.value=id;key.value='';operations.value=[];operationError.value='';void router.replace({query:{...route.query,identity:id}});if(canAdmin.value)await readOperations(id)}
async function create(){busy.value=true;error.value='';try{const v=await api.createSDKIdentity({name:name.value.trim(),agentId:agent.value.trim(),kind:kind.value});selected.value=v.identity.id;key.value='';operations.value=[];name.value='';await load()}catch(e){error.value='身份创建失败：'+(e as Error).message}finally{busy.value=false}}
async function action(value:SDKIdentityOperation['action']){const v=current.value;if(!v||busy.value)return;if(value==='logout'&&!await confirm({title:'撤销授权账户',body:'撤销后立即禁止新任务使用此账户，并请求取消正在使用它的任务。执行位置完成退出与凭据清理后更新状态；历史结果继续保留。'}))return;
 busy.value=true;error.value='';try{await api.startSDKIdentityOperation(v.identity.id,{generation:v.identity.generation,action:value,...(value==='login_api_key'?{apiKey:key.value}: {})});key.value='';await load()}catch(e){error.value='授权操作未创建：'+(e as Error).message}finally{busy.value=false}}
async function cancel(){if(!active.value||busy.value)return;busy.value=true;try{await api.cancelSDKIdentityOperation(active.value.id);await load()}catch(e){error.value=(e as Error).message}finally{busy.value=false}}
function move(event:KeyboardEvent,index:number){const next=index+(['ArrowDown','j'].includes(event.key)?1:-1);if(!['ArrowDown','ArrowUp','j','k'].includes(event.key)||!items.value[next])return;event.preventDefault();void select(items.value[next]!.identity.id);(event.currentTarget as HTMLElement).closest('table')?.querySelectorAll<HTMLButtonElement>('tbody button')[next]?.focus()}
async function tick(){await load();if(!disposed)timer=setTimeout(tick,2500)}onMounted(()=>{void tick();if(canAdmin.value)void loadLocations()});onUnmounted(()=>{disposed=true;generation++;clearTimeout(timer);key.value=''});
</script>
<template>
 <section class="security-section" aria-labelledby="sdk-identities-title">
  <div class="security-toolbar"><h3 id="sdk-identities-title">授权账户</h3><Button variant="outline" size="sm" :loading="loading" @click="load">刷新账户</Button></div>
  <p class="security-note">用于模型调用，与 Axiom 账户登录分开管理。每个授权账户固定一个执行位置；登录成功后仍需确认所选模型的访问权限。</p>
  <p v-if="error" role="alert" class="security-error">{{error}}</p>
  <form v-if="canAdmin" class="identity-create" aria-label="添加授权账户" @submit.prevent="create">
   <label for="sdk-name">账户名称<Input id="sdk-name" v-model="name" maxlength="120" required :disabled="busy" autocomplete="off"/></label>
   <label for="sdk-agent">执行位置<Select id="sdk-agent" aria-label="执行位置" v-model="agent" :options="locationOptions" :disabled="busy||locationsLoading" placeholder="选择执行位置"/></label>
   <label>授权方式<Select aria-label="授权方式" v-model="kind" :options="[{value:'chatgpt',label:'ChatGPT 设备码'},{value:'api_key',label:'API Key'}]" :disabled="busy"/></label>
   <Button type="submit" :loading="busy" :disabled="busy||!name.trim()||!agent.trim()">添加授权账户</Button>
  </form>
  <p v-if="canAdmin&&locationError" role="alert" class="security-error">{{locationError}}<Button variant="ghost" size="sm" :loading="locationsLoading" @click="loadLocations">重读执行位置</Button></p>
  <p v-if="canAdmin" class="security-note">找不到执行位置？<RouterLink to="/settings?section=diagnostics&binding=manual">在高级诊断中检查或手动指定</RouterLink>。</p>
  <p v-if="loading&&!items.length" role="status">正在读取授权账户…</p>
  <div class="identity-workspace">
   <div class="security-table-wrap" role="region" aria-label="授权账户数据表，可横向滚动" tabindex="0"><table class="security-table"><caption class="sr-only">授权账户及登录状态</caption><thead><tr><th>账户</th><th>状态</th><th>诊断</th></tr></thead><tbody>
    <tr v-for="(item,index) in items" :key="item.identity.id" :aria-selected="selected===item.identity.id"><td><button type="button" class="identity-name" :aria-pressed="selected===item.identity.id" @click="select(item.identity.id)" @keydown="move($event,index)">{{item.identity.name}}</button><small>{{item.identity.kind==='chatgpt'?'ChatGPT':'API Key'}}</small></td><td>{{identityState[item.state]}}<small v-if="item.cleanupPending">等待退出清理</small></td><td><RouterLink :to="{path:'/settings',query:{section:'diagnostics',identity:item.identity.id}}">查看绑定与版本</RouterLink></td></tr>
    <tr v-if="!items.length&&!loading"><td colspan="3">尚无授权账户。使用模型配置中 API Key 的现有扫描方式继续保留。</td></tr>
   </tbody></table></div>
   <aside v-if="current" class="identity-inspector" aria-label="授权账户详情">
    <h3>{{current.identity.name}}</h3><p>{{identityState[current.state]}}</p>
    <dl><dt>最近核对</dt><dd>{{current.checkedAt?absoluteTime(current.checkedAt):'尚未核对'}}</dd><dt>退出清理</dt><dd>{{current.cleanupPending?'等待执行位置确认；已禁止新任务使用':'无待完成清理'}}</dd></dl>
    <RouterLink :to="{path:'/settings',query:{section:'diagnostics',identity:current.identity.id}}">查看此账户的高级诊断</RouterLink>
    <SDKAccountScanSetup v-if="canOperate&&usableIdentity(current)" :key="current.identity.id" :identity="current" :disabled="busy||!!active||!!error"/>
    <template v-if="canAdmin">
     <form v-if="current.identity.kind==='api_key'&&current.state!=='revoked'" @submit.prevent="action('login_api_key')"><label for="sdk-key">此账户的 API Key<Input id="sdk-key" v-model="key" type="password" autocomplete="new-password" :disabled="busy||!!active" maxlength="8192"/></label><Button type="submit" :disabled="busy||!!active||!key">保存并登录 API Key</Button></form>
     <div class="identity-actions"><Button v-if="current.identity.kind==='chatgpt'&&current.state!=='revoked'" :disabled="busy||!!active" @click="action('login_device_code')">开始 ChatGPT 设备码登录</Button><Button variant="outline" :disabled="busy||!!active||current.state==='revoked'" @click="action('account')">查询账户状态</Button><Button variant="outline" :disabled="busy||(active?.action==='logout')||(current.state==='revoked'&&!current.cleanupPending)" @click="action('logout')">{{current.state==='revoked'?'重试退出清理':'撤销并退出'}}</Button></div>
     <p v-if="active" role="status">{{operationState(active.status)}} · {{identityAction[active.action]}}<Button v-if="active.action!=='logout'" variant="ghost" size="sm" :disabled="busy" @click="cancel">取消授权操作</Button></p>
     <section v-if="active?.instructions" class="device-instructions" aria-label="ChatGPT 设备码授权"><h4>在浏览器中完成授权</h4><p>设备码：<strong class="mono">{{active.instructions.userCode}}</strong></p><a :href="active.instructions.verificationUrl" target="_blank" rel="noopener noreferrer">打开 OpenAI 设备授权页面</a><p>本地等待截止：{{absoluteTime(active.instructions.expiresAt)}}；此时间不代表提供方承诺的有效期。无需在 Axiom 填写 ChatGPT 密码。</p></section>
     <p v-if="operationError" role="alert" class="security-error">{{operationError}}<Button variant="ghost" size="sm" @click="readOperations(selected)">重读身份操作</Button></p>
     <h4>最近操作</h4><ol class="identity-history"><li v-for="op in operations" :key="op.id"><span>{{identityAction[op.action]}} · {{operationState(op.status)}}</span><small>{{absoluteTime(op.createdAt)}}</small><p v-if="op.error" class="security-error">{{op.error}}</p></li></ol>
    </template><p v-else class="security-note">当前角色可查看授权状态；登录、设备码和撤销操作仅向管理员开放。</p>
   </aside>
  </div>
 </section>
</template>
<style scoped>
.security-toolbar h3{margin:0;font-size:var(--font-size-md);font-weight:var(--font-weight-semibold)}
a{color:var(--accent-primary);font-size:var(--font-size-sm)}
.identity-create{display:grid;grid-template-columns:repeat(3,minmax(0,1fr)) auto;gap:var(--space-3);align-items:end;margin:var(--space-4) 0}.identity-create label,.identity-inspector label{display:block;font-size:var(--font-size-sm)}.identity-workspace{display:grid;grid-template-columns:minmax(0,1fr) minmax(260px,320px);gap:var(--space-4);align-items:start}.identity-workspace:has(> :only-child){grid-template-columns:minmax(0,1fr)}.identity-inspector{border-left:1px solid var(--border-default);padding-left:var(--space-4);min-width:0}.identity-inspector h3{font-size:var(--font-size-md);font-weight:var(--font-weight-semibold);margin:0 0 var(--space-2)}.identity-inspector h4{font-size:var(--font-size-sm);margin:var(--space-4) 0 var(--space-2)}.identity-inspector p,.identity-inspector dl,.identity-history{font-size:var(--font-size-xs);line-height:var(--line-height-body)}.identity-inspector p,.identity-inspector dd{overflow-wrap:anywhere}.identity-inspector dt{color:var(--text-secondary);margin-top:var(--space-2)}.identity-inspector dd{margin:var(--space-1) 0}.identity-name{font:inherit;color:var(--text-primary);background:none;border:0;padding:0;text-align:left;cursor:pointer;overflow-wrap:anywhere}.identity-name:focus-visible{outline:2px solid var(--border-focus);outline-offset:2px}tr[aria-selected=true]{background:var(--surface-selected)}.identity-actions{display:flex;flex-wrap:wrap;gap:var(--space-2);margin:var(--space-3) 0}.identity-history{list-style:none;padding:0;margin:0}.identity-history li{padding:var(--space-2) 0;border-bottom:1px solid var(--border-subtle)}.identity-history small{display:block;color:var(--text-secondary)}.device-instructions{border-block:1px solid var(--border-default);padding:var(--space-2) 0}.device-instructions a{font-size:var(--font-size-sm);overflow-wrap:anywhere}
@media(max-width:1050px){.identity-workspace{grid-template-columns:minmax(0,1fr)}.identity-inspector{border-left:0;border-top:1px solid var(--border-default);padding:var(--space-3) 0}.identity-create{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:550px){.identity-create{grid-template-columns:minmax(0,1fr)}}
</style>
