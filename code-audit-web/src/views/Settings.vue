<script setup lang="ts">
import ModelsAndAuthorization from "../components/ModelsAndAuthorization.vue";
import AdvancedDiagnostics from "../components/AdvancedDiagnostics.vue";
import StorageMaintenance from "../components/StorageMaintenance.vue";
import ExecutionSettings from "../components/ExecutionSettings.vue";
import { ref } from "vue";
import { useRoute, useRouter } from 'vue-router';
import { useTheme } from "../composables/theme";
import { Sun, Moon } from "lucide-vue-next";
import PageHeader from "../components/PageHeader.vue";
import Button from "../components/ui/Button.vue";
import ChangePasswordDialog from "../components/ChangePasswordDialog.vue";
import { computed } from 'vue';
import { useAuth } from '../composables/auth';
import AccountSessions from '../components/AccountSessions.vue';
import AccountsAdministration from '../components/AccountsAdministration.vue';
import OperationHistory from '../components/OperationHistory.vue';
import '../styles/security.css';

/* 系统设置：账户与运行环境 */
const {user, roleLabel, canAdmin} = useAuth();
const { isDark, setTheme } = useTheme();
const passwordOpen = ref(false);
const groups = computed(() => [
 {label:'日常设置',items:[{id:'models',label:'模型与授权'},{id:'execution',label:'扫描执行'},{id:'storage',label:'存储与清理'},{id:'account',label:'账户与外观'}]},
 ...(canAdmin.value?[{label:'管理员',items:[{id:'users',label:'账户管理'},{id:'operations',label:'操作日志'}]}]:[]),
 {label:'高级',items:[{id:'diagnostics',label:'高级诊断'},{id:'about',label:'关于系统'}]},
]);
const sections = computed(()=>groups.value.flatMap(group=>group.items));
const route = useRoute(), router = useRouter();
const activeSection = computed({
  get: () => ['users','operations'].includes(String(route.query.section)) || sections.value.some(section => section.id === route.query.section) ? String(route.query.section) : 'account',
  set: section => { void router.replace({query: {section,...(section==='models'?{tab:'models'}:{})}}); },
});
const adminDenied=computed(()=>!canAdmin.value&&['users','operations'].includes(activeSection.value));
</script>

<template>
  <div class="settings-col">
    <PageHeader title="系统设置" description="管理模型连接、扫描偏好和工作空间账户。" />
    <div class="settings-layout">
    <nav class="settings-nav" aria-label="设置分类"><section v-for="group in groups" :key="group.label" :aria-label="group.label"><h2>{{group.label}}</h2><div><button v-for="section in group.items" :key="section.id" type="button" :aria-current="activeSection === section.id ? 'page' : undefined" @click="activeSection = section.id">{{ section.label }}</button></div></section></nav>
    <div class="settings-wrap">
      <p v-if="adminDenied" role="alert" class="request-error">需要管理员权限才能访问此设置。请从左侧选择你有权使用的功能。</p>
      <ModelsAndAuthorization v-if="activeSection==='models'"/>
      <KeepAlive><ExecutionSettings v-if="activeSection === 'execution'" /></KeepAlive>
      <AdvancedDiagnostics v-if="activeSection === 'diagnostics'"/>
      <StorageMaintenance v-if="activeSection === 'storage'" />
      <div v-if="activeSection === 'account'" class="section">
        <div class="section-label">账户</div>
        <div class="row"><span class="k">当前用户</span><span class="v mono">{{ user?.username }}</span></div>
        <div class="row"><span class="k">账户角色</span><span class="v">{{ roleLabel }}</span></div>
        <div class="row">
          <span class="k">密码</span>
          <Button variant="outline" size="sm" @click="passwordOpen = true">修改密码</Button>
        </div>
      </div>

      <AccountSessions v-if="activeSection === 'account'" />
      <AccountsAdministration v-if="activeSection === 'users' && canAdmin" />
      <OperationHistory v-if="activeSection === 'operations' && canAdmin" />

      <div v-if="activeSection === 'account'" class="section">
        <div class="section-label">外观</div>
        <div class="row"><span class="k">主题</span><div class="theme-options"><Button :variant="!isDark?'secondary':'ghost'" size="sm" :aria-pressed="!isDark" @click="setTheme(false)"><Sun :size="13"/>浅色</Button><Button :variant="isDark?'secondary':'ghost'" size="sm" :aria-pressed="isDark" @click="setTheme(true)"><Moon :size="13"/>深色</Button></div></div>
      </div>

      <div v-if="activeSection === 'about'" class="section">
        <div class="section-label">关于</div>
        <div class="row"><span class="k">产品</span><span class="v">Axiom Security Console</span></div>
        <div class="row"><span class="k">扫描引擎</span><span class="v">Codex Security</span></div>
        <RouterLink to="/settings?section=diagnostics">查看运行诊断</RouterLink>
      </div>
    </div>
    </div>
    <ChangePasswordDialog v-model:open="passwordOpen" />
  </div>
