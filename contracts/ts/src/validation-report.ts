/** Structural completeness only. This never proves a claim or invents a verdict.
 * No word/character minimum: a concise rationale is allowed. */
export type ValidationReportBodyState = 'body_present' | 'empty' | 'title_only';
export function validationReportBodyState(report: string): ValidationReportBodyState {
  const trim = (s:string) => s.replace(/^[\s\u0085]+|[\s\u0085]+$/g, '');
  const lines = report.replace(/<!--[\s\S]*?-->/g, '').replace(/\r\n?/g, '\n').split('\n');
  let title = false;
  for (let i = 0; i < lines.length; i++) {
    const line = trim(trim(lines[i]!).replace(/^(?:>[ \t]*)+/, ''));
    if (!line || /^(?:`{3,}|~{3,})[^`~]*$/.test(line) || /^(?:[-*_][ \t]*){3,}$/.test(line)) continue;
    // Some retained providers flatten a full multi-section assessment onto
    // one line. Preserve that prose; this guard rejects obvious bare titles.
    if (/^#{1,6}(?:[ \t]+|$)/.test(line)) { if (/[ \t]#{2,6}[ \t]+\S/.test(line)) return 'body_present'; title = true; continue; }
    if (/^<h[1-6](?:[ \t][^>]*)?>.*<\/h[1-6]>$/i.test(line)) { title = true; continue; }
    if (i + 1 < lines.length && /^(?:=+|-+)[ \t]*$/.test(trim(lines[i + 1]!))) { title = true; i++; continue; }
    return 'body_present';
  }
  return title ? 'title_only' : 'empty';
}
