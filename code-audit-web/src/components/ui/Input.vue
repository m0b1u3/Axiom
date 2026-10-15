<script setup lang="ts">
import { cn } from "../../lib/utils";

const props = withDefaults(
  defineProps<{
    type?: string;
    placeholder?: string;
    autocomplete?: string;
    clearable?: boolean;
    class?: string;
  }>(),
  { type: "text" },
);
const model = defineModel<string>({ default: "" });
const emit = defineEmits<{ enter: [] }>();
</script>

<template>
  <input
    v-model="model"
    :type="props.type"
    :placeholder="props.placeholder"
    :autocomplete="props.autocomplete"
    @keyup.enter="emit('enter')"
    :class="
      cn(
        'axiom-input flex h-8 w-full rounded-md border border-input bg-background px-2.5 text-[13px] text-foreground transition-colors placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-50',
        props.class,
      )
    "
  />
</template>

<style scoped>
.axiom-input:focus-visible {
  outline: none;
  border-color: var(--border-focus);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--border-focus) 20%, transparent);
}
</style>
