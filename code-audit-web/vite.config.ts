import { defineConfig, loadEnv } from "vite";
import vue from "@vitejs/plugin-vue";
import tailwindcss from "@tailwindcss/vite";
import monacoEditorPluginImport from "vite-plugin-monaco-editor";

// CJS/ESM 互操作
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const monacoEditorPlugin = ((monacoEditorPluginImport as any).default ??
  monacoEditorPluginImport) as (options?: { languageWorkers?: string[] }) => ReturnType<typeof vue>;

export default defineConfig(({ mode }) => ({
  plugins: [
    vue(),
    tailwindcss(),
    monacoEditorPlugin({ languageWorkers: ["editorWorkerService", "json", "css", "html", "typescript"] }),
  ],
  server: {
    host: true,
    port: 5173,
    proxy: {
      "/api": {
        target: loadEnv(mode, ".", "").AXIOM_API_TARGET || "http://127.0.0.1:8080",
        changeOrigin: true,
      },
    },
  },
}));
