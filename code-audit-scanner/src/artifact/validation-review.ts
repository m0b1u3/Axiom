import {readFile,lstat,realpath} from 'node:fs/promises';
import path from 'node:path';
import type {ScanResult} from '../contract/scan-result.js';
import type {ScanTask} from '../contract/scan-task.js';
import {EXPLOITABILITY_PROMPT_VERSION} from '../contract/exploitability.js';
// Archive all candidates, including rejected/deferred ones, with existing execution coverage.
export async function attachValidationReview(task:ScanTask,result:ScanResult,rawDir:string):Promise<void>{
 if(task.validationMode!=='exploitability')return;
 async function read(name:string){try{const file=path.join(rawDir,'artifacts/custom-validation',name);const [base,resolved]=await Promise.all([realpath(rawDir),realpath(file)]);if(!resolved.startsWith(base+path.sep))return null;const stat=await lstat(file);if(!stat.isFile()||stat.isSymbolicLink()||stat.size>16*1024*1024)return null;return JSON.parse(await readFile(file,'utf8')) as unknown}catch{return null}}
 const [candidates,results]=await Promise.all([read('candidates.json'),read('results.json')]);
 result.coverage={...result.coverage,completeness:result.coverage?.completeness??'unknown',axiomValidationReview:{mode:'exploitability',promptVersion:EXPLOITABILITY_PROMPT_VERSION,businessContext:task.businessContext??'',contextSource:'user',candidates,results}};
}
