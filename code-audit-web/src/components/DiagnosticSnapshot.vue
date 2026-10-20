<script setup lang="ts">
import {computed, ref, useId, watch} from 'vue';
import {ChevronRight} from 'lucide-vue-next';
import Button from './ui/Button.vue';

const props = defineProps<{title: string; value: unknown; filename: string}>();
const id = useId();
const expanded = ref(false), wrap = ref(true), copying = ref(false), feedback = ref('');
const raw = computed(() => JSON.stringify(props.value, null, 2) ?? '');
const lines = computed(() => raw.value.split('\n').length.toLocaleString());
const size = computed(() => {
  const bytes = new Blob([raw.value]).size;
  return bytes < 1024 ? `${bytes} B` : `${(bytes / 1024).toLocaleString(undefined, {maximumFractionDigits: 1})} KB`;
});
watch(raw, () => {feedback.value = '';});
function toggle(event: Event) {expanded.value = (event.target as HTMLDetailsElement).open;}
async function copy() {
  const value = raw.value;
  copying.value = true;
  feedback.value = '';
  try {
    await navigator.clipboard.writeText(value);
    if (raw.value === value) feedback.value = '已复制完整 JSON。';
  } catch {
    if (raw.value === value) feedback.value = '无法访问剪贴板，请在原始数据中选取文本，或下载 JSON。';
  } finally {copying.value = false;}
}
function download() {
  try {
    const url = URL.createObjectURL(new Blob([raw.value], {type: 'application/json;charset=utf-8'}));
    try {
      const link = document.createElement('a');
      link.href = url;
      link.download = props.filename;
      link.click();
      feedback.value = '';
    } finally {setTimeout(() => URL.revokeObjectURL(url), 0);}
  } catch {feedback.value = '下载未能开始，请复制或选取原始数据。';}
}
</script>

<template>
  <details class="diagnostic-snapshot" @toggle="toggle">
    <summary :aria-controls="id">
      <ChevronRight class="snapshot-chevron" :size="14" aria-hidden="true"/>
      <span class="snapshot-title">{{title}}</span>
      <span class="snapshot-meta">JSON · {{lines}} 行 · {{size}}</span>
    </summary>
    <div v-show="expanded" :id="id" class="snapshot-content">
      <template v-if="expanded">
      <div class="snapshot-toolbar" role="group" :aria-label="title+'阅读操作'">
        <span class="snapshot-format">完整原始数据</span>
        <div class="snapshot-actions">
          <Button variant="ghost" size="sm" :aria-pressed="wrap" :aria-label="title+'自动换行'" @click="wrap=!wrap">自动换行</Button>
          <Button variant="ghost" size="sm" :loading="copying" :aria-label="'复制'+title" @click="copy">复制 JSON</Button>
          <Button variant="ghost" size="sm" :aria-label="'下载'+title" @click="download">下载</Button>
        </div>
      </div>
      <pre tabindex="0" :aria-label="title+' JSON'" :class="{'snapshot-wrap':wrap}"><code>{{raw}}</code></pre>
      <p class="snapshot-feedback" role="status">{{feedback}}</p>
      </template>
    </div>
  </details>
</template>

<style scoped>
.diagnostic-snapshot{min-width:0;max-width:100%;border-block:1px solid var(--border-default);font-size:var(--font-size-sm)}
summary{display:flex;align-items:center;gap:var(--space-2);min-height:var(--height-lg);padding:var(--space-2) var(--space-3);list-style:none;cursor:pointer;color:var(--text-primary);background:var(--surface-secondary)}
summary::-webkit-details-marker{display:none}
summary:hover{background:var(--surface-hover)}
summary:focus-visible,pre:focus-visible{outline:2px solid var(--border-focus);outline-offset:-2px}
.snapshot-chevron{flex:none;color:var(--text-secondary);transition:transform var(--duration-fast) var(--ease-ui)}
details[open] .snapshot-chevron{transform:rotate(90deg)}
.snapshot-title{font-weight:var(--font-weight-medium);min-width:0;overflow-wrap:anywhere}
.snapshot-meta{margin-left:auto;flex-shrink:0;font-size:var(--font-size-xs);color:var(--text-secondary);font-variant-numeric:tabular-nums}
.snapshot-content{border-top:1px solid var(--border-default);background:var(--surface-primary)}
.snapshot-toolbar{display:flex;align-items:center;justify-content:space-between;gap:var(--space-2);flex-wrap:wrap;padding:var(--space-1) var(--space-3);border-bottom:1px solid var(--border-subtle)}
.snapshot-format{font-size:var(--font-size-xs);color:var(--text-secondary)}
.snapshot-actions{display:flex;align-items:center;gap:var(--space-1);margin-left:auto}
.snapshot-actions [aria-pressed=true]{color:var(--text-primary);background:var(--surface-selected)}
pre{box-sizing:border-box;max-height:calc(var(--height-table-row) * 8);overflow:auto;overscroll-behavior:contain;margin:0;padding:var(--space-3);font-family:var(--font-mono);font-size:var(--font-size-sm);line-height:var(--line-height-body);tab-size:2;white-space:pre;color:var(--text-primary)}
code{font:inherit}
pre.snapshot-wrap{white-space:pre-wrap;overflow-wrap:anywhere}
.snapshot-feedback{margin:0;padding:var(--space-2) var(--space-3);font-size:var(--font-size-xs);line-height:var(--line-height-body);color:var(--text-secondary);border-top:1px solid var(--border-subtle);overflow-wrap:anywhere}
.snapshot-feedback:empty{padding:0;border:0}
@media(max-width:600px){summary{flex-wrap:wrap;column-gap:var(--space-1-5)}.snapshot-meta{font-size:var(--font-size-xs)}.snapshot-toolbar{padding-inline:var(--space-2)}.snapshot-format{flex-basis:100%}.snapshot-actions{width:100%;justify-content:flex-end}pre{padding:var(--space-2)}}
@media(prefers-reduced-motion:reduce){.snapshot-chevron{transition:none}}
</style>
