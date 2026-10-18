import {computed, onUnmounted, ref, shallowRef, watch} from 'vue';
import {useRoute, useRouter} from 'vue-router';
import type {SortingState} from '@tanstack/vue-table';
import type {CatalogPage, CatalogQuery} from '../api';

// URL is the query source of truth: back/forward and a return from evidence
// restore the full server query. Only an accepted response changes visible rows
// and totals. Invalidated/late requests cannot overwrite newer filters.
export function useCatalog<T>(fetchPage: (query: CatalogQuery & Record<string, string | number>) => Promise<CatalogPage<T>>, fields: string[], defaultSort: string, defaultOrder: string) {
  const route = useRoute(), router = useRouter();
  const result = shallowRef<CatalogPage<T> | null>(null);
  const loading = ref(false), error = ref('');
  const value = (key: string) => String(route.query[key] ?? '');
  const search = ref(value('q'));
  let generation = 0, timer: ReturnType<typeof setTimeout> | undefined;
  const filters = computed(() => Object.fromEntries(fields.map(key => [key, value(key)])) as Record<string, string>);
  const sorting = computed<SortingState>(() => [{id:value('sort') || defaultSort, desc:(value('order') || defaultOrder) === 'desc'}]);
  function invalidate() { generation++; loading.value = true; error.value = ''; }
  async function load() {
    if (search.value.trim() !== value('q')) { await change({q:search.value.trim() || undefined,page:undefined}); return; }
    clearTimeout(timer);
    const current = ++generation;
    loading.value = true; error.value = '';
    const query: CatalogQuery & Record<string, string | number> = {page:Number(value('page')) || 1, pageSize:50, q:value('q'), sort:value('sort') || defaultSort, order:value('order') || defaultOrder};
    for (const key of fields) if (value(key) !== '') query[key] = value(key);
    try {
      const next = await fetchPage(query);
      if (current === generation) result.value = next;
    } catch (cause) {
      if (current === generation) error.value = (cause as Error).message;
    } finally { if (current === generation) loading.value = false; }
  }
  async function change(values: Record<string, string | number | undefined>) {
    clearTimeout(timer);
    // A select/pager action also commits any text still waiting for debounce.
    const query = {...route.query, q:search.value.trim() || undefined, ...values};
    invalidate();
    const target = router.resolve({path:route.path, query});
    if (target.fullPath === route.fullPath) await load();
    else await router.push({query});
  }
  function filter(key: string, next: string) { void change({[key]:next || undefined, page:undefined}); }
  function sort(next: SortingState) { void change({sort:next[0]?.id, order:next[0] ? (next[0].desc ? 'desc' : 'asc') : undefined, page:undefined}); }
  function page(next: number) { void change({page:next}); }
  watch(search, next => {
    if (next === value('q')) return;
    clearTimeout(timer); invalidate();
    timer = setTimeout(() => void change({q:next.trim() || undefined, page:undefined}), 250);
  });
  watch(() => route.fullPath, () => { search.value = value('q'); void load(); }, {immediate:true});
  onUnmounted(() => { generation++; clearTimeout(timer); });
  return {result, loading, error, search, filters, sorting, load, filter, sort, page, change};
}
