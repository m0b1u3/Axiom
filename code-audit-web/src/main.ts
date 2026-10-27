import { createApp } from "vue";
import { createPinia } from "pinia";
import "@fontsource-variable/jetbrains-mono";
import "vue-sonner/style.css";
import "./styles/reset.css";
import "./styles/design-tokens.css";
import "./styles/tailwind.css";
import "./styles/tokens.css";
import "./styles/app.css";
import App from "./App.vue";
import { router } from "./router";
import { message } from "./lib/toast";
import { reportPageLoadError } from './composables/page-load-recovery';

import { applyTheme } from "./composables/theme";
applyTheme();

const app = createApp(App).use(createPinia()).use(router);

// 兜底：任何未被 catch 的错误一律走应用内弹窗（toast），
// 全站禁止浏览器原生 alert/confirm —— 这里兜住漏网之鱼。
app.config.errorHandler = (err) => {
  if (reportPageLoadError(err)) return;
  message.error(err instanceof Error ? err.message : String(err));
};
window.addEventListener("unhandledrejection", (event) => {
  event.preventDefault();
  const reason: unknown = event.reason;
  if (reportPageLoadError(reason)) return;
  message.error(reason instanceof Error ? reason.message : String(reason));
});

window.addEventListener("axiom:session-expired", () => {
  const current = router.currentRoute.value;
  if (current.name !== "login") void router.replace({ name: "login", query: { redirect: current.fullPath } });
});

app.mount("#app");
