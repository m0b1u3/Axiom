<script setup lang="ts">
import { reactive, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { api, ApiError, setToken } from "../api";
import { useAuth } from '../composables/auth';
import BrandMark from "../components/BrandMark.vue";
import { useTheme } from "../composables/theme";
import { Sun, Moon } from "lucide-vue-next";
import Button from "../components/ui/Button.vue";
import Input from "../components/ui/Input.vue";
import { message } from "../lib/toast";

const router = useRouter();
const route = useRoute();
const loading = ref(false);
const errorMessage = ref("");
const { isDark, toggleTheme } = useTheme();
const form = reactive({ username: "admin", password: "" });
const auth = useAuth();

async function submit(): Promise<void> {
  if (!form.username.trim() || !form.password) {
    errorMessage.value = "请输入用户名和密码。";
    return;
  }
  if (loading.value) return;
  loading.value = true;
  errorMessage.value = "";
  try {
    const { token, user } = await api.login(form.username, form.password);
    setToken(token);
    auth.accept(user);
    const redirect = String(route.query.redirect || "/");
    void router.push(redirect.startsWith("/") && !redirect.startsWith("//") ? redirect : "/");
  } catch (error) {
    errorMessage.value = (error as Error).message + (error instanceof ApiError && error.status === 429 && error.retryAfter ? `（约 ${Math.ceil(error.retryAfter / 60)} 分钟后重试）` : '');
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <div class="login"><button class="login-theme" @click="toggleTheme" :aria-label="isDark?'切换浅色模式':'切换深色模式'"><Sun v-if="isDark" :size="18"/><Moon v-else :size="18"/></button>
    <div class="panel">
      <div class="brand"><BrandMark /></div>
      <h1>登录安全工作空间</h1>
      <p class="desc">连接代码、扫描与证据，开始安全审计。</p>
      <p v-if="errorMessage" class="login-error" role="alert">{{errorMessage}}</p>

      <form @submit.prevent="submit">
        <div class="field">
          <label class="field-label" for="login-username">用户名</label>
          <Input id="login-username" v-model="form.username" autocomplete="username" />
        </div>
        <div class="field">
          <label class="field-label" for="login-password">密码</label>
          <Input
            id="login-password"
            v-model="form.password"
            type="password"
            autocomplete="current-password"
           
          />
        </div>
        <Button type="submit" class="submit" size="lg" :loading="loading" style="width: 100%">
          登录控制台
        </Button>
      </form>

    </div>
  </div>
</template>

<style scoped>
.login {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100dvh;
  padding: var(--space-6);
  background: var(--surface-app);
}
.panel {
  width: 420px;
  max-width: 100%;
  background: var(--surface-primary);
  border: 1px solid var(--border-default);
  padding: var(--space-8) var(--space-6);
  border-radius: var(--radius-xl);
  box-shadow: none;
}
.brand {
  display: flex;
  flex-direction: column;
  gap: 1px;
  margin-bottom: var(--space-5);
}
.word {
  font-weight: 800;
  font-size: 16px;
  letter-spacing: 0.04em;
  color: var(--text-primary);
}
.cap {
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.1em;
  color: var(--text-tertiary);
}
h1 {
  margin: 0 0 var(--space-1);
  font-size: 22px;
  font-weight: 600;
  letter-spacing: -0.01em;
  color: var(--text-primary);
}
.desc {
  margin: 0 0 var(--space-6);
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
}
.field {
  margin-bottom: var(--space-3);
}
.field-label {
  display: block;
  font-size: var(--font-size-xs);
  color: var(--text-secondary);
  margin-bottom: var(--space-1-5);
}
.submit {
  margin-top: var(--space-2);
}
.login-theme{position:absolute;right:var(--space-6);top:var(--space-6);padding:var(--space-2);color:var(--text-secondary)}.login-error{font-size:var(--font-size-sm);color:var(--state-danger);line-height:1.7;margin:0 0 var(--space-4)}
</style>
