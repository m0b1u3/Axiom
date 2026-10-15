<script setup lang="ts">
import {
  DropdownMenuRoot, DropdownMenuTrigger, DropdownMenuPortal,
  DropdownMenuContent, DropdownMenuItem,
} from "reka-ui";

export interface MenuItem {
  key: string;
  label: string;
  icon?: unknown;
}

const props = defineProps<{ items: MenuItem[] }>();
const emit = defineEmits<{ select: [key: string] }>();
</script>

<template>
  <DropdownMenuRoot :modal="false">
    <DropdownMenuTrigger as-child><slot /></DropdownMenuTrigger>
    <DropdownMenuPortal>
      <DropdownMenuContent
        align="end"
        :side-offset="4"
        class="z-[200] max-h-64 min-w-56 overflow-auto rounded-md border border-border bg-popover p-1 shadow-[var(--shadow-popover)]"
      >
        <DropdownMenuItem
          v-for="it in props.items"
          :key="it.key"
          class="flex cursor-pointer select-none items-center gap-2 rounded-sm px-2 py-1.5 text-[13px] text-foreground outline-none data-[highlighted]:bg-accent"
          @select="emit('select', it.key)"
        >
          <component :is="it.icon" v-if="it.icon" :size="13" />
          {{ it.label }}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenuPortal>
  </DropdownMenuRoot>
</template>
