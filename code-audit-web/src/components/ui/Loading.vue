<script setup lang="ts">
import { useSlots } from 'vue';
withDefaults(defineProps<{ visible?: boolean; text?: string }>(), { visible: true, text: "加载中…" });
const slots = useSlots();
</script>

<template>
  <!-- v-loading 替代：包一层相对定位容器 -->
  <div class="relative" :aria-busy="visible">
    <slot />
    <div
      v-if="visible"
      role="status" aria-live="polite"
      :class="slots.default ? 'absolute inset-0 z-10 flex items-center justify-center gap-2 bg-background/60 text-xs text-muted-foreground' : 'flex items-center gap-2 py-4 text-xs text-muted-foreground'"
    >
      <svg aria-hidden="true" class="animate-spin" width="16" height="16" viewBox="0 0 24 24" fill="none">
        <circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="3" opacity="0.25" />
        <path d="M22 12a10 10 0 0 0-10-10" stroke="currentColor" stroke-width="3" stroke-linecap="round" />
      </svg>
      {{ text }}
    </div>
  </div>
</template>
