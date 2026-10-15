<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { api } from '../api';
import type { OperationRecord } from '../types';
import Button from './ui/Button.vue';
import { absoluteTime } from '../utils/time';
const entries = ref<OperationRecord[]>([]), cursor = ref(''), error = ref(''), loading = ref(false), expanded = ref('');
const outcomes = {started: '结果未确认', succeeded: '成功', failed: '失败', denied: '拒绝'};
const actions: Record<string,string> = {'session.restore-revoke':'恢复时撤销会话', 'storage.policy':'修改存储策略','storage.preview':'预览清理','storage.cancel':'放弃清理','storage.execute':'执行或继续清理','budget.update':'调整任务预算', 'cost.reconcile':'核对执行费用', 'session.login':'登录', 'session.logout':'退出', 'session.revoke':'撤销会话', 'user.password':'修改密码', 'user.create':'创建账户', 'user.update':'更新账户', 'execution.settings':'修改调度', 'project.create':'创建项目', 'project.delete':'删除项目', 'scan.create':'创建扫描', 'scan.cancel':'取消扫描', 'scan.retry':'重试扫描', 'scan.delete':'删除扫描', 'snapshot.upload':'上传快照', 'snapshot.project':'关联项目', 'snapshot.delete':'删除快照', 'finding.status':'标记漏洞', 'model.create':'创建模型', 'model.update':'修改模型', 'model.enable':'启用模型', 'model.delete':'删除模型', 'model.fetch':'获取型号', 'model.test':'测试模型', 'report.html':'导出 HTML', 'report.pdf':'导出 PDF', 'artifact.download':'下载产物', 'checkpoint.download':'下载检查点', 'access.read':'访问受限数据'};
async function load(more = false) {
  if (loading.value) return;
  loading.value = true; error.value = '';
  try { const result = await api.listOperations(more ? cursor.value : ''); entries.value = more ? [...entries.value, ...result.items] : result.items; cursor.value = result.nextCursor; }
  catch(e) { error.value = (e as Error).message; }
  finally { loading.value = false; }
}
function objectLink(entry: OperationRecord): string | undefined {
  if (!/^\d+$/.test(entry.resourceId)) return;
  if (entry.resource === 'scan') return `/scans/${entry.resourceId}`;
  if (entry.resource === 'project') return `/projects/${entry.resourceId}`;
}
onMounted(() => load());
</script>
<template>
  <section class="security-section" aria-labelledby="operations-title">
    <div class="security-toolbar"><h2 id="operations-title">操作记录</h2><Button variant="outline" size="sm" :loading="loading" @click="load()">刷新记录</Button></div>
    <p class="security-note">记录操作者、对象、时间和结果。结果未确认表示已登记操作，但未能确认最终结果。记录不包含密码、令牌或 API Key。</p>
    <p v-if="error" class="security-error" role="alert">{{error}}</p><p v-if="loading && !entries.length" role="status">正在加载操作记录…</p>
    <div v-else class="security-table-wrap"><table class="security-table"><caption class="sr-only">工作空间操作记录</caption><thead><tr><th>时间</th><th>操作者</th><th>操作 / 对象</th><th>结果</th><th>证据</th></tr></thead><tbody><template v-for="entry in entries" :key="entry.id"><tr><td>{{absoluteTime(entry.occurredAt)}}</td><td>{{entry.actorName || '未认证'}}</td><td>{{actions[entry.action] || entry.action}}<small><router-link v-if="objectLink(entry)" :to="objectLink(entry)!">{{entry.resource}} #{{entry.resourceId}}</router-link><span v-else>{{entry.resource || '受限资源'}} {{entry.resourceId ? `#${entry.resourceId.slice(0,12)}` : ''}}</span></small></td><td>{{outcomes[entry.outcome] || entry.outcome}}</td><td><Button variant="ghost" size="sm" :aria-expanded="expanded === entry.id" :aria-label="`原始记录 ${entry.id}`" @click="expanded = expanded === entry.id ? '' : entry.id">原始记录</Button></td></tr><tr v-if="expanded === entry.id"><td colspan="5"><pre class="security-raw">{{JSON.stringify(entry, null, 2)}}</pre></td></tr></template><tr v-if="!entries.length"><td colspan="5">尚无操作记录。</td></tr></tbody></table></div>
    <Button v-if="cursor" variant="outline" size="sm" :loading="loading" @click="load(true)">加载较早记录</Button>
  </section>
</template>
