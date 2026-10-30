import type {CreateScanInput} from "../api";

export function scopePaths(text: string): string[] {
  // One path per line: commas are legal filename characters.
  return text.split(/\r?\n/).map(value=>value.trim()).filter(Boolean);
}

export function optionalBudget(text: string): number | null {
  try{return optionalTotalBudget(text)}
  catch{throw new Error("单次上限预算须为最多六位小数的正数，最高 9007199254.740991 USD；不设置预算时请留空。")}
}

export function optionalTotalBudget(text: string): number | null {
  const trimmed=String(text).trim();if(!trimmed)return null;
  const value=Number(trimmed),match=/^\+?(\d*)(?:\.(\d*))?(?:e([+-]?\d+))?$/i.exec(trimmed);
  const invalid=()=>new Error('任务总预算须为最多六位小数的正数，最高 9007199254.740991 USD。');
  if(!match||!Number.isFinite(value)||value<=0)throw invalid();
  const digits=BigInt((match[1]||'0')+(match[2]||'')),scale=(match[2]?.length??0)-Number(match[3]??0);
  const divisor=scale>6?10n**BigInt(scale-6):1n;
  if(digits%divisor!==0n)throw invalid();
  const units=scale>6?digits/divisor:digits*10n**BigInt(6-scale);
  if(units<1n||units>9007199254740991n || Number(units)/1e6!==value)throw invalid();
  return value;
}

export function optionalWorkers(text: string): number | undefined {
  if(String(text).trim()==="")return undefined;
  const value=Number(text);
  if(!Number.isSafeInteger(value)||value<1)throw new Error("Worker 数量须为正安全整数（最高 9007199254740991），或留空使用 SDK 默认值。");
  return value;
}

export interface ScanFormInput {
  snapshotId:number;
  modelConfigId:number;
  projectId:number|null;
  mode:string;
  timeoutMinutes:number;
  budget:string;
  totalBudget?:string;
  workers:string;
  includeText:string;
  focus:string[];
  validationMode:"default"|"exploitability";
  businessContext:string;
  deepMaxDiscoveryRuns:string;
  deepStopAfterNoNew:string;
}

export function scanInput(form: ScanFormInput): CreateScanInput {
  if(!Number.isSafeInteger(form.snapshotId)||form.snapshotId<1)throw new Error("请上传源码包或选择已有代码快照。");
  if(!Number.isSafeInteger(form.modelConfigId)||form.modelConfigId<1)throw new Error("请选择模型配置。");
  if(!Number.isInteger(form.timeoutMinutes)||form.timeoutMinutes<1||form.timeoutMinutes>600)throw new Error("扫描时限须为 1–600 分钟的整数。");
  const include=scopePaths(form.includeText);
  const canonical=new Set<string>();
  for(const value of include) {
    if(value.includes("\0")||value.startsWith("/")||value.startsWith("\\")||/^[A-Za-z]:/.test(value)||value.replaceAll("\\","/").split("/").some(part=>part==='..'||part==='.'))throw new Error("扫描范围必须是源码内的相对路径，不能包含 . 或 ..。");
    const key=value.replaceAll("\\","/").replace(/\/+/g,"/").replace(/\/$/,"");
    if(canonical.has(key))throw new Error("扫描范围包含重复路径。");
    canonical.add(key);
  }
  const input:CreateScanInput={sourceSnapshotId:form.snapshotId,modelConfigId:form.modelConfigId,scanMode:form.mode,timeoutMinutes:form.timeoutMinutes,
    maxCostUsd:optionalBudget(form.budget),includePaths:include,scanFocus:[...form.focus],validationMode:form.validationMode,
    ...(form.validationMode==='exploitability'?{businessContext:form.businessContext}:{})};
  if(String(form.totalBudget??'').trim()){
    const total=optionalTotalBudget(String(form.totalBudget))!;
    if(input.maxCostUsd!=null){try{optionalTotalBudget(form.budget);}catch{throw new Error('使用累计预算时，单次上限也须为最多六位小数的正数。')}}
    input.maxTotalCostUsd=total;
  }
  if(form.projectId!==null)input.projectId=form.projectId;
  if(form.mode==='deep'){
    const workers=optionalWorkers(form.workers);
    if(workers!==undefined)input.workers=workers;
    for(const [key,text] of [['deepMaxDiscoveryRuns',form.deepMaxDiscoveryRuns],['deepStopAfterNoNew',form.deepStopAfterNoNew]] as const){
      if(String(text).trim()==='')continue;
      const value=Number(text);
      if(!Number.isSafeInteger(value)||value<1)throw new Error("深度扫描次数须为正安全整数（最高 9007199254740991），或留空使用 SDK 默认值。");
      input[key]=value;
    }
  }
  return input;
}
