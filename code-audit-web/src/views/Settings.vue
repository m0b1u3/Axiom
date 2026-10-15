<script setup lang="ts">
import StorageMaintenance from "../components/StorageMaintenance.vue";
import ExecutionSettings from "../components/ExecutionSettings.vue";
import { ref } from "vue";
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

/* 系统设置：账户与运行环境（单机密略，无远程配置项） */
const {user, roleLabel, canAdmin} = useAuth();
const { isDark, setTheme } = useTheme();
const passwordOpen = ref(false);
const activeSection = ref(new URLSearchParams(window.location.search).get('section')==='storage'?'storage':'execution');
const sections = computed(() => [{ id: 'execution', label: '扫描调度' }, {id:'storage',label:'存储与清理'}, { id: 'account', label: '账户与外观' }, ...(canAdmin.value ? [{id: 'users', label: '账户管理'}, {id: 'operations', label: '操作记录'}] : []), { id: 'about', label: '关于系统' }]);
</script>

<template>
  <div class="settings-col">
    <PageHeader title="系统设置" description="账户与运行环境" />
    <nav class="settings-nav" aria-label="设置分类"><button v-for="section in sections" :key="section.id" type="button" :aria-current="activeSection === section.id ? 'page' : undefined" @click="activeSection = section.id">{{ section.label }}</button></nav>
    <div class="settings-wrap">
      <ExecutionSettings v-show="activeSection === 'execution'" />
      <StorageMaintenance v-if="activeSection === 'storage'" />
      <div v-if="activeSection === 'account'" class="section">
        <div class="section-label">账户</div>
        <div class="row"><span class="k">当前用户</span><span class="v mono">{{ user?.username }}</span></div>
        <div class="row"><span class="k">账户角色</span><span class="v">{{ roleLabel }}</span></div>
        <div class="row"><span class="k">认证方式</span><span class="v">本地账户（单机部署）</span></div>
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
        <div class="row"><span class="k">部署形态</span><span class="v">Server + Agent · PostgreSQL 业务数据库</span></div>
        <div class="row"><span class="k">扫描引擎</span><span class="v">Codex Security · 独立扫描工作目录</span></div>
      </div>
    </div>
    <ChangePasswordDialog v-model:open="passwordOpen" />
  </div>
</template>

<style scoped>
.settings-col{max-width:1040px}
.settings-nav{display:flex;gap:var(--space-5);border-bottom:1px solid var(--border-default);margin-bottom:var(--space-5);flex-wrap:wrap}
.settings-nav button{padding:var(--space-3) 0;border:0;border-bottom:2px solid transparent;background:none;color:var(--text-secondary);font:inherit;font-size:var(--font-size-sm);cursor:pointer}
.settings-nav button[aria-current]{border-bottom-color:var(--accent-primary);color:var(--text-primary);font-weight:600}
.settings-nav button:focus-visible{outline:2px solid var(--accent-primary);outline-offset:2px}
.section{max-width:720px;padding:var(--space-4) 0}
.section+.section{border-top:1px solid var(--border-default);margin-top:var(--space-4)}
.section-label{font-size:var(--font-size-md);font-weight:600;margin-bottom:var(--space-3)}
.row{display:grid;grid-template-columns:140px minmax(0,1fr);gap:var(--space-4);align-items:center;min-height:48px;font-size:var(--font-size-sm)}
.k{color:var(--text-secondary)}.v{color:var(--text-primary);overflow-wrap:anywhere}.row>button{justify-self:start}.theme-options{display:flex;gap:var(--space-1)}
@media(max-width:600px){.row{grid-template-columns:96px minmax(0,1fr)}.settings-nav{gap:var(--space-4)}}
</style>
