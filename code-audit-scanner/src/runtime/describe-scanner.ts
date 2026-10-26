import {readFile} from 'node:fs/promises';
import {CONTRACT_BASELINE_VERSION,SCANNER_DESCRIPTION_VERSION,SDK_RESULT_ADAPTER_VERSION} from './contract-baseline.gen.js';

/** Reads package and generated baseline metadata without creating scan state. */
export async function describeScanner() {
  const pkg=JSON.parse(await readFile(new URL('../../package.json',import.meta.url),'utf8')) as {
    version:string; dependencies:Record<string,string>;
  };
  const version=CONTRACT_BASELINE_VERSION;
  const baselineVersions={assignment:[version],job:[version,2,3,4,5,6,7,8],progress:[version,2],event:[version,2],result:[version],completion:[version]};
  return {
    schemaVersion:SCANNER_DESCRIPTION_VERSION,
 sdkResultAdapterVersion:SDK_RESULT_ADAPTER_VERSION,
 sdkDeepResumeVersion:1,
 sdkIdentityVersion:1,
 sdkParametersVersion:1,
 sdkConcurrencyVersion:1,
    name:'axiom-scanner',
    version:pkg.version,
    baselineVersions,
    runtime:{input:'scanner_job',events:'scan_event',result:'scan_result',
      versions:{assignment:[version],job:[version,2,3,4,5,6,7,8],progress:[2],event:[2],result:[version],completion:[version]}},
    capabilities:{describe:true,jobFile:true,maxCostUsd:true,structuredEvents:true,versionedResults:true,atomicResultCommit:true},
    configuredSdk:{package:'@openai/codex-security',version:pkg.dependencies['@openai/codex-security']},
  };
}
