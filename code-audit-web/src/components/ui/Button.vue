<script setup lang="ts">
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../../lib/utils";

const buttonVariants = cva(
  // border-transparent 统一占 1px 边框位：压掉浏览器 UA 按钮边框（本项目不加载 preflight），
  // 需要边框的 variant 只改 border-border 颜色，全站按钮尺寸恒定一致。
  "relative inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-md border border-transparent text-sm font-medium transition-colors cursor-pointer select-none disabled:pointer-events-none disabled:opacity-50 outline-none focus-visible:ring-2 focus-visible:ring-ring/40 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground hover:opacity-90",
        secondary: "bg-secondary text-secondary-foreground border-border hover:bg-accent",
        outline: "border-border bg-background hover:bg-accent",
        ghost: "bg-transparent hover:bg-accent",
        destructive: "bg-destructive text-destructive-foreground hover:opacity-90",
        link: "bg-transparent text-primary underline-offset-4 hover:underline h-auto px-0",
      },
      size: {
        default: "h-8 px-3 text-[13px]",
        sm: "h-7 px-2.5 text-xs",
        lg: "h-9 px-4",
        icon: "h-8 w-8",
        iconSm: "h-7 w-7",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);

type ButtonVariants = VariantProps<typeof buttonVariants>;

const props = withDefaults(
  defineProps<{
    variant?: ButtonVariants["variant"];
    size?: ButtonVariants["size"];
    loading?: boolean;
    disabled?: boolean;
    type?: "button" | "submit";
    class?: string;
  }>(),
  { variant: "default", size: "default", type: "button" },
);
</script>

<template>
  <button
    :type="props.type"
    :disabled="disabled || loading"
    :aria-busy="loading"
    :class="cn(buttonVariants({ variant, size }), props.class)"
  >
    <!-- loading 时隐藏内容但保留占位：按钮尺寸不变，spinner 绝对定位居中 -->
    <span class="btn-inner" :class="{ 'btn-inner--hidden': loading }"><slot /></span>
    <svg v-if="loading" class="btn-spinner" aria-hidden="true" width="14" height="14" viewBox="0 0 24 24" fill="none">
      <circle cx="12" cy="12" r="10" stroke="currentColor" stroke-width="3" opacity="0.25" />
      <path d="M22 12a10 10 0 0 0-10-10" stroke="currentColor" stroke-width="3" stroke-linecap="round" />
    </svg>
  </button>
</template>

<style scoped>
.btn-inner {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
}
.btn-inner--hidden {
  opacity: 0;
}
.btn-spinner {
  position: absolute;
  inset: 0;
  margin: auto;
  animation: button-progress-spin .8s linear infinite;
  transform-origin: center;
}
@keyframes button-progress-spin { to { transform: rotate(360deg); } }
@media(prefers-reduced-motion:reduce){.btn-spinner{animation:none}}
</style>
