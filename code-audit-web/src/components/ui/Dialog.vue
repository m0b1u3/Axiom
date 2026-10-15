<script setup lang="ts">
import {
  DialogRoot, DialogPortal, DialogOverlay, DialogContent, DialogTitle, DialogClose, DialogDescription,
} from "reka-ui";
import { X } from "lucide-vue-next";

withDefaults(
  defineProps<{
    title?: string;
    description?: string;
    width?: string;
  }>(),
  { width: "440px" },
);
const open = defineModel<boolean>("open", { default: false });

/* 下拉/选择/toast 都 portaled 到 body：点它们不算点弹窗外，否则选一项弹窗直接关 */
function ignorePortaledOverlay(event: Event): void {
  const detail = (event as Event & { detail?: { originalEvent?: Event } }).detail;
  const target = (detail?.originalEvent?.target ?? (event as PointerEvent).target) as Node | null;
  if (
    target instanceof Element &&
    target.closest('[role="menu"], [role="listbox"], [data-sonner-toaster]')
  ) {
    event.preventDefault();
  }
}
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="dialog-overlay fixed inset-0 z-[100]" />
      <DialogContent
        class="dialog-content fixed left-1/2 top-1/2 z-[101] -translate-x-1/2 -translate-y-1/2 rounded-[10px] border border-border bg-popover p-6 shadow-[var(--shadow-dialog)] focus:outline-none flex flex-col overflow-hidden"
        :style="{ width, maxWidth: 'calc(100vw - 32px)', maxHeight: 'calc(100vh - 48px)' }"
        @pointer-down-outside="ignorePortaledOverlay"
        @focus-outside="ignorePortaledOverlay"
        @interact-outside="ignorePortaledOverlay"
      >
        <div class="mb-4 shrink-0">
          <div class="flex items-center justify-between">
            <DialogTitle class="text-[15px] font-semibold text-foreground">{{ title }}</DialogTitle>
            <DialogClose aria-label="关闭弹窗" class="rounded-sm p-1 text-muted-foreground/70 hover:bg-accent hover:text-foreground">
              <X :size="15" :stroke-width="1.6" />
            </DialogClose>
          </div>
          <DialogDescription v-if="description" class="mt-1 text-[12px] leading-5 text-muted-foreground">{{ description }}</DialogDescription>
          <DialogDescription v-else class="sr-only">{{title}}</DialogDescription>
        </div>
        <div class="dlg-body">
          <slot />
        </div>
        <div v-if="$slots.footer" class="dialog-footer mt-6 flex justify-end gap-2 shrink-0">
          <slot name="footer" />
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>

<style scoped>
/* 内容超出可视高度时主体区滚动（负边距把滚动条贴到弹窗右缘，头部/底部固定不滚动） */
.dlg-body {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  /* 留出焦点环空间，避免首尾控件的描边被滚动容器裁切。 */
  margin: -4px -24px;
  padding: 4px 24px;
}
.dialog-overlay{background:rgba(13,22,39,.28);animation:overlay-appear 130ms ease-out}.dialog-content{animation:dialog-appear 160ms ease-out}.dialog-footer{border-top:1px solid var(--border-default);padding-top:var(--space-4)}
@keyframes overlay-appear{from{opacity:0}to{opacity:1}}@keyframes dialog-appear{from{opacity:.7;margin-top:4px}to{opacity:1;margin-top:0}}
@media(prefers-reduced-motion:reduce){.dialog-overlay,.dialog-content{animation:none}}
</style>
