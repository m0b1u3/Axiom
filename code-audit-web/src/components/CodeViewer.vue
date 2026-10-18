<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from "vue";
import { useTheme } from "../composables/theme";
const { isDark } = useTheme();
let monacoModule: typeof import("monaco-editor") | null = null;

/* Monaco 只读代码查看器（证据 / raw JSON）。
   monaco 体积大 → 动态 import，首次用到才加载 */
const props = withDefaults(
  defineProps<{
    code: string;
    language?: string;
    maxHeight?: number;
    startLine?: number;
  }>(),
  { language: "plaintext", maxHeight: 320, startLine: 1 },
);

const el = ref<HTMLDivElement | null>(null);
// eslint-disable-next-line @typescript-eslint/no-explicit-any
let editor: any = null;
const height = ref(120);
const loadError = ref("");
const loading = ref(false);
let disposed = false;

function langOf(): string {
  return props.language;
}

async function loadEditor(): Promise<void> {
  loading.value = true; loadError.value = "";
  try {
  const monaco = await import("monaco-editor");
  if (disposed || !el.value) return;
  monacoModule = monaco;
  const dark = document.documentElement.classList.contains("dark");
  const lines = props.code.split("\n").length;
  height.value = Math.min(Math.max(lines * 15 + 12, 60), props.maxHeight);
  editor = monaco.editor.create(el.value, {
    value: props.code,
    language: langOf(),
    readOnly: true,
    theme: dark ? "vs-dark" : "vs",
    minimap: { enabled: false },
    lineNumbers: (line: number) => String(props.startLine + line - 1),
    lineNumbersMinChars: 3,
    folding: false,
    scrollBeyondLastLine: false,
    renderLineHighlight: "none",
    overviewRulerLanes: 0,
    hideCursorInOverviewRuler: true,
    scrollbar: { vertical: "auto", horizontal: "auto", verticalScrollbarSize: 6, horizontalScrollbarSize: 6 },
    fontSize: 11,
    fontFamily: '"JetBrains Mono Variable", "JetBrains Mono", Consolas, monospace',
    padding: { top: 6, bottom: 6 },
    wordWrap: "on",
    automaticLayout: true,
    domReadOnly: true,
    contextmenu: false,
  });
  } catch (error) { if (!disposed) loadError.value = `代码查看器加载失败：${(error as Error).message}。下方保留原始文本。`; }
  finally { if (!disposed) loading.value = false; }
}
onMounted(loadEditor);

watch(
  () => props.code,
  (v) => {
    if (editor && editor.getValue() !== v) {
      editor.setValue(v);
      const lines = v.split("\n").length;
      height.value = Math.min(Math.max(lines * 15 + 12, 60), props.maxHeight);
    }
  },
);

watch(isDark, dark => monacoModule?.editor.setTheme(dark ? "vs-dark" : "vs"));
watch(() => props.language, language => { if (editor && monacoModule) monacoModule.editor.setModelLanguage(editor.getModel(), language); });

onUnmounted(() => {
  disposed = true;
  const model = editor?.getModel();
  editor?.dispose();
  model?.dispose();
  editor = null;
});
</script>

<template>
  <div class="code-viewer" :style="{ height: loadError ? undefined : height + 'px' }">
    <p v-if="loading" role="status">正在加载代码查看器…</p>
    <p v-if="loadError" role="alert">{{loadError}}</p>
    <pre v-if="loadError" tabindex="0" aria-label="原始代码文本">{{code}}</pre>
    <div v-show="!loadError" ref="el" class="cv-el" />
  </div>
</template>

<style scoped>
.code-viewer {
  border: 1px solid var(--border-default);

  border-radius: var(--radius-xs);
  overflow: hidden;
}
.cv-el {
  width: 100%;
  height: 100%;
}
.code-viewer p{margin:var(--space-2);font-size:var(--font-size-xs);color:var(--text-secondary)}
.code-viewer pre{margin:0;padding:var(--space-3);max-height:320px;overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere;font:var(--font-size-xs)/var(--line-height-body) var(--font-mono)}
</style>
