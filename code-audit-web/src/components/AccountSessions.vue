<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { api, clearToken } from '../api';
import type { LoginSession } from '../types';
import Button from './ui/Button.vue';
import { confirm } from './ui/confirm';
import { absoluteTime } from '../utils/time';
const items = ref<LoginSession[]>([]), loading = ref(false), error = ref(''), revoking = ref('');
const router = useRouter();
async function load() {
  if (loading.value) return;
  loading.value = true; error.value = '';
  try { items.value = await api.listSessions(); }
  catch (e) { error.value = (e as Error).message; }
  finally { loading.value = false; }
}
async function revoke(session: LoginSession) {
  if (revoking.value) return;
  if (!await confirm({title: session.current ? '退出当前会话' : '撤销登录会话', body: session.current ? '当前浏览器将返回登录页。后台扫描继续执行。' : '该会话的访问令牌会立即失效，使用它的浏览器需重新登录。'})) return;
  revoking.value = session.id; error.value = '';
  try {
    await api.revokeSession(session.id);
    if (session.current) { clearToken(); await router.replace({name: 'login'}); }
    else await load();
  } catch (e) { error.value = `会话撤销未完成：${(e as Error).message}`; }
  finally { revoking.value = ''; }
}
onMounted(load);
</script>
<template>
  <section class="security-section" aria-labelledby="sessions-title">
    <div class="security-toolbar"><h2 id="sessions-title">登录会话</h2><Button variant="outline" size="sm" :loading="loading" @click="load">刷新会话</Button></div>
    <p class="security-note">会话有效期为 24 小时，每个账户最多保留 10 个有效会话。修改密码、角色或停用账户会撤销全部会话。</p>
    <p v-if="error" class="security-error" role="alert">{{error}}</p>
    <p v-if="loading && !items.length" role="status">正在加载登录会话…</p>
    <div v-else class="security-table-wrap"><table class="security-table"><caption class="sr-only">当前账户的有效登录会话</caption><thead><tr><th>会话</th><th>登录时间</th><th>到期时间</th><th>操作</th></tr></thead><tbody><tr v-for="session in items" :key="session.id"><td><span>{{session.current ? '当前浏览器' : '其他会话'}}</span><small class="mono">{{session.id.slice(0,12)}}</small></td><td>{{absoluteTime(session.createdAt)}}</td><td>{{absoluteTime(session.expiresAt)}}</td><td><Button variant="ghost" size="sm" :loading="revoking === session.id" :disabled="!!revoking && revoking !== session.id" :aria-label="`${session.current ? '退出当前' : '撤销其他'}会话 ${session.id.slice(0,12)}`" @click="revoke(session)">{{session.current ? '退出此会话' : '撤销'}}</Button></td></tr><tr v-if="!items.length"><td colspan="4">没有有效会话，请重新登录。</td></tr></tbody></table></div>
  </section>
</template>
