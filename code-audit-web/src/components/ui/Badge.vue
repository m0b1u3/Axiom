<script setup lang="ts">
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../../lib/utils";

const badgeVariants = cva(
  "inline-flex items-center gap-1 rounded-sm border px-1.5 py-px text-xs font-medium whitespace-nowrap",
  {
    variants: {
      variant: {
        default: "border-transparent bg-primary text-primary-foreground",
        secondary: "border-border bg-secondary text-secondary-foreground",
        outline: "border-border text-foreground",
        success: "border-transparent bg-[var(--state-success-bg)] text-[var(--state-success)]",
        warning: "border-transparent bg-[var(--state-warning-bg)] text-[var(--state-warning)]",
        danger: "border-transparent bg-[var(--state-danger-bg)] text-[var(--state-danger)]",
        info: "border-transparent bg-[var(--state-info-bg)] text-[var(--state-info)]",
      },
    },
    defaultVariants: { variant: "default" },
  },
);

type BadgeVariants = VariantProps<typeof badgeVariants>;

const props = withDefaults(defineProps<{ variant?: BadgeVariants["variant"]; class?: string }>(), {
  variant: "default",
});
</script>

<template>
  <span :class="cn(badgeVariants({ variant }), props.class)"><slot /></span>
</template>
