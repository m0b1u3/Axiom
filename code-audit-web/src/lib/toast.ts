import { toast } from "vue-sonner";

/* 统一消息提示入口（替代 ElMessage） */
export const message = {
  success: (m: string) => toast.success(m),
  error: (m: string) => toast.error(m),
  warning: (m: string) => toast.warning(m),
  info: (m: string) => toast.info(m),
};
