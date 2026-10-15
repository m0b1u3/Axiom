<script setup lang="ts">
import { TooltipRoot, TooltipTrigger, TooltipPortal, TooltipContent, TooltipArrow } from "reka-ui";

withDefaults(
  defineProps<{
    content: string;
    side?: "top" | "bottom" | "left" | "right";
    disabled?: boolean;
  }>(),
  { side: "top" },
);
</script>

<template>
  <TooltipRoot v-if="!disabled" :delay-duration="350">
    <TooltipTrigger as-child><slot /></TooltipTrigger>
    <TooltipPortal>
      <TooltipContent
        :side="side"
        :side-offset="5"
        class="z-[200] max-w-72 rounded-md border border-border bg-popover px-2 py-1 text-xs text-popover-foreground shadow-[var(--shadow-popover)]"
      >
        {{ content }}
        <TooltipArrow :width="10" :height="5" class="fill-popover" />
      </TooltipContent>
    </TooltipPortal>
  </TooltipRoot>
  <slot v-else />
</template>
