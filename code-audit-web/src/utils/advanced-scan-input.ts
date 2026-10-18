import type {ScanInput} from '../contracts';
export interface AdvancedScanForm {
 parentTaskId:string;subagents:string;scanPrompt:string;validationPrompt:string;postScanPrompt:string;workflowId:string;
 safetyIdentifier:string;parentScanId:string;expectedPluginVersion:string;failureSeverity:string;
}
export function emptyAdvancedScanForm():AdvancedScanForm{return {parentTaskId:'',subagents:'',scanPrompt:'',validationPrompt:'',postScanPrompt:'',workflowId:'',safetyIdentifier:'',parentScanId:'',expectedPluginVersion:'',failureSeverity:''};}
export function advancedScanInput(form:AdvancedScanForm,mode:string,validation:string):ScanInput|undefined {
 if(String(form.parentTaskId??'').trim()&&(!Number.isSafeInteger(Number(form.parentTaskId))||Number(form.parentTaskId)<1))throw new Error('父扫描任务编号须为正整数。');
 const out:ScanInput={schemaVersion:1,knowledge:[]};
 for(const key of ['scanPrompt','validationPrompt','postScanPrompt','workflowId','safetyIdentifier','parentScanId','expectedPluginVersion'] as const){
  const value=form[key].trim();if(!value)continue;
  const max=key==='safetyIdentifier'?64:['scanPrompt','validationPrompt','postScanPrompt'].includes(key)?16000:128;
  if([...value].length>max||value.includes('\0'))throw new Error(`扫描参数 ${key} 超过长度限制或包含无效字符。`);
  out[key]=value;
 }
 if(String(form.subagents).trim()){
  const n=Number(form.subagents);if(mode!=='deep'||!Number.isSafeInteger(n)||n<0||n>64)throw new Error('子代理数量须为 0–64 的整数，且仅适用于 Deep。');out.subagents=n;
 }
 if(out.validationPrompt&&(mode!=='standard'||validation==='exploitability'))throw new Error('自定义验证指令仅适用于 Standard 默认验证，请清空该指令或切换验证方式。');
 for(const key of ['workflowId','safetyIdentifier'] as const)if(out[key]&&!/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(out[key]!))throw new Error('工作流和隐私标识只能包含英文字母、数字、点、下划线和连字符。');
 if(out.expectedPluginVersion&&!/^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?$/.test(out.expectedPluginVersion))throw new Error('预期插件版本须使用版本号，例如 0.1.94。');
 if(form.failureSeverity){if(!['critical','high','medium','low','informational'].includes(form.failureSeverity))throw new Error('SDK 严重程度阈值不受支持。');out.failureSeverity=form.failureSeverity as ScanInput['failureSeverity'];}
 return Object.keys(out).length>2?out:undefined;
}