</template>

<style scoped>
.settings-col{max-width:var(--content-max-width);min-width:0}.settings-layout{display:grid;grid-template-columns:160px minmax(0,1fr);gap:var(--space-6);align-items:start}.settings-wrap{min-width:0}
.settings-nav{position:sticky;top:var(--space-6);max-height:calc(100dvh - var(--space-6) * 2);overflow-y:auto;border-right:1px solid var(--border-default);padding-right:var(--space-4)}.settings-nav section+section{margin-top:var(--space-6)}.settings-nav h2{margin:0 0 var(--space-2);font-size:var(--font-size-xs);color:var(--text-tertiary);font-weight:var(--font-weight-medium)}.settings-nav section>div{display:flex;flex-direction:column;gap:var(--space-1)}
.settings-nav button{padding:var(--space-2);border:0;text-align:left;background:none;color:var(--text-secondary);font:inherit;font-size:var(--font-size-sm);cursor:pointer;min-height:var(--height-md)}
.settings-nav button:hover{background:var(--surface-hover);color:var(--text-primary)}.settings-nav button[aria-current]{background:var(--surface-selected);color:var(--accent-primary);font-weight:var(--font-weight-medium)}
.settings-nav button:focus-visible{outline:2px solid var(--accent-primary);outline-offset:2px}
.section{max-width:720px;padding:var(--space-4) 0}
.section+.section{border-top:1px solid var(--border-default);margin-top:var(--space-4)}
.section-label{font-size:var(--font-size-md);font-weight:600;margin-bottom:var(--space-3)}
.row{display:grid;grid-template-columns:140px minmax(0,1fr);gap:var(--space-4);align-items:center;min-height:48px;font-size:var(--font-size-sm)}
.k{color:var(--text-secondary)}.v{color:var(--text-primary);overflow-wrap:anywhere}.row>button{justify-self:start}.theme-options{display:flex;gap:var(--space-1)}
.section>a{display:inline-block;font-size:var(--font-size-sm);color:var(--accent-primary);margin-top:var(--space-3)}
@media(max-width:1100px){.settings-layout{grid-template-columns:1fr;gap:var(--space-5)}.settings-nav{position:static;max-height:none;overflow:visible;display:flex;gap:var(--space-6);flex-wrap:wrap;border-right:0;border-bottom:1px solid var(--border-default);padding:0 0 var(--space-4)}.settings-nav section+section{margin-top:0}.settings-nav section>div{flex-direction:row;flex-wrap:wrap;gap:var(--space-1) var(--space-2)}}
@media(max-width:600px){.row{grid-template-columns:96px minmax(0,1fr)}.settings-nav{display:block}.settings-nav section{display:grid;grid-template-columns:64px minmax(0,1fr);gap:var(--space-2);align-items:start}.settings-nav section+section{margin-top:var(--space-2)}.settings-nav h2{padding-top:var(--space-2);margin:0}.settings-nav button{padding:var(--space-2) var(--space-1)}}
</style>
