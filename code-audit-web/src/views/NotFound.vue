<script setup lang="ts">
import PageHeader from '../components/PageHeader.vue';
</script>

<template>
  <section aria-labelledby="missing-page-title">
    <PageHeader title="页面不存在" />
    <h2 id="missing-page-title">无法找到这个工作空间页面</h2>
    <p>链接可能有误或已失效。请从导航中重新打开项目、扫描任务或报告。</p>
    <nav aria-label="返回工作空间"><router-link to="/">返回工作台</router-link><router-link to="/scans">查看扫描任务</router-link></nav>
  </section>
</template>

<style scoped>
h2{font-size:var(--font-size-lg);font-weight:var(--font-weight-medium)}
p{font-size:var(--font-size-md);color:var(--text-secondary)}
nav{display:flex;gap:var(--space-5);margin-top:var(--space-5);font-size:var(--font-size-md);color:var(--accent-primary)}
</style>
