<script setup lang="ts">
import { onMounted, reactive, ref, watch } from 'vue';
import { api } from '../api';
import type { Account, User } from '../types';
import { roleLabels, useAuth } from '../composables/auth';
import Button from './ui/Button.vue';
import Input from './ui/Input.vue';
import Select from './ui/Select.vue';
import Dialog from './ui/Dialog.vue';
import { confirm } from './ui/confirm';
const {user} = useAuth();
const accounts = ref<Account[]>([]), loading = ref(false), error = ref(''), saving = ref(false), dialogOpen = ref(false), formError = ref('');
const target = ref<Account | null>(null);
const form = reactive({username: '', password: '', role: 'viewer', active: true});
const roles = Object.entries(roleLabels).map(([value,label]) => ({value, label}));
const roleDescriptions: Record<string,string> = {viewer: '查看工作空间数据、漏洞证据和历史报告。', operator: '查看数据，并创建、取消、重试扫描和标记漏洞。', admin: '管理账户、模型凭据、调度配置，并删除工作空间数据。'};
async function load() {
  loading.value = true; error.value = '';
  try { accounts.value = await api.listAccounts(); }
  catch(e) { error.value = (e as Error).message; }
  finally { loading.value = false; }
}
function open(account: Account | null) {
  target.value = account; Object.assign(form, {username: account?.username ?? '', password: '', role: account?.role ?? 'viewer', active: account?.active ?? true});
  formError.value = ''; dialogOpen.value = true;
}
watch(dialogOpen, value => { if (!value) form.password = ''; });
async function save() {
  if (saving.value) return;
  formError.value = '';
  if (!target.value && (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(form.username) || [...form.password].length < 12 || new TextEncoder().encode(form.password).length > 1024)) {
    formError.value = '用户名须为 1–64 个字母、数字、点、横线或下划线；密码至少 12 个字符，最多 1024 字节。'; return;
  }
  if (target.value && (form.role !== target.value.role || form.active !== target.value.active)) {
    if (!await confirm({title: '更新账户权限', body: `更新「${target.value.username}」后，该账户的全部会话会撤销。${form.active ? '重新登录后使用新权限。' : '停用后不能登录。'}`})) return;
  }
  saving.value = true;
  try {
    if (target.value) await api.updateAccount(target.value.id, form.role as User['role'], form.active);
    else await api.createAccount(form.username, form.password, form.role as User['role']);
    dialogOpen.value = false; form.password = ''; await load();
  } catch(e) { formError.value = (e as Error).message; }
  finally { saving.value = false; }
}
onMounted(load);
</script>
<template>
  <section class="security-section" aria-labelledby="accounts-title">
    <div class="security-toolbar"><h2 id="accounts-title">工作空间账户</h2><div class="security-actions"><Button variant="outline" size="sm" :loading="loading" @click="load">刷新账户</Button><Button size="sm" @click="open(null)">创建账户</Button></div></div>
    <p class="security-note">所有角色共享本工作空间的项目与报告；管理员管理账户、模型凭据和删除操作。</p>
    <p v-if="error" class="security-error" role="alert">{{error}}</p><p v-if="loading && !accounts.length" role="status">正在加载账户…</p>
    <div v-else class="security-table-wrap"><table class="security-table"><caption class="sr-only">工作空间账户和角色</caption><thead><tr><th>账户</th><th>角色</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="account in accounts" :key="account.id"><td>{{account.username}}<small v-if="account.id === user?.id">当前账户</small></td><td>{{roleLabels[account.role]}}</td><td>{{account.active ? '启用' : '停用'}}</td><td><Button v-if="account.id !== user?.id" variant="ghost" size="sm" :aria-label="`管理账户 ${account.username}`" @click="open(account)">管理</Button><span v-else class="security-note">由其他管理员修改</span></td></tr><tr v-if="!accounts.length"><td colspan="4">没有可显示的账户。</td></tr></tbody></table></div>
  </section>
  <Dialog v-model:open="dialogOpen" :title="target ? '管理账户' : '创建账户'" width="460px">
    <form id="account-form" class="security-form" @submit.prevent="save">
      <p v-if="formError" class="security-error" role="alert">{{formError}}</p>
      <label for="account-name">用户名</label><Input id="account-name" v-model="form.username" :disabled="!!target" autocomplete="off" />
      <template v-if="!target"><label for="account-password">初始密码</label><Input id="account-password" v-model="form.password" type="password" autocomplete="new-password" aria-describedby="account-password-note" /><p id="account-password-note" class="security-note">至少 12 个字符，最多 1024 字节。</p></template>
      <label for="account-role">角色</label><Select id="account-role" v-model="form.role" :options="roles" aria-label="账户角色" /><p class="security-note">{{roleDescriptions[form.role]}}</p>
      <label v-if="target" class="security-check"><input v-model="form.active" type="checkbox" />允许登录</label>
    </form>
    <template #footer><Button variant="secondary" @click="dialogOpen = false">取消</Button><Button type="submit" form="account-form" :loading="saving">{{target ? '保存权限' : '创建账户'}}</Button></template>
  </Dialog>
</template>
