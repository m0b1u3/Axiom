<script setup lang="ts">
import { shortPath } from "../utils/format";
import Tooltip from "./ui/Tooltip.vue";

defineProps<{ path: string }>();
</script>

<template>
  <Tooltip :content="path">
    <span class="sp">{{ shortPath(path) }}</span>
  </Tooltip>
</template>

<style scoped>
.sp {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--app-text-2);
  cursor: default;
  user-select: none;
}
</style>
