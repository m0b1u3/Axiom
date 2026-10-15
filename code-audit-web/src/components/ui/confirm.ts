import { reactive } from "vue";

/* 全局确认框状态（替代 ElMessageBox.confirm）：
   const ok = await confirm({ title: "删除项目", body: "确定删除…？" }) */
interface ConfirmState {
  open: boolean;
  title: string;
  body: string;
  resolve: ((ok: boolean) => void) | null;
}

export const confirmState = reactive<ConfirmState>({
  open: false,
  title: "",
  body: "",
  resolve: null,
});

export function confirm(opts: { title: string; body: string }): Promise<boolean> {
  confirmState.title = opts.title;
  confirmState.body = opts.body;
  confirmState.open = true;
  return new Promise((resolve) => {
    confirmState.resolve = resolve;
  });
}

export function settleConfirm(ok: boolean): void {
  confirmState.open = false;
  confirmState.resolve?.(ok);
  confirmState.resolve = null;
}
