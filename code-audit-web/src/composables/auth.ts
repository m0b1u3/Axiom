import { computed, readonly, ref } from 'vue';
import { api, getToken, TOKEN_KEY } from '../api';
import type { User } from '../types';

const user = ref<User | null>(null);
const error = ref('');
const loading = ref(false);
let generation = 0;
let pending: Promise<void> | null = null;
export const roleLabels = {admin: '管理员', operator: '操作员', viewer: '只读用户'};

async function refresh(): Promise<void> {
  if (pending) return pending;
  const token = getToken(), version = ++generation;
  if (!token) { user.value = null; return; }
  loading.value = true;
  error.value = '';
  pending = (async () => {
    try {
      const result = await api.me();
      if (version === generation && token === getToken()) user.value = result;
    } catch (e) {
      if (version === generation && token === getToken()) { user.value = null; error.value = (e as Error).message; }
    } finally {
      if (version === generation) { loading.value = false; pending = null; }
    }
  })();
  return pending;
}

window.addEventListener('axiom:auth-changed', () => {
  generation++; pending = null; user.value = null; error.value = ''; loading.value = false;
});
window.addEventListener('storage', event => {
  if (event.key === TOKEN_KEY) {
    generation++; pending = null; user.value = null;
    if (getToken()) void refresh();
    else window.dispatchEvent(new Event('axiom:session-expired'));
  }
});

export function useAuth() {
  const roleKnown = computed(() => !!user.value && user.value.active === true && Object.hasOwn(roleLabels, user.value.role) && Array.isArray(user.value.permissions));
  return {
    user: readonly(user), error: readonly(error), loading: readonly(loading), refresh,
    accept(value: User) { if (getToken()) user.value = value; },
    canOperate: computed(() => roleKnown.value && user.value!.permissions.includes('operate')),
    canAdmin: computed(() => roleKnown.value && user.value!.permissions.includes('admin')),
    roleLabel: computed(() => user.value ? roleKnown.value ? roleLabels[user.value.role] : '角色未上报' : '身份未确认'),
  };
}
