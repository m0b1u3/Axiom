<script setup lang="ts">
import {
  SelectRoot, SelectTrigger, SelectValue, SelectPortal, SelectContent,
  SelectViewport, SelectItem, SelectItemText, SelectItemIndicator, SelectIcon,
} from "reka-ui";
import { Check, ChevronDown, X } from "lucide-vue-next";
import { computed } from "vue";
import { cn } from "../../lib/utils";

export interface SelectOption {
  value: string;
  label: string;
}

const props = withDefaults(
  defineProps<{
    options: SelectOption[];
    placeholder?: string;
    ariaLabel?: string;
    clearable?: boolean;
    class?: string;
  }>(),
  { placeholder: "请选择" },
);
const model = defineModel<string>({ default: "" });
const emit = defineEmits<{ change: [value: string] }>();

/* Reka SelectItem 不允许空字符串 value（空串 = 清除选择）——防御性过滤 */
const safeOptions = computed(() => props.options.filter((o) => o.value !== ""));

function onUpdate(v: string): void {
  model.value = v;
  emit("change", v);
}
function clear(e: Event): void {
  e.stopPropagation();
  model.value = "";
  emit("change", "");
}
</script>

<template>
  <SelectRoot :disabled="safeOptions.length === 0" :modal="false" :model-value="model || undefined" @update:model-value="onUpdate">
    <SelectTrigger
      :aria-label="props.ariaLabel || props.placeholder"
      :class="
        cn(
          'group flex h-8 w-full items-center justify-between gap-2 rounded-md border border-input bg-background px-2.5 text-[13px] text-foreground transition-colors focus:outline-none focus:ring-2 focus:ring-ring/40 data-[placeholder]:text-muted-foreground',
          props.class,
        )
      "
    >
      <SelectValue :placeholder="props.placeholder" class="truncate" />
      <span class="flex items-center gap-1">
        <button
          v-if="props.clearable && model"
          class="rounded p-0.5 text-muted-foreground hover:text-foreground"
          @click="clear"
        >
          <X :size="12" />
        </button>
        <SelectIcon><ChevronDown :size="14" class="text-muted-foreground" /></SelectIcon>
      </span>
    </SelectTrigger>
    <SelectPortal>
      <SelectContent v-if="safeOptions.length"
        position="popper"
        :side-offset="4"
        class="z-[200] max-h-64 min-w-[var(--reka-select-trigger-width)] overflow-auto rounded-md border border-border bg-popover p-1 shadow-[var(--shadow-popover)]"
      >
        <SelectViewport>
          <SelectItem
            v-for="o in safeOptions"
            :key="o.value"
            :value="o.value"
            class="relative flex cursor-pointer select-none items-center rounded-sm py-1.5 pl-2 pr-7 text-[13px] text-foreground outline-none data-[highlighted]:bg-accent data-[state=checked]:font-medium"
          >
            <SelectItemText>{{ o.label }}</SelectItemText>
            <SelectItemIndicator class="absolute right-1.5"><Check :size="13" /></SelectItemIndicator>
          </SelectItem>
        </SelectViewport>
      </SelectContent>
    </SelectPortal>
  </SelectRoot>
</template>
