import type {ScanEventV2} from '../contracts';
import type { Progress, ScanLogEvent } from '../types';
export const PHASE_LABELS: Record<string,string> = {ranking:'风险排序',file_review:'文件审查',preparing:'准备环境',preflight:'环境预检',threat_model:'威胁建模',discovery:'发现风险',validation:'验证发现',attack_path:'分析攻击路径',reporting:'整理结果',processing:'处理结果',partial:'部分完成',completed:'扫描完成',failed:'扫描失败',canceled:'已取消',timeout:'扫描超时',unknown:'阶段未知'};
export function phaseLabel(value:string):string { return PHASE_LABELS[value] || value || '准备中'; }
export type ActivityEntry = ScanLogEvent & {category:'progress'|'command'|'message'|'system'|'detail'; label:string; summary:string; failed:boolean; warning:boolean; recovered?:boolean};
export function activityEntry(e:ScanLogEvent):ActivityEntry {
 if(e.event)return structuredEntry(e);
 const raw=e.message||'';
 const parsed=/^(running|completed|failed)\s+(command|tool|message|reasoning)\s+([\s\S]*)$/i.exec(raw);
 const failed=e.level==='error'||parsed?.[1]==='failed'||raw==='scan failed';
 const warning=e.level==='warn'||e.kind==='warning';
 let category:ActivityEntry['category']='detail',label='详情',summary=raw;
 if(e.kind==='system'){category='system';label='系统';summary=({'scan started':'扫描开始','scan completed':'扫描完成','scan failed':'扫描失败','scan canceled':'扫描已取消'} as Record<string,string>)[raw]||raw;}
 else if(e.kind==='progress'){category='progress';label='阶段';const phase=/phase=(\S+)/.exec(raw)?.[1];const files=/files=(\S+)/.exec(raw)?.[1];summary=phase?[phaseLabel(phase),files?(/^0\//.test(files)?`共 ${files.split('/')[1]} 个文件 · 完成数量尚未上报`:`已处理 ${files} 文件`):null].filter(Boolean).join(' · '):raw;}
 else if(parsed){const [,status,kind,body]=parsed;category=kind==='command'||kind==='tool'?'command':kind==='message'?'message':'detail';label=category==='command'?(status==='running'?'执行中':failed?'调用失败':'已执行'):category==='message'?'进展':'详情';summary=body;if(kind==='tool'&&body.trim()==='update_codex_security_scan_progress')summary=failed?'进度上报失败':'更新扫描进度';}
 else if(e.kind==='worker'){category='progress';label='工作线程';}
 else if(e.kind==='session'){label='会话';summary=raw.replace(/^.*?\[(reasoning|message|system)\]\s*/,'');}
 if(warning)label='警告';if(failed)label='失败';
 if(summary.includes('会话开始')) summary=summary.replace('会话开始','执行轮次开始（不代表重新扫描）');
 return {...e,category,label,summary,failed,warning};
}
export function isImportant(e:ActivityEntry):boolean {return e.failed||e.warning||e.recovered===true||['system','progress','message'].includes(e.category);}
export function mergeLogEvents(old:ScanLogEvent[],incoming:ScanLogEvent[],limit=1500):ScanLogEvent[]{if(!incoming.length)return old;const key=(e:ScanLogEvent)=>e.event?`${e.event.execution.taskId}:${e.event.execution.attempt}:${e.event.sequence}`:`legacy:${e.n}`;const byN=new Map(old.map(e=>[key(e),e]));for(const e of incoming)if(!byN.has(key(e)))byN.set(key(e),e);return [...byN.values()].sort((a,b)=>a.n-b.n).slice(-limit);}
export function localLogTime(value:string):string {const date=new Date(value);return Number.isNaN(date.getTime())?value:new Intl.DateTimeFormat('zh-CN',{hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false}).format(date);}

export function progressEntries(events:ScanLogEvent[], reportedAt?:string):ActivityEntry[] {
 const successTimes=events.filter(e=>!e.event).filter(e=>/^completed tool (?:\S*__)?update_codex_security_scan_progress\s*$/.test(e.message)).map(e=>Date.parse(e.timestamp));
 const recoveredAt=Math.max(Date.parse(reportedAt||'')||0,...successTimes);
 return events.map(activityEntry).map(e=>{
  if(!e.event && e.failed && /failed tool (?:\S*__)?update_codex_security_scan_progress\s*$/.test(e.message) && Date.parse(e.timestamp)<recoveredAt)
   return {...e,failed:false,warning:false,recovered:true,label:'已恢复',summary:'进度上报曾失败，后续已成功上报'};
  return e;
 });
}
export function fileProgressLabel(p:Progress|null):string {
 if(p?.snapshot){const v=p.snapshot.files;if(!v)return '文件进度尚未上报';return `${v.basis==='reported'?'SDK 上报':'观察到范围'} ${v.completed??'—'} / ${v.total??'—'} 文件 · 未核验覆盖`;}
 if(!p?.filesTotal)return '';
 if(typeof p.filesCompleted==='number' && p.filesCompleted>0) return `扫描器上报 ${p.filesCompleted} / ${p.filesTotal} 文件 · 未核验覆盖`;
 return `共 ${p.filesTotal} 个文件 · 完成数量尚未上报`;
}
export function phaseProgressLabel(p:Progress|null):string {
 const v=p?.phaseProgress;if(!v||v.total==null||!v.unit)return '';
 const units:Record<string,string>={checks:'项预检',threat_surfaces:'个攻击面',review_receipts:'项审查回执',candidate_findings:'项候选发现',validated_findings:'项验证发现',report_artifacts:'项报告产物'};
 return `${v.completed??'—'} / ${v.total} ${units[v.unit]||v.unit}`;
}

function structuredEntry(e:ScanLogEvent):ActivityEntry {
 const event=e.event!;let category:ActivityEntry['category']='detail',label='详情',summary=event.message;
 let failed=event.level==='error',warning=event.level==='warn';
 switch(event.type){
 case 'scan.phase_changed':category='progress';label='阶段';summary=phaseLabel(event.data.phase)+(event.data.rawPhase?` · ${event.data.rawPhase}`:'');break;
 case 'scan.progress':category='progress';label='进度';summary=phaseLabel(event.data.phase)+(event.data.rawPhase?` · ${event.data.rawPhase}`:'');if(event.data.phaseCounts)summary+=` · ${event.data.phaseCounts.completed??'—'} / ${event.data.phaseCounts.total??'—'} ${event.data.phaseCounts.unit}`;if(event.data.deep)summary+=` · 完成 ${event.data.deep.completed??'—'} 次独立审查，活跃 ${event.data.deep.active??'—'} 个`;break;
 case 'scan.activity': {const v=event.data;category=v.kind==='tool'||v.kind==='command'?'command':v.kind==='message'?'message':'detail';label=v.status==='failed'?'调用失败':v.status==='running'?'执行中':v.kind==='reasoning'?'推理记录':'已执行';failed=v.status==='failed';summary=v.description;break;}
 case 'scan.worker':category='progress';label='线程';summary=`${workerLabel(event.data.worker)} · ${workerStates[event.data.status]??event.data.status}${event.data.activity?` · ${event.data.activity.description}`:''}`;failed=event.data.status==='failed';break;
 case 'scan.worker_status':category='progress';label='调度';summary=event.data.kind==='preflight'?`Worker 预检 · ${event.data.delegation??'unknown'} · 配置槽位 ${event.data.configuredSlots??'—'}`:`${phaseLabel(event.data.phase??'')} · 已启动 ${event.data.started} / 计划 ${event.data.planned}`;break;
 case 'scan.cost':label='费用';summary=`估算费用 ${event.data.estimatedUsd===null?'未知':`$${event.data.estimatedUsd}`} · 输入 ${event.data.inputTokens??'—'} / 输出 ${event.data.outputTokens??'—'} Token`;break;
 case 'scan.usage':label='用量';summary=`已观测 Token · 输入 ${event.data.inputTokens??'—'} / 输出 ${event.data.outputTokens??'—'} / 总计 ${event.data.totalTokens??'—'}；费用独立核对`;break;
 case 'scan.reconnect':category='system';label='连接重试';summary=`${event.data.reason} · 第 ${event.data.attempt} / ${event.data.maximum} 次`;break;
 case 'scan.warning':label='警告';warning=true;summary=`${event.data.code} · ${event.data.message}`;break;
 case 'scan.error':label='错误';failed=true;summary=`${event.data.code} · ${event.data.origin} · ${event.data.message}`;break;
 case 'scan.finished':category='system';label='扫描器结束';summary=phaseLabel(event.data.status);if(event.data.error)summary+=` · ${event.data.error.code} · ${event.data.error.message}`;break;
 }
 return {...e,category,label,summary,failed,warning};
}
export const workerStates:Record<string,string>={running:'执行轮次运行中',waiting:'等待下一轮',completed:'已完成',failed:'失败',unknown:'状态未上报'};
export function workerLabel(worker:{threadId?:string;index?:number;componentIndex?:number}):string{return (worker.componentIndex!==undefined?`组件 ${worker.componentIndex+1} · `:'')+(worker.threadId?`${worker.index!==undefined?`Worker ${worker.index} · `:'线程 '}${worker.threadId}`:`Worker ${worker.index}`);}
