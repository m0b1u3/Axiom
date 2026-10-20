import {advancedScanInput,emptyAdvancedScanForm} from './advanced-scan-input';

/** Reuse ordinary scan prompt validation; a component cannot change its frozen scope. */
export function componentSDKInput(knowledgeVersionIds:number[],scanPrompt:string,postScanPrompt:string){
 const value=advancedScanInput({...emptyAdvancedScanForm(),scanPrompt,postScanPrompt},'standard','default');
 return {...(knowledgeVersionIds.length?{knowledgeVersionIds:[...knowledgeVersionIds]}:{}),
  ...(value?.scanPrompt?{scanPrompt:value.scanPrompt}:{}),
  ...(value?.postScanPrompt?{postScanPrompt:value.postScanPrompt}:{})};
}
