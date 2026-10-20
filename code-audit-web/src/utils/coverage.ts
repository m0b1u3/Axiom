export function coverageSections(doc:Record<string,unknown>){
 const definitions=[['surfaces','审查范围'],['explicitExclusions','明确排除项'],['deferred','延期验证'],['openQuestions','未解决问题'],['resolvedDeferred','已完成的审查任务']] as const;
 return definitions.map(([key,title])=>({key,title,available:Array.isArray(doc[key]),items:Array.isArray(doc[key])?doc[key] as unknown[]:[]}));
}
export function coverageLabel(doc:Record<string,unknown>|null):string{
 return ({complete:'完整',partial:'部分覆盖',unknown:'完整性未知'} as Record<string,string>)[String(doc?.completeness)]??(doc?.documentType==='codex-security.coverage'?'按审查记录核对覆盖范围':'完整性未知');
}
export function coverageText(value:unknown):string {return typeof value==='string'?value:JSON.stringify(value,null,2)??'—';}
