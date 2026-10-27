/** Browser/Vite module loading failures, separate from API and model errors. */
export function isPageLoadError(error: unknown): boolean {
  const text = error instanceof Error ? error.message :
    typeof error === 'string' ? error :
    error !== null && typeof error === 'object' && 'message' in error ? String(error.message) : '';
  return /Failed to fetch dynamically imported module|Importing a module script failed|error loading dynamically imported module|Loading (?:CSS )?chunk [\w-]+ failed|Unable to preload CSS for/i.test(text);
}

/** A recovery button may only open a page within this application. */
export function pageReloadTarget(target: string, currentURL: string): string {
  const current = new URL(currentURL);
  try {
    const next = new URL(target, current);
    if (next.origin === current.origin && /^https?:$/.test(next.protocol)) {
      return next.pathname + next.search + next.hash;
    }
  } catch { /* Preserve the current page when a target is invalid. */ }
  return current.pathname + current.search + current.hash;
}
