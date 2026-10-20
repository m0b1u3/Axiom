import {lstat,readFile,realpath} from 'node:fs/promises';
import path from 'node:path';
import {decode,COMPATIBILITY} from '@axiom/contracts';
import type {ScanTask} from '../contract/scan-task.js';

/** Load a separately negotiated extension; never reinterpret old strict jobs. */
export async function loadSDKParameters(file:string,task:ScanTask):Promise<void>{
 const stat=await lstat(file);
 if(!stat.isFile()||stat.isSymbolicLink()||stat.size>COMPATIBILITY.limits.documentBytes)throw new Error('Invalid SDK parameters file');
 const root=await realpath(task.outputPath),runtime=path.join(root,'runtime');
 if(await realpath(path.dirname(file))!==runtime||await realpath(file)!==path.join(runtime,'sdk-parameters.json'))throw new Error('SDK parameters must belong to this execution');
 const value=decode('SDKParametersAssignment',await readFile(file),{taskId:String(task.taskId),attempt:task.executionAttempt??1});
 if(value.parameters.componentInput&&!task.componentScan)throw new Error('Component SDK input is not bound to a component task');
 task.sdkParameters=value;
}
