<script setup lang="ts">
defineProps<{ title: string; description?: string }>();
</script>

<template>
  <div class="page-head">
    <div>
      <h1>{{ title }}</h1>
      <p v-if="description" class="desc">{{ description }}</p>
    </div>
    <div v-if="$slots.default" class="actions">
      <slot />
    </div>
  </div>
</template>

<style scoped>
/* WorkspaceHeader（design/COMPONENTS.md #3）：标题 20–22px 静默，数据面才是主角 */
.page-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-5);
  flex-wrap: wrap;
  margin-bottom: var(--space-7); /* 无下划线：标题+间距即分区，少一条装饰线 */
}
h1 {
  margin: 0;
  font-size: var(--font-size-title);
  line-height: var(--line-height-tight);
  font-weight: var(--font-weight-semibold);
  letter-spacing: -0.025em;
  color: var(--text-primary);
}
.desc {
  margin: var(--space-1-5) 0 0;
  font-size: var(--font-size-sm);
  line-height: var(--line-height-ui);
  color: var(--text-secondary);
}
.actions {
  display: flex;
  gap: var(--space-2);
  flex-wrap: wrap;
}
</style>
