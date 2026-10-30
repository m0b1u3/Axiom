import type {ScanOptions} from '@openai/codex-security';
import type {ScanTask} from './scan-task.js';
import {sdkScanInputOptions} from './sdk-scan-input.js';
import {deepScanOptions} from './deep-policy.js';
import {exploitabilityOptions} from './exploitability.js';
import {buildScanPrompt} from './scan-prompt.js';
import {sdkScanTarget} from '../runtime/git-target.js';
import {selectedSDKAuth} from '../runtime/sdk-identity.js';
import {nativeOpenAI} from '../provider/model-provider.js';
import {tokenBudget} from '../runtime/token-budget.js';

/** The same resolved settings reach native preflight and the actual scan. */
export async function sdkScanOptions(task:ScanTask,signal?:AbortSignal,workspacePrompt?:string):Promise<ScanOptions> {
 tokenBudget(task);
 const advanced=await sdkScanInputOptions(task),validation=exploitabilityOptions(task),target=await sdkScanTarget(task,signal);
 return {
  ...advanced,mode:task.scanMode,...deepScanOptions(task),
  scanPrompt:[buildScanPrompt(task.scanFocus),workspacePrompt,validation.scanPrompt,advanced.scanPrompt].filter(Boolean).join('\n\n'),
  ...(validation.validationPrompt?{validationPrompt:validation.validationPrompt}:{}),
  ...(target!==undefined?{target}:{}),
  ...(task.mock?{mock:true}:nativeOpenAI(task)?{auth:selectedSDKAuth(task)}:{}),
  // Configured-price budgets are enforced by Axiom, independently of the SDK's
  // built-in model price list. Historical CLI calls retain native semantics.
  ...(task.maxCostUsd!==undefined&&!task.tokenBudget?{maxCostUsd:task.maxCostUsd}:{}),
  ...(task.scanMode==='deep'&&task.workers!==undefined?{workers:task.workers}:{}),
  ...(signal?{signal}:{}),
 };
}
