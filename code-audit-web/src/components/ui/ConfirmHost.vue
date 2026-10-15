<script setup lang="ts">
import Dialog from "./Dialog.vue";
import Button from "./Button.vue";
import { confirmState, settleConfirm } from "./confirm";
</script>

<template>
  <Dialog
    :open="confirmState.open"
    :title="confirmState.title"
    width="400px"
    @update:open="(v: boolean) => { if (!v) settleConfirm(false); }"
  >
    <p class="m-0 text-[13px] leading-relaxed text-foreground/80">{{ confirmState.body }}</p>
    <template #footer>
      <Button variant="secondary" @click="settleConfirm(false)">取消</Button>
      <Button variant="destructive" @click="settleConfirm(true)">确定</Button>
    </template>
  </Dialog>
</template>
