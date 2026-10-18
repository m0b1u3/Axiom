import {lstat,readFile,realpath} from 'node:fs/promises';
import {createHash} from 'node:crypto';
import path from 'node:path';
import {validate,SCAN_INPUT_POLICY} from '@axiom/contracts';
import type {ScanOptions} from '@openai/codex-security';
import type {ScanTask} from './scan-task.js';

export async function sdkScanInputOptions(task:ScanTask):Promise<Pick<ScanOptions,'knowledgeBasePaths'|'subagents'|'scanPrompt'|'validationPrompt'|'postScanPrompt'|'workflowId'|'safetyIdentifier'|'parentScanId'|'expectedPluginVersion'|'failureSeverity'>> {
 const input=task.scanInput;if(!input)return {};
 validate('ScanInput',input);
 if(input.subagents!==undefined&&task.scanMode!=='deep')throw new Error('Subagents require Deep mode');
 if(input.validationPrompt!==undefined&&(task.scanMode==='deep'||task.validationMode==='exploitability'))throw new Error('Custom validation requires Standard default validation');
 const names:string[]=[],seen=new Set<string>();
 for(const doc of input.knowledge){
  if(seen.has(doc.versionId))throw new Error('Duplicate knowledge document version');seen.add(doc.versionId);
  if(Buffer.byteLength(doc.content)>SCAN_INPUT_POLICY.knowledgeDocumentMaxBytes)throw new Error('Knowledge document exceeds byte limit');
  const root=await realpath(task.outputPath),directory=path.join(root,'runtime','knowledge');
  const actualDirectory=await realpath(directory);if(actualDirectory!==directory)throw new Error('Knowledge directory must not be a symlink');
  const name=path.join(directory,`${doc.versionId}.md`),stat=await lstat(name);
  if(!stat.isFile()||stat.isSymbolicLink()||stat.size>SCAN_INPUT_POLICY.knowledgeDocumentMaxBytes)throw new Error('Knowledge document is not a regular bounded file');
  const data=await readFile(name);if(data.toString('utf8')!==doc.content||createHash('sha256').update(data).digest('hex')!==doc.sha256)throw new Error('Knowledge content changed before SDK execution');
  names.push(name);
 }
 const {schemaVersion,knowledge,...options}=input;
 return {...options,...(names.length?{knowledgeBasePaths:names}:{})};
}
