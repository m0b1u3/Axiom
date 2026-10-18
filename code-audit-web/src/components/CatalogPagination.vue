<script setup lang="ts">
import {computed, ref, watch} from 'vue';
import Button from './ui/Button.vue';
import Input from './ui/Input.vue';
const props = defineProps<{label:string; page?:number; pageSize?:number; total?:number; loading?:boolean; unavailable?:boolean}>();
const emit = defineEmits<{change:[page:number]}>();
const last = computed(() => Math.max(1, Math.ceil((props.total ?? 0)/(props.pageSize ?? 50))));
const destination = ref('');
watch(() => props.page, page => { destination.value = String(page ?? 1); }, {immediate:true});
const valid = computed(() => Number.isSafeInteger(Number(destination.value)) && Number(destination.value) >= 1 && Number(destination.value) <= last.value);
function jump() { if (valid.value && !props.loading && props.total !== undefined) emit('change', Number(destination.value)); }
</script>
<template>
  <nav class="catalog-pagination" :aria-label="label" :aria-busy="loading">
    <span role="status">{{unavailable ? (total === undefined ? '尚未读取：' : '上次成功结果：') : ''}}共 {{total ?? '—'}} 条 · 第 {{page ?? '—'}} / {{total === undefined ? '—' : last}} 页<span v-if="loading"> · 正在查询…</span></span>
    <div class="page-controls">
      <Button variant="outline" size="sm" :disabled="loading || !page || page <= 1" @click="emit('change', 1)">首页</Button>
      <Button variant="outline" size="sm" :disabled="loading || !page || page <= 1" @click="emit('change', page! - 1)">上一页</Button>
      <form class="page-jump" @submit.prevent="jump">
        <Input v-model="destination" type="number" :min="1" :max="last" :aria-label="label + '页码'" :disabled="loading || total === undefined" />
        <Button type="submit" variant="ghost" size="sm" :disabled="loading || total === undefined || !valid">跳转</Button>
      </form>
      <Button variant="outline" size="sm" :disabled="loading || !page || page >= last" @click="emit('change', page! + 1)">下一页</Button>
      <Button variant="outline" size="sm" :disabled="loading || !page || page >= last" @click="emit('change', last)">末页</Button>
    </div>
  </nav>
</template>
<style scoped>
.catalog-pagination{display:flex;justify-content:space-between;align-items:center;gap:var(--space-3);padding-block:var(--space-3);font-size:var(--font-size-xs);color:var(--text-secondary);flex-wrap:wrap}
.page-controls,.page-jump{display:flex;align-items:center;gap:var(--space-1)}.page-jump input{width:calc(var(--space-8) * 2)}
@media(max-width:760px){.page-controls{flex-wrap:wrap}.page-jump{order:1}.catalog-pagination{align-items:flex-start}}
</style>
