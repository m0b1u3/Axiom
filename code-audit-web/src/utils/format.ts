/** 路径缩短：只留最后两段，其余用 … 收起 */
export function shortPath(p: string): string {
  if (!p) return "—";
  const segs = p.split(/[\\/]/).filter((s) => s.length > 0);
  if (segs.length <= 2) return p;
  return `…/${segs[segs.length - 2]}/${segs[segs.length - 1]}`;
}

/** 归档/内容哈希缩短：8 位 + … */
export function shortHash(h: string): string {
  if (!h) return "—";
  return h.slice(0, 8) + "…";
}
