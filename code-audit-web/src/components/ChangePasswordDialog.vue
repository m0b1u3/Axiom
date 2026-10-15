<script setup lang="ts">
import { reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { api, clearToken } from "../api";
import Dialog from "./ui/Dialog.vue";
import Input from "./ui/Input.vue";
import Button from "./ui/Button.vue";
import { message } from "../lib/toast";

/* 修改密码对话框（用户菜单 / 系统设置共用）。成功后强制重新登录。 */
const open = defineModel<boolean>("open", { default: false });
const router = useRouter();
const form = reactive({ oldPassword: "", newPassword: "", confirm: "" });
const saving = ref(false);
const errorMessage = ref('');

function reset(): void {
  form.oldPassword = "";
  form.newPassword = "";
  form.confirm = "";
  errorMessage.value = '';
}
watch(open, (v) => {
  if (!v) reset();
});

async function submit(): Promise<void> {
  if (saving.value) return;
  if (!form.oldPassword || !form.newPassword) {
    errorMessage.value = '请填写原密码与新密码';
    return;
  }
  if ([...form.newPassword].length < 12 || new TextEncoder().encode(form.newPassword).length > 1024) {
    errorMessage.value = '新密码至少 12 个字符，最多 1024 字节';
    return;
  }
  if (form.newPassword !== form.confirm) {
    errorMessage.value = '两次输入的新密码不一致';
    return;
  }
  saving.value = true;
  errorMessage.value = '';
  try {
    await api.changePassword(form.oldPassword, form.newPassword);
    open.value = false;
    message.success("密码已修改，请重新登录");
    clearToken();
    void router.push({ name: "login" });
  } catch (error) {
    errorMessage.value = (error as Error).message;
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <Dialog v-model:open="open" title="修改密码" description="修改成功后，该账户的全部登录会话会撤销，请重新登录。" width="420px">
    <div class="cp-form">
      <p v-if="errorMessage" class="cp-error" role="alert">{{errorMessage}}</p>
      <label class="cp-label" for="cp-old">原密码</label>
      <Input id="cp-old" v-model="form.oldPassword" type="password" autocomplete="current-password" placeholder="当前使用的密码" />
      <label class="cp-label" for="cp-new">新密码</label>
      <Input id="cp-new" v-model="form.newPassword" type="password" autocomplete="new-password" placeholder="至少 12 个字符" />
      <label class="cp-label" for="cp-confirm">确认新密码</label>
      <Input id="cp-confirm" v-model="form.confirm" type="password" autocomplete="new-password" placeholder="再次输入新密码" @enter="submit" />
    </div>
    <template #footer>
      <Button variant="secondary" @click="open = false">取消</Button>
      <Button :loading="saving" @click="submit">确认修改</Button>
    </template>
  </Dialog>
</template>

<style scoped>
.cp-form {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}
.cp-label {
  font-size: var(--font-size-xs);
  color: var(--text-tertiary);
  margin-top: var(--space-1);
}
.cp-error{color:var(--state-danger);font-size:var(--font-size-sm)}
</style>
