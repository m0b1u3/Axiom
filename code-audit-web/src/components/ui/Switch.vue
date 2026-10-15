<script setup lang="ts">
/* 开关（Reka 无原生 Switch 风格需求，轻量自绘；尺寸固定，点击不变形） */
withDefaults(defineProps<{ checked: boolean; disabled?: boolean; size?: "sm" | "md" }>(), {
  size: "md",
});
const emit = defineEmits<{ change: [value: boolean] }>();
</script>

<template>
  <button
    type="button"
    role="switch"
    :aria-checked="checked"
    class="sw"
    :class="{ on: checked, sm: size === 'sm' }"
    :disabled="disabled"
    @click="emit('change', !checked)"
  >
    <span class="knob" />
  </button>
</template>

<style scoped>
.sw {
  position: relative;
  width: 36px;
  height: 20px;
  flex-shrink: 0;
  border-radius: 10px;
  border: 1px solid var(--border-default);
  background: var(--surface-secondary);
  cursor: pointer;
  padding: 0;
  transition: background-color var(--duration-fast) var(--ease-ui),
    border-color var(--duration-fast) var(--ease-ui);
}
.sw.sm {
  width: 30px;
  height: 17px;
}
.sw .knob {
  position: absolute;
  top: 2px;
  left: 2px;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: var(--text-tertiary);
  transition: transform var(--duration-fast) var(--ease-ui),
    background-color var(--duration-fast) var(--ease-ui);
}
.sw.sm .knob {
  width: 11px;
  height: 11px;
}
.sw.on {
  background: var(--accent-primary);
  border-color: var(--accent-primary);
}
.sw.on .knob {
  transform: translateX(16px);
  background: #fff;
}
.sw.sm.on .knob {
  transform: translateX(13px);
}
.sw:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
</style>
