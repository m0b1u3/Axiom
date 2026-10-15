<script setup lang="ts">
withDefaults(defineProps<{ visible?: boolean; text?: string }>(), { text: "加载中…" });
</script>

<template>
  <!-- v-loading 替代：包一层相对定位容器 -->
  <div class="relative">
    <slot />
    <div
      v-if="visible"
      class="absolute inset-0 z-10 flex items-center justify-center gap-2 bg-background/60 text-xs text-muted-foreground"
    >
      <svg class="animate-spin" width="16" height="16" viewBox="0 0 24 24" fill="none">
        <circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="3" opacity="0.25" />
        <path d="M22 12a10 10 0 0 0-10-10" stroke="currentColor" stroke-width="3" stroke-linecap="round" />
      </svg>
      {{ text }}
    </div>
  </div>
</template>
