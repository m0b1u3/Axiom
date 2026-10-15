<script setup lang="ts">
import { computed } from "vue";

const props = defineProps<{ status: string }>();

/* 统一 StatusIndicator（design/COMPONENTS.md #32）
   状态语义：running/completed/failed/queued/waiting/canceled
   只用 ● + 文字表达，无动画、无图标堆叠 */
const MAP: Record<string, { label: string; kind: string; pulse?: boolean }> = {
  pending: { label: "排队中", kind: "neutral" },
  claimed: { label: "准备中", kind: "primary", pulse: true },
  scanning: { label: "扫描中", kind: "primary", pulse: true },
  cancel_requested: { label: "取消中", kind: "warning", pulse: true },
  completed: { label: "已完成", kind: "success" },
  abandoned: { label: "执行中断", kind: "warning" },
  partial: { label: "部分完成", kind: "warning" },
  failed: { label: "失败", kind: "danger" },
  canceled: { label: "已取消", kind: "neutral" },
  timeout: { label: "超时", kind: "danger" },
};

const meta = computed(
  () => MAP[props.status] ?? { label: props.status, kind: "neutral" },
);
</script>

<template>
  <span class="badge" :class="meta.kind">
    <span class="dot" />
    {{ meta.label }}
  </span>
</template>

<style scoped>
.dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
  flex-shrink: 0;
}
</style>
