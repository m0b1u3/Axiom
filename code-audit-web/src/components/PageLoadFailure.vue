<script setup lang="ts">
import { failedPage, clearPageLoadError, reloadFailedPage } from '../composables/page-load-recovery';
import Button from './ui/Button.vue';
</script>

<template>
  <section v-if="failedPage" class="page-load-failure" role="alert" aria-label="页面加载失败">
    <div class="page-load-message">
      <strong>{{ failedPage.title ? `${failedPage.title}未能打开` : '页面资源加载失败' }}</strong>
      <p>可能是页面版本更新或网络中断。请先保留未提交内容，再刷新打开；不会自动启动扫描。</p>
    </div>
    <div class="page-load-actions">
      <Button size="sm" variant="outline" @click="reloadFailedPage">刷新打开</Button>
      <Button size="sm" variant="ghost" @click="clearPageLoadError">稍后处理</Button>
    </div>
  </section>
</template>

<style scoped>
.page-load-failure{display:flex;align-items:center;justify-content:space-between;gap:var(--space-3);padding:var(--space-2) var(--space-3);border-bottom:1px solid var(--border-default);background:var(--surface-primary);color:var(--text-primary)}
.page-load-message{min-width:0;overflow-wrap:anywhere;font-size:var(--font-size-xs);line-height:var(--line-height-body)}
strong{font-weight:var(--font-weight-medium);color:var(--state-warning)}
p{margin:var(--space-1) 0 0;color:var(--text-secondary)}
.page-load-actions{display:flex;flex-shrink:0;gap:var(--space-1)}
@media(max-width:600px){.page-load-failure{align-items:flex-start;flex-direction:column;gap:var(--space-2)}}
</style>
