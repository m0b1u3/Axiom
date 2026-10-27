import { shallowRef } from 'vue';
import { isPageLoadError, pageReloadTarget } from '../utils/page-load-error';

export const failedPage = shallowRef<{path: string; title: string} | null>(null);

export function reportPageLoadError(error: unknown, path?: string, title?: string): boolean {
  if (!isPageLoadError(error)) return false;
  failedPage.value = {
    path: pageReloadTarget(path ?? failedPage.value?.path ?? window.location.href, window.location.href),
    title: title ?? failedPage.value?.title ?? '',
  };
  return true;
}

export function clearPageLoadError(): void { failedPage.value = null; }

export function reloadFailedPage(): void {
  // Only an explicit button click reloads; pending forms and tasks are not replayed.
  if (failedPage.value) window.location.assign(pageReloadTarget(failedPage.value.path, window.location.href));
}
